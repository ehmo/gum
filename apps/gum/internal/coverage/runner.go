package coverage

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// MeasureOptions controls the `go test` invocation used to measure
// per-package coverage.
type MeasureOptions struct {
	// WorkDir is the module root (contains go.mod). Defaults to ".".
	WorkDir string
	// Packages is the list of import-path patterns to measure, e.g.
	// ["./internal/dispatch/...", "./internal/output/..."]. Defaults
	// to GatedPackages().
	Packages []string
	// CoverMode is the -covermode value. Defaults to "atomic".
	CoverMode string
}

// Measure runs `go test -coverprofile=<temp> -covermode=<mode> <packages>`
// then `go tool cover -func` and returns per-package coverage Readings.
// Packages reporting "[no test files]" are recorded with HasTests=false.
func Measure(opts MeasureOptions) ([]Reading, error) {
	if opts.WorkDir == "" {
		opts.WorkDir = "."
	}
	if len(opts.Packages) == 0 {
		opts.Packages = GatedPackages()
	}
	if opts.CoverMode == "" {
		opts.CoverMode = "atomic"
	}

	tmp, err := os.CreateTemp("", "gum-coverage-*.out")
	if err != nil {
		return nil, fmt.Errorf("coverage: create temp: %w", err)
	}
	tmpPath := tmp.Name()
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return nil, fmt.Errorf("coverage: close temp: %w", err)
	}
	defer func() { _ = os.Remove(tmpPath) }()

	testArgs := []string{"test", "-coverprofile=" + tmpPath, "-covermode=" + opts.CoverMode, "-count=1"}
	testArgs = append(testArgs, opts.Packages...)
	testCmd := exec.Command("go", testArgs...)
	testCmd.Dir = opts.WorkDir
	var testOut, testErr bytes.Buffer
	testCmd.Stdout = &testOut
	testCmd.Stderr = &testErr
	if err := testCmd.Run(); err != nil {
		return nil, fmt.Errorf("coverage: go test failed: %w\nstdout:\n%s\nstderr:\n%s",
			err, testOut.String(), testErr.String())
	}

	readings := parseGoTestEmptyPackages(testOut.String())

	if !profileHasData(tmpPath) {
		return readings, nil
	}

	profile, err := os.ReadFile(tmpPath)
	if err != nil {
		return nil, fmt.Errorf("coverage: read profile: %w", err)
	}
	perPackage := ParseProfile(string(profile))
	pkgs := make([]string, 0, len(perPackage))
	for k := range perPackage {
		pkgs = append(pkgs, k)
	}
	sort.Strings(pkgs)
	for _, p := range pkgs {
		readings = append(readings, Reading{
			Package:  p,
			Percent:  perPackage[p],
			HasTests: true,
		})
	}
	return readings, nil
}

// profileLineRe matches one block line from a coverage profile, e.g.
// `github.com/ehmo/gum/internal/dispatch/foo.go:10.40,11.5 3 1`. The
// three integer-suffix fields are `numStatements`, `numStatements`,
// `count` for `-covermode=atomic` (count) and `-covermode=set` (0/1).
var profileLineRe = regexp.MustCompile(`^(\S+\.go):\d+\.\d+,\d+\.\d+\s+(\d+)\s+(\d+)$`)

// ParseProfile aggregates a `go test -coverprofile` profile body into
// per-package line-coverage percentages, matching `go test -cover`'s
// reported number byte-for-byte (modulo the same rounding `go test`
// performs).
//
// Empty body or unknown mode header yields an empty map.
func ParseProfile(body string) map[string]float64 {
	type acc struct {
		covered, total int
	}
	buckets := map[string]*acc{}
	sc := bufio.NewScanner(strings.NewReader(body))
	first := true
	for sc.Scan() {
		line := sc.Text()
		if first {
			first = false
			if !strings.HasPrefix(line, "mode:") {
				return nil
			}
			continue
		}
		m := profileLineRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		file := m[1]
		stmts, err := strconv.Atoi(m[2])
		if err != nil {
			continue
		}
		count, err := strconv.Atoi(m[3])
		if err != nil {
			continue
		}
		pkg := filepath.ToSlash(filepath.Dir(file))
		a, ok := buckets[pkg]
		if !ok {
			a = &acc{}
			buckets[pkg] = a
		}
		a.total += stmts
		if count > 0 {
			a.covered += stmts
		}
	}
	out := make(map[string]float64, len(buckets))
	for pkg, a := range buckets {
		if a.total == 0 {
			continue
		}
		out[pkg] = float64(a.covered) / float64(a.total) * 100.0
	}
	return out
}

// goTestEmptyRe matches the per-package "no test files" line emitted
// by `go test` so empty-test packages can be tagged HasTests=false.
var goTestEmptyRe = regexp.MustCompile(`^\?\s+(\S+)\s+\[no test files\]\s*$`)

func parseGoTestEmptyPackages(out string) []Reading {
	var readings []Reading
	for _, line := range strings.Split(out, "\n") {
		if m := goTestEmptyRe.FindStringSubmatch(line); m != nil {
			readings = append(readings, Reading{Package: m[1], HasTests: false})
		}
	}
	return readings
}

func profileHasData(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	lines := 0
	for sc.Scan() {
		lines++
		if lines > 1 {
			return true
		}
	}
	return false
}

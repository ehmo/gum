// Command coverage-floor measures per-package line coverage for the
// dispatch kernel and output pipeline and exits non-zero when any
// package falls below its declared threshold (FloorPercent, or a
// ratcheted minimum from internal/coverage.Ratchets).
//
// Spec source of truth: bead gum-b22o.5. The single canonical floor
// is internal/coverage.FloorPercent. Per-package ratchets capture
// current-state minimums for packages with follow-up beads tracking
// the lift to FloorPercent.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"

	"github.com/ehmo/gum/internal/coverage"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run holds the whole command so tests can drive it against a fixture
// module without spawning a subprocess. The returned int is the process
// exit code: 0 clean, 1 a floor violation on the baseline platform, 2 a
// usage or measurement failure.
func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("coverage-floor", flag.ContinueOnError)
	fs.SetOutput(stderr)
	workDir := fs.String("workdir", ".", "module directory containing go.mod for the `go test` invocation")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	readings, err := coverage.Measure(coverage.MeasureOptions{WorkDir: *workDir})
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "coverage-floor: measure: %v\n", err)
		return 2
	}

	violations := coverage.Check(readings)

	// Name the platform. Build-tagged files make per-package coverage
	// GOOS-dependent, so a clean local run on darwin is not evidence that
	// the linux gate passes.
	if coverage.OnBaselinePlatform() {
		_, _ = fmt.Fprintf(stdout, "measured on GOOS=%s (baseline platform)\n\n", runtime.GOOS)
	} else {
		_, _ = fmt.Fprintf(stdout, "measured on GOOS=%s; baselines were measured on %s. "+
			"Readings for build-tagged packages will differ from CI, "+
			"ratchet hints are suppressed, and floor violations are "+
			"warnings.\n\n", runtime.GOOS, coverage.BaselineGOOS)
	}

	_, _ = fmt.Fprintf(stdout, "%-60s %-9s %-9s\n", "PACKAGE", "COVERAGE", "FLOOR")
	_, _ = fmt.Fprintln(stdout, strings.Repeat("-", 80))
	for _, r := range readings {
		if !r.HasTests {
			_, _ = fmt.Fprintf(stdout, "%-60s %-9s %-9s\n", r.Package, "n/a", "n/a (no tests)")
			continue
		}
		need := coverage.Threshold(r.Package)
		mark := "ok"
		if r.Percent < need {
			mark = "FAIL"
		}
		_, _ = fmt.Fprintf(stdout, "%-60s %7.2f%%  %5.1f%%  %s\n", r.Package, r.Percent, need, mark)
	}
	_, _ = fmt.Fprintln(stdout)

	// Warn-only: surface packages whose coverage has climbed far enough
	// above their retention baseline to justify tightening the ratchet.
	// This never affects the exit code.
	if opps := coverage.Opportunities(readings); len(opps) > 0 {
		_, _ = fmt.Fprintln(stdout, "RATCHET_OPPORTUNITY: coverage now exceeds the baseline by "+
			fmt.Sprintf("%.1f%%+; consider raising these ratchet Mins in internal/coverage.Ratchets:", coverage.RatchetOpportunityMargin))
		for _, o := range opps {
			_, _ = fmt.Fprintf(stdout, "  %s: %.2f%% (baseline %.1f%%)\n", o.Reading.Package, o.Reading.Percent, o.Min)
		}
		_, _ = fmt.Fprintln(stdout)
	}

	if len(violations) == 0 {
		return 0
	}

	_, _ = fmt.Fprintln(stderr, "coverage-floor: per-package floor violations:")
	_, _ = fmt.Fprint(stderr, coverage.FormatViolations(violations))
	_, _ = fmt.Fprintf(stderr, "\nFloor: %.1f%% (internal/coverage.FloorPercent); ratchets track follow-up beads.\n",
		coverage.FloorPercent)

	// Only a linux reading can fail the run. internal/pluginenv has no
	// tests for its darwin files, so on darwin it always reads below the
	// linux baseline.
	if !coverage.OnBaselinePlatform() {
		_, _ = fmt.Fprintf(stderr, "Not failing: GOOS=%s is not the baseline platform. CI enforces these floors on %s.\n",
			runtime.GOOS, coverage.BaselineGOOS)
		return 0
	}

	return 1
}

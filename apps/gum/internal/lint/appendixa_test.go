// appendixa_test.go — hold the spec's dependency table to go.mod.
//
// Appendix A used to promise "CI fails the build on drift from these floors"
// with nothing reading it. Three rows had rotted into fiction by the time this
// gate was written: `spf13/viper`, `refraction-networking/utls` and
// `cyberphone/json-canonicalization` were pinned at exact versions while none of
// the three appeared in go.mod or in a single import, and `lukechampine.com/blake3`
// signed every confirmation token with no row at all.
//
// The table mixes machine-checkable cells with prose on purpose: a floor can
// read "(pin at integration)" for an optional backend, or name a pin that lives
// in a workflow file. This gate checks the two cell shapes it can check and
// leaves the prose alone.
package lint_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// appendixAHeading opens the section. The gate scans from here to the end of
// the file, so a dependency row elsewhere in the spec is out of scope.
const appendixAHeading = "## Appendix A — Dependency choices"

// notADependency is the floor cell for a module the binary must not link. The
// reserved sandboxes have carried it since the first draft; the three rows this
// gate caught now carry it too.
const notADependency = "not a dependency"

// appendixARow matches one table row, capturing the backticked module path and
// the floor cell. The notes column is free prose and is not captured.
var appendixARow = regexp.MustCompile("^\\|\\s*`([^`]+)`\\s*\\|\\s*([^|]*?)\\s*\\|")

// floorVersion matches a floor cell that is a bare version, optionally bold and
// optionally suffixed with "+" to mark it as a floor rather than a pin. A cell
// holding anything else is prose.
var floorVersion = regexp.MustCompile(`^\*{0,2}(v[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.\-]+)?)\*{0,2}\+?$`)

// TestAppendixAFloorsMatchGoMod is the gate Appendix A names. It fails when a
// stated floor is above what go.mod requires, and when a row marked "not a
// dependency" appears in go.mod anyway.
func TestAppendixAFloorsMatchGoMod(t *testing.T) {
	spec := readSpecForAppendixA(t)
	if spec == "" {
		t.Skip("docs/spec.md not present; it is private-only, see scripts/public-release-manifest.json")
	}

	required := goModRequirements(t)

	idx := strings.Index(spec, appendixAHeading)
	if idx < 0 {
		t.Fatalf("docs/spec.md: no line starts with %q", appendixAHeading)
	}

	var checked int

	for _, line := range strings.Split(spec[idx:], "\n") {
		match := appendixARow.FindStringSubmatch(line)
		if match == nil {
			continue
		}

		module, floor := match[1], match[2]

		if floor == notADependency {
			if have, ok := required[module]; ok {
				t.Errorf("Appendix A marks %s %q, but go.mod requires %s.\n"+
					"Either drop the requirement or give the row a floor.",
					module, notADependency, have)
			}
			checked++
			continue
		}

		version := floorVersion.FindStringSubmatch(floor)
		if version == nil {
			continue // prose floor: "(pin at integration)", a workflow pin, a table header
		}

		have, ok := required[module]
		if !ok {
			t.Errorf("Appendix A pins %s at %s, but go.mod requires no such module.\n"+
				"Either add the requirement or mark the row %q.",
				module, version[1], notADependency)
			checked++
			continue
		}
		if compareSemver(have, version[1]) < 0 {
			t.Errorf("go.mod has %s %s, below the Appendix A floor %s", module, have, version[1])
		}
		checked++
	}

	if checked == 0 {
		t.Fatalf("Appendix A matched no checkable rows; %s or the row format changed",
			appendixAHeading)
	}
	t.Logf("checked %d Appendix A rows against go.mod", checked)
}

// readSpecForAppendixA returns the spec text, or "" when the file is absent.
// The public export omits docs/spec.md, so absence is a skip and not a failure.
func readSpecForAppendixA(t *testing.T) string {
	t.Helper()

	path := filepath.Join(repoRootForCites(t), "docs", "spec.md")
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return ""
	}
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	return string(raw)
}

// goModRequirements returns every module go.mod requires, direct and indirect,
// keyed by module path. Indirect requirements count: a floor is a floor
// regardless of who pulled the module in.
func goModRequirements(t *testing.T) map[string]string {
	t.Helper()

	path := filepath.Join(moduleRoot(t), "go.mod")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	required := map[string]string{}
	inBlock := false

	for _, line := range strings.Split(string(raw), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "//") {
			continue
		}

		switch {
		case trimmed == "require (":
			inBlock = true
			continue
		case inBlock && trimmed == ")":
			inBlock = false
			continue
		}

		fields := strings.Fields(trimmed)
		if !inBlock {
			// A single-line requirement: "require example.com/mod v1.2.3".
			if len(fields) < 3 || fields[0] != "require" {
				continue
			}
			fields = fields[1:]
		}
		if len(fields) < 2 || !strings.HasPrefix(fields[1], "v") {
			continue
		}

		required[fields[0]] = fields[1]
	}

	if len(required) == 0 {
		t.Fatalf("%s: parsed no requirements", path)
	}

	return required
}

// compareSemver orders two module versions. It returns a negative number when a
// sorts before b, zero when they are equal, and a positive number otherwise. It
// compares the numeric triple only: a pseudo-version or prerelease suffix sorts
// with its release, which is enough to answer "is go.mod below the floor".
func compareSemver(a, b string) int {
	an, bn := semverTriple(a), semverTriple(b)

	for i := range an {
		if an[i] != bn[i] {
			return an[i] - bn[i]
		}
	}

	return 0
}

// semverTriple splits a version into its major, minor and patch numbers.
// A component that does not parse counts as zero, which keeps the comparison
// total for the pseudo-versions and +incompatible suffixes go.mod can hold.
func semverTriple(v string) [3]int {
	var out [3]int

	trimmed := strings.TrimPrefix(v, "v")
	trimmed = strings.TrimSuffix(trimmed, "+incompatible")
	if cut := strings.IndexAny(trimmed, "-+"); cut >= 0 {
		trimmed = trimmed[:cut]
	}

	for i, part := range strings.SplitN(trimmed, ".", 3) {
		if i >= len(out) {
			break
		}
		n, err := strconv.Atoi(part)
		if err != nil {
			continue
		}
		out[i] = n
	}

	return out
}

// TestAppendixARowParsing pins the two cell shapes the gate acts on. The walk
// above only proves the table is clean today. A regex that stopped matching the
// bold pin or started matching a prose floor would keep passing while the gate
// silently checked nothing, which is how the three fictional rows survived in
// the first place.
func TestAppendixARowParsing(t *testing.T) {
	rows := []struct {
		name   string
		line   string
		module string
		floor  string
	}{
		{
			"plain pin",
			"| `github.com/spf13/cobra` | v1.10.2 | CLI command tree. |",
			"github.com/spf13/cobra", "v1.10.2",
		},
		{
			"bold pin",
			"| `github.com/modelcontextprotocol/go-sdk` | **v1.6.0** | $defs preservation. |",
			"github.com/modelcontextprotocol/go-sdk", "**v1.6.0**",
		},
		{
			"floor with plus",
			"| `go.uber.org/goleak` | v1.3.0+ | Goroutine-leak verification. |",
			"go.uber.org/goleak", "v1.3.0+",
		},
		{
			"not a dependency",
			"| `go.starlark.net` | not a dependency | Reserved sandbox. |",
			"go.starlark.net", notADependency,
		},
		{
			"prose floor",
			"| `github.com/ehmo/gomoufox` | (pin at integration) | Optional. |",
			"github.com/ehmo/gomoufox", "(pin at integration)",
		},
		{
			"major-version suffix in path",
			"| `github.com/pelletier/go-toml/v2` | v2.2.4 | Replaces BurntSushi. |",
			"github.com/pelletier/go-toml/v2", "v2.2.4",
		},
	}

	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			match := appendixARow.FindStringSubmatch(row.line)
			if match == nil {
				t.Fatalf("appendixARow did not match %q", row.line)
			}
			if match[1] != row.module {
				t.Errorf("module = %q; want %q", match[1], row.module)
			}
			if match[2] != row.floor {
				t.Errorf("floor = %q; want %q", match[2], row.floor)
			}
		})
	}

	// The header separator and the notes column must not read as rows.
	for _, line := range []string{
		"|---|---|---|",
		"| Dependency | Floor | Notes |",
		"Appendix A lists `github.com/spf13/cobra` in prose, not a row.",
	} {
		if appendixARow.MatchString(line) {
			t.Errorf("appendixARow matched a non-row: %q", line)
		}
	}
}

// TestAppendixAFloorCellShapes separates a machine-checkable floor from prose.
// A prose cell that parsed as a version would have the gate compare against a
// number nobody wrote.
func TestAppendixAFloorCellShapes(t *testing.T) {
	versions := map[string]string{
		"v1.10.2":                        "v1.10.2",
		"**v1.6.0**":                     "v1.6.0",
		"v0.2.6+":                        "v0.2.6",
		"v2.16.0+":                       "v2.16.0",
		"v0.0.0-20241213102144-19d51d7f": "v0.0.0-20241213102144-19d51d7f",
	}
	for cell, want := range versions {
		match := floorVersion.FindStringSubmatch(cell)
		if match == nil {
			t.Errorf("floorVersion did not match %q", cell)
			continue
		}
		if match[1] != want {
			t.Errorf("floorVersion(%q) = %q; want %q", cell, match[1], want)
		}
	}

	prose := []string{
		notADependency,
		"(pin at integration)",
		"release-current floor pinned in `go.mod`",
		"pinned in `.github/workflows/govulncheck.yml` and `release.yml`",
		"v2.16.0+ (toolchain pin in `.github/workflows/release.yml`)",
		"Floor",
		"",
	}
	for _, cell := range prose {
		if floorVersion.MatchString(cell) {
			t.Errorf("floorVersion matched prose: %q", cell)
		}
	}
}

// TestCompareSemverOrdersFloors pins the comparison the floor check relies on.
// A string compare would put v1.9.0 above v1.10.0 and pass a go.mod that had
// dropped below the floor.
func TestCompareSemverOrdersFloors(t *testing.T) {
	cases := []struct {
		a, b string
		want string // "<", "=", ">"
	}{
		{"v1.9.0", "v1.10.0", "<"}, // the string-compare trap
		{"v1.10.2", "v1.10.2", "="},
		{"v1.56.0", "v1.50.1", ">"},
		{"v2.4.3", "v2.2.4", ">"},
		{"v0.8.1", "v0.1.1", ">"},
		{"v1.5.0", "v1.4.3", ">"},
		{"v1.3.0", "v1.3.0", "="},
		// A pseudo-version sorts with the release it precedes.
		{"v0.0.0-20241213102144-19d51d7fe467", "v0.0.0", "="},
		// +incompatible is not part of the ordering.
		{"v1.7.0+incompatible", "v1.7.0", "="},
		// A missing patch component counts as zero.
		{"v1.2", "v1.2.0", "="},
	}

	for _, tc := range cases {
		got := compareSemver(tc.a, tc.b)
		var sym string
		switch {
		case got < 0:
			sym = "<"
		case got == 0:
			sym = "="
		default:
			sym = ">"
		}
		if sym != tc.want {
			t.Errorf("compareSemver(%q, %q) = %d (%s); want %s", tc.a, tc.b, got, sym, tc.want)
		}
	}
}

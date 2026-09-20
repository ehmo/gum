package coverage

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestMeasureAppliesItsDefaults pins the two zero-value defaults. WorkDir
// falls back to the process directory and Packages to GatedPackages(), so a
// caller that passes an empty MeasureOptions measures the gated surface from
// where it stands rather than measuring nothing. Run from this package's own
// directory the gated patterns match no packages, and the failure names them,
// which is what makes both defaults observable without a full gated sweep.
func TestMeasureAppliesItsDefaults(t *testing.T) {
	if testing.Short() {
		t.Skip("Measure spawns `go test`; skip in -short")
	}
	_, err := Measure(MeasureOptions{})
	if err == nil {
		t.Fatal("Measure err = nil; want the gated patterns to fail against this package directory")
	}
	for _, want := range GatedPackages() {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Measure err = %v; want it to name the defaulted pattern %q", err, want)
		}
	}
}

// TestMeasureSkipsTheParseWhenTheProfileIsEmpty pins the early return for a run
// that emitted no coverage blocks at all. A package holding only declarations
// has nothing to instrument, so the profile is the bare mode header. Parsing it
// would divide by a zero statement total; the measurement has to end with the
// readings it already has.
func TestMeasureSkipsTheParseWhenTheProfileIsEmpty(t *testing.T) {
	if testing.Short() {
		t.Skip("Measure spawns `go test`; skip in -short")
	}
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "go.mod"), "module example.test\n\ngo 1.21\n")
	// Declarations only: no function bodies, so no statements to cover.
	mustWrite(t, filepath.Join(dir, "pkg", "pkg.go"), "package pkg\n\ntype T struct{ N int }\n\nconst X = 1\n")

	readings, err := Measure(MeasureOptions{WorkDir: dir, Packages: []string{"./pkg"}})
	if err != nil {
		t.Fatalf("Measure: %v", err)
	}
	if len(readings) != 1 {
		t.Fatalf("readings = %+v; want the one no-test-files reading", readings)
	}
	if readings[0].Package != "example.test/pkg" || readings[0].HasTests || readings[0].Percent != 0 {
		t.Errorf("readings[0] = %+v; want example.test/pkg at 0%% with HasTests=false", readings[0])
	}
}

// TestMeasureReportsATempFileFailure pins the CreateTemp arm. The profile path
// is the only channel between the `go test` subprocess and the parse step, so
// failing to make one has to abort the measurement instead of reporting an
// empty result that reads as "everything is at 0%".
func TestMeasureReportsATempFileFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("TMPDIR is not the temp-dir source on windows")
	}
	t.Setenv("TMPDIR", filepath.Join(t.TempDir(), "does-not-exist"))

	_, err := Measure(MeasureOptions{WorkDir: t.TempDir(), Packages: []string{"./pkg"}})
	if err == nil {
		t.Fatal("Measure err = nil; want the temp-file failure")
	}
	if !strings.Contains(err.Error(), "create temp") {
		t.Errorf("Measure err = %v; want it wrapped as \"create temp\"", err)
	}
}

// TestParseProfileSkipsCountsThatDoNotFitAnInt pins the two Atoi arms. The
// block regex accepts any run of digits, so a corrupt or truncated profile can
// carry a number wider than an int. Such a line is dropped; counting it as
// zero statements would silently deflate the package percentage.
func TestParseProfileSkipsCountsThatDoNotFitAnInt(t *testing.T) {
	body := strings.Join([]string{
		"mode: atomic",
		"x/y/a.go:1.1,2.1 99999999999999999999 1", // statement count overflows
		"x/y/a.go:3.1,4.1 2 99999999999999999999", // hit count overflows
		"x/y/a.go:5.1,6.1 4 1",                    // the only usable line
		"",
	}, "\n")

	got := ParseProfile(body)
	want := map[string]float64{"x/y": 100.0}
	if len(got) != len(want) || got["x/y"] != want["x/y"] {
		t.Errorf("ParseProfile = %v; want %v", got, want)
	}
}

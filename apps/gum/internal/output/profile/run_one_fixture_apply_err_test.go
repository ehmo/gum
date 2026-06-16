package profile_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ehmo/gum/internal/output/profile"
)

// TestRunFixturesApplyErrorFails pins fixture.go:116-119 — the `apply
// profile` failure arm of runOneFixture. The read-error arm is already
// pinned elsewhere; this exercises the next arm: a fixture file that reads
// fine but holds non-JSON content makes Apply fail at its json.Unmarshal
// gate (default_format is non-raw), which runOneFixture records as a
// fixture failure (Passed=false) without surfacing a hard error.
func TestRunFixturesApplyErrorFails(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "bad.json"), []byte("this is not json"), 0o644); err != nil {
		t.Fatalf("seed bad fixture: %v", err)
	}

	p, err := profile.Parse("default_format = \"toon\"\n\n[[tests]]\nname = \"badjson\"\nfixture = \"bad.json\"\n")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	res, err := profile.RunFixtures(p, dir)
	if err != nil {
		t.Fatalf("RunFixtures returned hard error; want soft per-fixture failure: %v", err)
	}
	if res.Passed {
		t.Fatal("Passed=true; want false (Apply must reject non-JSON body)")
	}
	if !containsAny(res.Fixtures[0].Failures, "apply profile") {
		t.Errorf("failures=%v; want 'apply profile' wrap", res.Fixtures[0].Failures)
	}
}

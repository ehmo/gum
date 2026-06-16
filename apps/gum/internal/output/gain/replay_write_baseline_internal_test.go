package gain

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestWriteBaselineHappyPathWritesExpectedBaselineFile pins writeBaseline's
// happy path: both runReplay calls succeed → writeBaseline marshals the
// map[string]baselineEntry to expected-baseline.json with both "toon" and
// "json" entries. Uses a tiny fixture (single response.json) so the
// token-measurement cost stays low.
func TestWriteBaselineHappyPathWritesExpectedBaselineFile(t *testing.T) {
	root := t.TempDir()
	fixDir := filepath.Join(root, "single-fixture")
	if err := os.MkdirAll(fixDir, 0o755); err != nil {
		t.Fatalf("mkdir fixture: %v", err)
	}
	resp := `{"items":[]}`
	if err := os.WriteFile(filepath.Join(fixDir, "response.json"), []byte(resp), 0o644); err != nil {
		t.Fatalf("write response.json: %v", err)
	}

	if err := writeBaseline(root); err != nil {
		t.Fatalf("writeBaseline: %v", err)
	}

	body, err := os.ReadFile(filepath.Join(root, "expected-baseline.json"))
	if err != nil {
		t.Fatalf("read expected-baseline.json: %v", err)
	}
	var got map[string]baselineEntry
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("unmarshal baseline: %v (body=%s)", err, body)
	}
	if _, ok := got["toon"]; !ok {
		t.Errorf("baseline missing 'toon' entry; got keys=%v", got)
	}
	if _, ok := got["json"]; !ok {
		t.Errorf("baseline missing 'json' entry; got keys=%v", got)
	}
}

// TestWriteBaselineRunReplayErrorPropagates pins writeBaseline's `runReplay
// err → return err` arm (replay.go:343-345). Reached when the fixture
// directory doesn't exist — collectFixtureLeaves's WalkDir surfaces the
// stat err, runReplay wraps it with "walk dir", writeBaseline returns it
// verbatim.
func TestWriteBaselineRunReplayErrorPropagates(t *testing.T) {
	err := writeBaseline("/nonexistent/path/that/does/not/exist")
	if err == nil {
		t.Fatal("writeBaseline(bogus dir) err=nil; want propagated runReplay err")
	}
	if !strings.Contains(err.Error(), "walk dir") {
		t.Errorf("err=%v; want 'walk dir' wrap from runReplay", err)
	}
}

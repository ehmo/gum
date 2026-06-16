package testmatrix

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestParseExtractsGroupsAndTests verifies the parser walks a synthetic
// matrix and assigns each backticked test name to the correct group while
// dropping rows that appear before any group header.
func TestParseExtractsGroupsAndTests(t *testing.T) {
	in := strings.NewReader(`
# Header
| Requirement | Proof | Phase |
|---|---|---|
| pre-group row dropped | ` + "`" + `TestOrphan` + "`" + ` | v0.1 CI |
<!-- Group A: Alpha -->
| row one | ` + "`" + `TestAlphaOne` + "`" + ` | v0.1 CI |
| row two | ` + "`" + `TestAlphaTwo` + "`" + ` / ` + "`" + `TestAlphaThree` + "`" + ` | v0.1 CI |
| duplicate | ` + "`" + `TestAlphaOne` + "`" + ` | v0.1 CI |
<!-- Group B: Bravo -->
| row b | ` + "`" + `TestBravoOne` + "`" + ` | v0.1 CI |
| placeholder | ` + "`" + `TestBackendKind<Name>` + "`" + ` | Same PR |
| fuzz target | ` + "`" + `FuzzBravoFoo` + "`" + ` | v0.1 CI |
`)
	groups, err := Parse(in)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got, want := len(groups), 2; got != want {
		t.Fatalf("group count: got %d want %d", got, want)
	}
	if got, want := groups[0].Letter, "A"; got != want {
		t.Errorf("groups[0].Letter: got %q want %q", got, want)
	}
	if got, want := groups[0].Description, "Alpha"; got != want {
		t.Errorf("groups[0].Description: got %q want %q", got, want)
	}
	wantA := []string{"TestAlphaOne", "TestAlphaThree", "TestAlphaTwo"}
	if !equalStrings(groups[0].Tests, wantA) {
		t.Errorf("groups[0].Tests: got %v want %v", groups[0].Tests, wantA)
	}
	wantB := []string{"FuzzBravoFoo", "TestBravoOne"}
	if !equalStrings(groups[1].Tests, wantB) {
		t.Errorf("groups[1].Tests: got %v want %v (placeholder must be filtered)", groups[1].Tests, wantB)
	}
}

// TestParseFileLiveMatrix is an integration check that runs the parser
// against the actual docs/test-matrix.md and asserts the canonical group
// letters appear. This is a drift detector: if a group letter disappears
// from the matrix or a new one is added, this test surfaces the change.
func TestParseFileLiveMatrix(t *testing.T) {
	path := liveMatrixPath(t)
	groups, err := ParseFile(path)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	if len(groups) == 0 {
		t.Fatalf("no groups parsed from live matrix")
	}
	have := map[string]bool{}
	for _, g := range groups {
		have[g.Letter] = true
		if len(g.Tests) == 0 {
			t.Errorf("group %s (%s) has zero runnable tests", g.Letter, g.Description)
		}
	}
	for _, want := range []string{"A", "B", "C", "D", "E", "F"} {
		if !have[want] {
			t.Errorf("group %s missing from live matrix", want)
		}
	}
}

func liveMatrixPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, _ := runtime.Caller(0)
	// internal/testmatrix/parser_test.go -> apps/gum/docs/test-matrix.md
	return filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "..", "docs", "test-matrix.md")
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

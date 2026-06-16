package testmatrix

import "testing"

// TestCompileRunPatternEmptyMatchesNothing pins compileRunPattern's
// `len(names) == 0 → return "^$"` arm (runner.go:109-111). An empty
// group MUST yield a regex that matches no test name — otherwise an
// empty -run pattern in go test would match every test and the
// group's RAN count would explode.
func TestCompileRunPatternEmptyMatchesNothing(t *testing.T) {
	t.Parallel()
	got := compileRunPattern(nil)
	if got != "^$" {
		t.Errorf("compileRunPattern(nil) = %q; want %q", got, "^$")
	}
}

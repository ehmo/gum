package testmatrix_test

import (
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/testmatrix"
)

// TestParseFileOpenErrorWraps pins ParseFile's `os.Open err → wrap`
// arm (parser.go:44-46). A missing path MUST surface "testmatrix:
// open" + the path so the operator knows which file the test-matrix
// loader couldn't find.
func TestParseFileOpenErrorWraps(t *testing.T) {
	t.Parallel()
	missing := "/does/not/exist/testmatrix.md"
	_, err := testmatrix.ParseFile(missing)
	if err == nil {
		t.Fatal("ParseFile(missing) err=nil; want open-wrap")
	}
	if !strings.Contains(err.Error(), "testmatrix: open") {
		t.Errorf("err=%v; want 'testmatrix: open' wrap", err)
	}
	if !strings.Contains(err.Error(), missing) {
		t.Errorf("err=%v; want path %q in wrap", err, missing)
	}
}

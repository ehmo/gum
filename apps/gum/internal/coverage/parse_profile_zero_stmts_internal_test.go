package coverage

import "testing"

// TestParseProfileZeroStatementBucketSkipped pins ParseProfile's
// `a.total == 0 → continue` arm (runner.go:143-144). A profile block
// with stmts=0 creates a bucket whose total never advances; that
// bucket MUST be skipped from the output map rather than emitting a
// 0/0 division (NaN) for the package.
func TestParseProfileZeroStatementBucketSkipped(t *testing.T) {
	t.Parallel()
	body := "mode: set\n" +
		"github.com/x/y/empty.go:1.0,2.0 0 0\n" +
		"github.com/x/y/empty.go:2.0,3.0 0 1\n"
	got := ParseProfile(body)
	if _, ok := got["github.com/x/y"]; ok {
		t.Errorf("got %+v; want bucket skipped (all-zero-statement entries)", got)
	}
}

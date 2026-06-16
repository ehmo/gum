package pluginenv

import "testing"

// TestParseDenylistSkipsBlankAndCommentLines pins parseDenylist's
// `line == "" || HasPrefix(line, "#") → continue` arm (denylist.go:38-39).
// The embedded denylist.txt at init time has no blanks or comments,
// so this arm is only reachable via direct parseDenylist invocation
// from an internal test. Future contributors may add comments to the
// denylist file — without this test, an editorial blank/comment would
// silently produce a "" entry that IsDeniedEnv would treat as a wild
// catch-all (every env starts with "").
func TestParseDenylistSkipsBlankAndCommentLines(t *testing.T) {
	in := []byte("\n# leading comment\nFOO\n   \n# trailing comment\nBAR\n\n")
	out := parseDenylist(in)
	if len(out) != 2 {
		t.Fatalf("len(out)=%d %v; want 2 entries [FOO BAR]", len(out), out)
	}
	if out[0] != "FOO" || out[1] != "BAR" {
		t.Errorf("out=%v; want [FOO BAR]", out)
	}
}

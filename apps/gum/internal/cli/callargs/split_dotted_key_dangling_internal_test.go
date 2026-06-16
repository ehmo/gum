package callargs

import (
	"strings"
	"testing"
)

// TestSplitDottedKeyDanglingBackslashRejected pins splitDottedKey's
// `i+1 >= len(key) → dangling backslash` arm (parser.go:285-287).
// This arm is structurally unreachable through ParseArgs: splitKVPair
// requires a key to terminate at an unescaped `=`, and its own `\X`
// skip ensures the key handed to splitDottedKey never ends with a
// lone `\`. We pin the defensive guard via a direct internal call so
// a future refactor that *does* expose this code path to user input
// (e.g. via a new field path syntax in --filter) doesn't quietly
// regress to "trim trailing backslash" semantics.
func TestSplitDottedKeyDanglingBackslashRejected(t *testing.T) {
	t.Parallel()
	_, err := splitDottedKey(`x\`)
	if err == nil {
		t.Fatal("splitDottedKey(\"x\\\\\") err=nil; want dangling-backslash err")
	}
	if !strings.Contains(err.Error(), "dangling backslash") {
		t.Errorf("err=%q; want 'dangling backslash' substring", err.Error())
	}
}

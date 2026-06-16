package callargs_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/cli/callargs"
)

// splitDottedKey is unexported but reachable via ParseArgs: any
// key=value where the key contains an unescaped dot routes through
// it. We pin three error arms via ParseArgs and surface the typed
// error so the message text + code stay observable.

// TestParseArgsEmptyKeyRejected pins splitDottedKey's
// `key == "" → CLI_ARG_INVALID(empty key)` arm (parser.go:277-279).
// An =value with no key on the left is structurally meaningless and
// must NOT be silently dropped or coerced.
func TestParseArgsEmptyKeyRejected(t *testing.T) {
	t.Parallel()
	_, err := callargs.ParseArgs([]string{"=value"}, callargs.Options{})
	if err == nil {
		t.Fatal("ParseArgs([=value]) err=nil; want CLI_ARG_INVALID")
	}
	var ce *callargs.Error
	if !errors.As(err, &ce) {
		t.Fatalf("err=%T; want *callargs.Error", err)
	}
	if ce.Code != "CLI_ARG_INVALID" {
		t.Errorf("code=%q; want CLI_ARG_INVALID", ce.Code)
	}
}

// TestParseArgsUnrecognizedEscapeRejected pins the
// `unrecognized escape \X → CLI_ARG_INVALID` arm (parser.go:289-292).
// Only \. and \\ are valid escapes; anything else (e.g. \n) is a
// typo or a misunderstanding of the syntax and must error rather than
// silently emit a backslash literal.
func TestParseArgsUnrecognizedEscapeRejected(t *testing.T) {
	t.Parallel()
	_, err := callargs.ParseArgs([]string{`weird\nkey=v`}, callargs.Options{})
	if err == nil {
		t.Fatal("ParseArgs([weird\\nkey=v]) err=nil; want unrecognized-escape err")
	}
	if !strings.Contains(err.Error(), "unrecognized escape") {
		t.Errorf("err=%q; want 'unrecognized escape' substring", err.Error())
	}
}

// TestParseArgsEmptyDottedSegmentRejected pins the
// `cur.Len() == 0 on dot → CLI_ARG_INVALID(empty segment)` arm
// (parser.go:298-300). Two consecutive unescaped dots leave a
// zero-length segment between them, which has no meaningful path
// interpretation and must error.
func TestParseArgsEmptyDottedSegmentRejected(t *testing.T) {
	t.Parallel()
	_, err := callargs.ParseArgs([]string{"a..b=v"}, callargs.Options{})
	if err == nil {
		t.Fatal("ParseArgs([a..b=v]) err=nil; want empty-segment err")
	}
	if !strings.Contains(err.Error(), "empty segment") {
		t.Errorf("err=%q; want 'empty segment' substring", err.Error())
	}
}

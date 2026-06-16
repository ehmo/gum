package callargs

import (
	"strings"
	"testing"
)

// newResult builds an empty *Result the way ParseArgs does, for direct
// applyInline unit tests.
func newResult() *Result {
	return &Result{
		Args:       map[string]any{},
		InlineKeys: map[string]struct{}{},
		FromFiles:  map[string]struct{}{},
	}
}

// TestParseArgsFileLoadErrorPropagates pins parser.go:105-107 — the
// fileArgs loop's `loadFile err → return nil, err` arm. A @-prefixed
// arg pointing at a nonexistent file must abort ParseArgs with the
// loadFile error rather than silently skipping the file.
func TestParseArgsFileLoadErrorPropagates(t *testing.T) {
	_, err := ParseArgs([]string{"@/no/such/file/anywhere.json"}, Options{})
	if err == nil {
		t.Fatal("want loadFile error; got nil")
	}
	var ce *Error
	if !asArgError(err, &ce) {
		t.Fatalf("err type=%T; want *callargs.Error", err)
	}
	if ce.Code != "CLI_ARG_INVALID" {
		t.Errorf("Code=%q; want CLI_ARG_INVALID", ce.Code)
	}
}

// TestParseArgsFileNotJSONObjectRejected pins parser.go:109-111 — the
// `json.Unmarshal(body, &obj) err → CLI_ARG_INVALID "not a JSON object"`
// arm. A file whose body is valid JSON but NOT an object (here a bare
// array via @- stdin) must be rejected: file args must merge as a
// key→value object, so a top-level array has no merge semantics.
func TestParseArgsFileNotJSONObjectRejected(t *testing.T) {
	_, err := ParseArgs([]string{"@-"}, Options{Stdin: strings.NewReader("[1,2,3]")})
	if err == nil {
		t.Fatal("want 'not a JSON object' error; got nil")
	}
	var ce *Error
	if !asArgError(err, &ce) {
		t.Fatalf("err type=%T; want *callargs.Error", err)
	}
	if !strings.Contains(ce.Reason, "not a JSON object") {
		t.Errorf("Reason=%q; want 'not a JSON object' substr", ce.Reason)
	}
}

// TestLoadFileStdinDashNilReaderFallsBackToOsStdin pins parser.go:130-132
// — the `@-` branch with a nil Options.Stdin, which falls back to
// os.Stdin. Under `go test` stdin is wired to /dev/null, so ReadAll
// returns EOF immediately; the deliverable is that the nil-reader
// fallback assignment is exercised without a custom reader.
func TestLoadFileStdinDashNilReaderFallsBackToOsStdin(t *testing.T) {
	body, err := loadFile("@-", nil)
	if err != nil {
		t.Fatalf("loadFile(@-, nil): %v", err)
	}
	// /dev/null stdin yields an empty (non-nil) body; we only assert the
	// call returned cleanly via the os.Stdin fallback path.
	if body == nil {
		// io.ReadAll never returns nil on success, but guard anyway.
		t.Error("body nil; want empty slice from os.Stdin EOF")
	}
}

// TestApplyInlineEmptyArgRejected pins parser.go:148-150 — the
// `a == "" → CLI_ARG_INVALID "empty positional argument"` guard.
func TestApplyInlineEmptyArgRejected(t *testing.T) {
	err := applyInline("", newResult(), map[string]struct{}{})
	if err == nil {
		t.Fatal("want empty-positional error; got nil")
	}
	var ce *Error
	if !asArgError(err, &ce) {
		t.Fatalf("err type=%T; want *callargs.Error", err)
	}
	if !strings.Contains(ce.Reason, "empty positional argument") {
		t.Errorf("Reason=%q; want 'empty positional argument' substr", ce.Reason)
	}
}

// TestApplyInlineJSONKindDecodeErrorRejected pins parser.go:171-173 —
// the `key:=value` (json kind) decode-failure arm. A value that is not
// parseable JSON must be rejected with CLI_ARG_INVALID rather than
// stored as a malformed any.
func TestApplyInlineJSONKindDecodeErrorRejected(t *testing.T) {
	err := applyInline("x:={not valid json", newResult(), map[string]struct{}{})
	if err == nil {
		t.Fatal("want json-decode error; got nil")
	}
	var ce *Error
	if !asArgError(err, &ce) {
		t.Fatalf("err type=%T; want *callargs.Error", err)
	}
	if ce.Code != "CLI_ARG_INVALID" {
		t.Errorf("Code=%q; want CLI_ARG_INVALID", ce.Code)
	}
}

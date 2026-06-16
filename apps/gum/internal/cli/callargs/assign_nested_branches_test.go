package callargs_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/cli/callargs"
)

// TestAssignNestedDuplicateNestedKeyReportsCLIArgDuplicate pins the
// `existing && !arraySet → CLI_ARG_DUPLICATE` arm for the nested case:
// two inline args that target the SAME dotted key MUST surface
// CLI_ARG_DUPLICATE rather than silently overwrite, because both calls
// are user intent and the second one losing would be unobservable.
func TestAssignNestedDuplicateNestedKeyReportsCLIArgDuplicate(t *testing.T) {
	_, err := callargs.ParseArgs([]string{
		"foo.bar=first",
		"foo.bar=second",
	}, callargs.Options{})
	if err == nil {
		t.Fatal("want CLI_ARG_DUPLICATE; got nil")
	}
	var ce *callargs.Error
	if !errors.As(err, &ce) {
		t.Fatalf("err type=%T; want *callargs.Error", err)
	}
	if ce.Code != "CLI_ARG_DUPLICATE" {
		t.Errorf("Code=%q; want CLI_ARG_DUPLICATE", ce.Code)
	}
	if ce.Key != "foo.bar" {
		t.Errorf("Key=%q; want foo.bar", ce.Key)
	}
}

// TestAssignNestedDuplicateOnArrayFieldAppends pins the
// `existing && arraySet → appendArray(existing, value)` arm: when a
// dotted key is declared array-typed in ArrayFields, the second
// assignment MUST append rather than error. This is the spec'd way
// for `gum read users.list ids=1 ids=2` to build `ids: [1, 2]`.
func TestAssignNestedDuplicateOnArrayFieldAppends(t *testing.T) {
	res, err := callargs.ParseArgs([]string{
		"event.tags=first",
		"event.tags=second",
		"event.tags=third",
	}, callargs.Options{ArrayFields: []string{"event.tags"}})
	if err != nil {
		t.Fatalf("ParseArgs err=%v; want array-merge success", err)
	}
	ev, ok := res.Args["event"].(map[string]any)
	if !ok {
		t.Fatalf("Args[event] not map[string]any: %T", res.Args["event"])
	}
	arr, ok := ev["tags"].([]any)
	if !ok {
		t.Fatalf("Args[event][tags] not []any: %T", ev["tags"])
	}
	if len(arr) != 3 {
		t.Errorf("len(tags)=%d; want 3 (appended)", len(arr))
	}
	if arr[0] != "first" || arr[1] != "second" || arr[2] != "third" {
		t.Errorf("tags=%v; want [first second third]", arr)
	}
}

// TestAssignNestedPathCollidesWithScalar pins the
// `nv non-map → "collides with non-object value"` arm. When a scalar
// is assigned first then a nested path tries to walk through it, the
// parser MUST surface CLI_ARG_INVALID with both the full target path
// and the collision point so the operator knows exactly which earlier
// arg shadowed the nested one.
func TestAssignNestedPathCollidesWithScalar(t *testing.T) {
	_, err := callargs.ParseArgs([]string{
		"foo=scalar-value",
		"foo.bar=nested",
	}, callargs.Options{})
	if err == nil {
		t.Fatal("want collision err; got nil")
	}
	var ce *callargs.Error
	if !errors.As(err, &ce) {
		t.Fatalf("err type=%T; want *callargs.Error", err)
	}
	if ce.Code != "CLI_ARG_INVALID" {
		t.Errorf("Code=%q; want CLI_ARG_INVALID", ce.Code)
	}
	if !strings.Contains(ce.Reason, "collides with non-object value") {
		t.Errorf("Reason=%q; want 'collides with non-object value' substr", ce.Reason)
	}
}

// TestNestedFileValueOverriddenByInline pins the audit fix: an inline arg may
// override a NESTED value that came from an @file (spec §12.0 rule 3: inline
// overrides file), not just top-level keys. A second inline for the same nested
// path still conflicts.
func TestNestedFileValueOverriddenByInline(t *testing.T) {
	stdin := strings.NewReader(`{"message":{"subject":"from-file","body":"keep"}}`)
	res, err := callargs.ParseArgs([]string{"@-", "message.subject=override"}, callargs.Options{Stdin: stdin})
	if err != nil {
		t.Fatalf("ParseArgs: %v", err)
	}
	msg, ok := res.Args["message"].(map[string]any)
	if !ok {
		t.Fatalf("message is %T, want map", res.Args["message"])
	}
	if msg["subject"] != "override" {
		t.Errorf("message.subject = %v, want override (inline must override the file value)", msg["subject"])
	}
	if msg["body"] != "keep" {
		t.Errorf("message.body = %v, want keep (untouched file value)", msg["body"])
	}

	// Two inline overrides of the same nested file key must still conflict.
	stdin2 := strings.NewReader(`{"message":{"subject":"f"}}`)
	_, err2 := callargs.ParseArgs([]string{"@-", "message.subject=a", "message.subject=b"}, callargs.Options{Stdin: stdin2})
	if !callargs.IsDuplicate(err2) {
		t.Errorf("two inline overrides of the same nested file key: err = %v, want CLI_ARG_DUPLICATE", err2)
	}
}

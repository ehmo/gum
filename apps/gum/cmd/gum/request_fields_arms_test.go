package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/ehmo/gum/internal/catalog"
)

// errReader fails on the first Read with a non-EOF error, modeling a broken
// pipe or EIO on the wizard's stdin.
type errReader struct{ err error }

func (r errReader) Read([]byte) (int, error) { return 0, r.err }

func TestLookupRequestFieldsAndCatalogHasOp(t *testing.T) {
	if got := lookupRequestFields("no.such.op"); got != nil {
		t.Fatalf("unknown op: want nil fields, got %d", len(got))
	}
	if len(lookupRequestFields("calendar.events.list")) == 0 {
		t.Fatal("calendar.events.list: want request fields from the embedded catalog")
	}
	if !catalogHasOp("calendar.events.list") {
		t.Fatal("catalogHasOp(calendar.events.list) = false")
	}
	if catalogHasOp("no.such.op") {
		t.Fatal("catalogHasOp(no.such.op) = true")
	}
}

func TestCoerceScalarParsesNumbers(t *testing.T) {
	if got := coerceScalar("1.5", "number"); got != 1.5 {
		t.Fatalf("number: got %#v, want 1.5", got)
	}
	if got := coerceScalar("not-a-number", "number"); got != "not-a-number" {
		t.Fatalf("unparseable number: got %#v, want the string back", got)
	}
}

func TestFieldTypeHintUntypedArray(t *testing.T) {
	if got := fieldTypeHint(catalog.RequestField{Type: "array"}); got != "value" {
		t.Fatalf("array without an item type: got %q, want %q", got, "value")
	}
	if got := fieldTypeHint(catalog.RequestField{}); got != "value" {
		t.Fatalf("field without a type: got %q, want %q", got, "value")
	}
	if got := fieldTypeHint(catalog.RequestField{Type: "integer"}); got != "integer" {
		t.Fatalf("typed field: got %q, want %q", got, "integer")
	}
}

func TestValidateEnumArgsCoercesTypedListElements(t *testing.T) {
	args := map[string]any{"mode": []any{5}}
	fields := []catalog.RequestField{{Name: "mode", Enum: []string{"5", "b"}}}
	if err := validateEnumArgs(args, fields); err != nil {
		t.Fatalf("validateEnumArgs: %v", err)
	}
	list, ok := args["mode"].([]any)
	if !ok || len(list) != 1 || list[0] != "5" {
		t.Fatalf("typed list element: got %#v, want []any{\"5\"}", args["mode"])
	}
}

func TestValidateEnumArgsSkipsANonObjectBody(t *testing.T) {
	args := map[string]any{bodyArgKey: "raw-json-string"}
	fields := []catalog.RequestField{{Name: "mode", Enum: []string{"a"}}}
	if err := validateEnumArgs(args, fields); err != nil {
		t.Fatalf("non-object body: got %v, want nil", err)
	}
}

// TestValidateEnumArgsChecksTheExplicitBody covers every arm of the nested
// body pass: a bad string, a typed list element, and a bare typed scalar.
func TestValidateEnumArgsChecksTheExplicitBody(t *testing.T) {
	fields := []catalog.RequestField{{Name: "mode", Enum: []string{"a", "b"}}}

	t.Run("string mismatch", func(t *testing.T) {
		args := map[string]any{bodyArgKey: map[string]any{"mode": "z"}}
		err := validateEnumArgs(args, fields)
		if err == nil || !strings.Contains(err.Error(), `mode="z" is not a valid choice`) {
			t.Fatalf("got %v, want a not-a-valid-choice error", err)
		}
	})

	t.Run("string canonicalized in place", func(t *testing.T) {
		body := map[string]any{"mode": "A"}
		args := map[string]any{bodyArgKey: body}
		if err := validateEnumArgs(args, fields); err != nil {
			t.Fatalf("validateEnumArgs: %v", err)
		}
		if body["mode"] != "a" {
			t.Fatalf("body mode: got %#v, want %q", body["mode"], "a")
		}
	})

	t.Run("typed list element", func(t *testing.T) {
		body := map[string]any{"mode": []any{7}}
		args := map[string]any{bodyArgKey: body}
		err := validateEnumArgs(args, fields)
		if err == nil || !strings.Contains(err.Error(), `mode="7" is not a valid choice`) {
			t.Fatalf("got %v, want a not-a-valid-choice error naming 7", err)
		}
	})

	t.Run("bare typed scalar", func(t *testing.T) {
		body := map[string]any{"mode": 7}
		args := map[string]any{bodyArgKey: body}
		err := validateEnumArgs(args, fields)
		if err == nil || !strings.Contains(err.Error(), `mode="7" is not a valid choice`) {
			t.Fatalf("got %v, want a not-a-valid-choice error naming 7", err)
		}
	})
}

func TestCheckScalarTypeRejectsABadNumber(t *testing.T) {
	err := checkScalarType("score", "high", "number")
	if err == nil || !strings.Contains(err.Error(), "expected a number") {
		t.Fatalf("got %v, want an expected-a-number error", err)
	}
	if err := checkScalarType("score", "1.5", "number"); err != nil {
		t.Fatalf("valid number: %v", err)
	}
}

func TestPromptMissingFieldsShowsEnumChoices(t *testing.T) {
	var errOut bytes.Buffer
	args := map[string]any{}
	fields := []catalog.RequestField{{Name: "mode", Required: true, Enum: []string{"a", "b"}}}
	if err := promptMissingFields(strings.NewReader("a\n"), &errOut, args, fields); err != nil {
		t.Fatalf("promptMissingFields: %v", err)
	}
	if !strings.Contains(errOut.String(), "[a|b]") {
		t.Fatalf("prompt %q: want the enum choices", errOut.String())
	}
	if args["mode"] != "a" {
		t.Fatalf("mode: got %#v, want %q", args["mode"], "a")
	}
}

func TestPromptMissingFieldsSurfacesAReadError(t *testing.T) {
	boom := errors.New("broken pipe")
	err := promptMissingFields(errReader{err: boom}, &bytes.Buffer{}, map[string]any{},
		[]catalog.RequestField{{Name: "mode", Required: true}})
	if err == nil || !errors.Is(err, boom) {
		t.Fatalf("got %v, want the underlying read error", err)
	}
	if !strings.Contains(err.Error(), "reading mode") {
		t.Fatalf("error %q: want it to name the field", err)
	}
}

func TestApplyKebabFlagsMergesASinglePositional(t *testing.T) {
	cmd := &cobra.Command{Use: "call"}
	cmd.Flags().StringArray("dimensions", nil, "")
	if err := cmd.Flags().Set("dimensions", "page"); err != nil {
		t.Fatalf("set flag: %v", err)
	}
	args := map[string]any{"dimensions": "query"} // single positional, pre-coercion
	applyKebabFlags(cmd, args, []catalog.RequestField{{Name: "dimensions", Type: "array"}})
	got, ok := args["dimensions"].([]any)
	if !ok || len(got) != 2 || got[0] != "query" || got[1] != "page" {
		t.Fatalf("dimensions: got %#v, want []any{\"query\",\"page\"}", args["dimensions"])
	}
}

func TestRegisterDynamicCallFlagsIgnoresANonCallRoot(t *testing.T) {
	root := &cobra.Command{Use: "root"}
	registerDynamicCallFlags(root, []string{"call", "calendar.events.list"})
	if root.Flags().Lookup("order-by") != nil {
		t.Fatal("registered a dynamic flag on a root without a call command")
	}
}

func TestRegisterDynamicCallFlagsWithoutAnOpID(t *testing.T) {
	root := newRootCmd()
	registerDynamicCallFlags(root, []string{"call", "--raw", "notanopid"})
	callCmd, _, err := root.Find([]string{"call"})
	if err != nil {
		t.Fatalf("find call: %v", err)
	}
	if callCmd.Flags().Lookup("order-by") != nil {
		t.Fatal("registered a dynamic flag without an op_id in the raw args")
	}
}

// TestRegisterDynamicCallFlagsSkipsFlagValues pins the op_id scan: a value
// token after a value-taking flag is skipped, a dotless token is skipped, a
// field colliding with a real call flag is not shadowed, and an enum field
// gets a completion function.
func TestRegisterDynamicCallFlagsSkipsFlagValues(t *testing.T) {
	root := newRootCmd()
	raw := []string{"gum", "call", "--profile", "my.profile", "--raw", "notanopid", "calendar.events.list"}
	registerDynamicCallFlags(root, raw)

	callCmd, _, err := root.Find([]string{"call"})
	if err != nil {
		t.Fatalf("find call: %v", err)
	}
	if callCmd.Flags().Lookup("order-by") == nil {
		t.Fatal("orderBy: want a dynamic --order-by flag")
	}
	// pageToken collides with the host --page-token control, so the dynamic
	// registration must leave the original flag in place.
	pt := callCmd.Flags().Lookup("page-token")
	if pt == nil || pt.Value.Type() != "string" {
		t.Fatalf("page-token: got %#v, want the pre-existing host flag", pt)
	}

	fn, ok := callCmd.GetFlagCompletionFunc("order-by")
	if !ok {
		t.Fatal("order-by: want a registered completion function")
	}
	vals, directive := fn(callCmd, nil, "")
	if len(vals) == 0 {
		t.Fatal("order-by completion: want the enum values")
	}
	if directive != cobra.ShellCompDirectiveNoFileComp {
		t.Fatalf("directive: got %v, want NoFileComp", directive)
	}
}

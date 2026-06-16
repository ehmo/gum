package adapters_test

import (
	"context"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/adapters"
	"github.com/ehmo/gum/internal/dispatch"
)

// TestCodeRunnerExecuteNilArgsTreatedAsEmpty pins the
// `inv.Args == nil → args = map[string]any{}` initializer. A caller
// can hand the adapter a wholly-empty Invocation (the dispatch path
// permits this when an op's variant carries no args schema). The
// subsequent guards MUST fire on the empty map rather than panicking
// on a nil-map lookup — without this initializer, the first
// `args["language"]` index would still be safe (Go map index on nil
// returns zero), but the assertion is documented contract.
func TestCodeRunnerExecuteNilArgsTreatedAsEmpty(t *testing.T) {
	cr := adapters.NewCodeRunner()
	inv := &dispatch.Invocation{OpID: "gum.code", Args: nil}
	_, err := cr.Execute(context.Background(), inv, minimalCodeVariant(), nil)
	if err == nil {
		t.Fatal("Execute(nil args)=nil err; want LANGUAGE_NOT_SUPPORTED")
	}
	// Empty args → language == "" → fails the "must be risor" check.
	if !strings.Contains(err.Error(), "LANGUAGE_NOT_SUPPORTED") {
		t.Errorf("err=%q; want LANGUAGE_NOT_SUPPORTED (proves nil-args was normalized to empty map and the language guard fired)", err)
	}
}

// TestCodeRunnerExecuteRejectsNonRisorLanguage pins the
// `language != "risor" → LANGUAGE_NOT_SUPPORTED` guard. v0.1.0 is
// Risor-only; the guard makes that contract visible to the caller
// (operators may set `language: "python"` expecting future support).
// Without the guard the empty-source path would surface a
// non-actionable "code is required" error instead of the real reason.
func TestCodeRunnerExecuteRejectsNonRisorLanguage(t *testing.T) {
	cr := adapters.NewCodeRunner()
	inv := &dispatch.Invocation{
		OpID: "gum.code",
		Args: map[string]any{"language": "python", "source": "print('hi')"},
	}
	_, err := cr.Execute(context.Background(), inv, minimalCodeVariant(), nil)
	if err == nil {
		t.Fatal("Execute(python)=nil err; want LANGUAGE_NOT_SUPPORTED")
	}
	if !strings.Contains(err.Error(), "LANGUAGE_NOT_SUPPORTED") {
		t.Errorf("err=%q; want LANGUAGE_NOT_SUPPORTED", err)
	}
}

// TestCodeRunnerExecuteRejectsEmptyCode pins the
// `code == "" → INVALID_ARGS: code is required` guard. The sandbox
// would happily run a no-op empty program and return zero printed
// bytes, but that would silently mask a caller bug (forgot to
// populate `source`) — the guard surfaces it as a clean dispatch
// error operators can act on.
func TestCodeRunnerExecuteRejectsEmptyCode(t *testing.T) {
	cr := adapters.NewCodeRunner()
	inv := &dispatch.Invocation{
		OpID: "gum.code",
		Args: map[string]any{"language": "risor", "source": ""},
	}
	_, err := cr.Execute(context.Background(), inv, minimalCodeVariant(), nil)
	if err == nil {
		t.Fatal("Execute(empty code)=nil err; want INVALID_ARGS")
	}
	if !strings.Contains(err.Error(), "INVALID_ARGS") {
		t.Errorf("err=%q; want INVALID_ARGS: code is required", err)
	}
}

// TestCodeRunnerExecuteGumSearchStubReturnsEmpty pins the
// `"gum_search": func(query string) any { return []any{} }` closure
// body. v0.1.0 ships gum_search as a registered-but-stubbed global
// so Risor scripts don't crash with "name not found"; the contract
// is that it returns an empty list for any query so downstream
// `.length()` / iteration code is well-defined. Without this test
// the body is unreachable code; without the body, a future refactor
// could silently break the well-defined-empty contract.
func TestCodeRunnerExecuteGumSearchStubReturnsEmpty(t *testing.T) {
	cr := adapters.NewCodeRunner()
	inv := &dispatch.Invocation{
		OpID: "gum.code",
		Args: map[string]any{
			"language": "risor",
			// Risor: assign gum_search result, print its length. Empty list -> "0".
			"source": `gum_print(gum_search("anything"))`,
		},
	}
	resp, err := cr.Execute(context.Background(), inv, minimalCodeVariant(), nil)
	if err != nil {
		t.Fatalf("Execute(gum_search): %v", err)
	}
	// gum_search stub returns []any{}, which prints as "[]".
	if strings.TrimSpace(string(resp.Body)) != "[]" {
		t.Errorf("body=%q; want '[]' (empty list)", resp.Body)
	}
}

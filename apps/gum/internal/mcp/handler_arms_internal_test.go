package mcp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/dispatch"
)

// TestFoldConvenienceBodyRejectsNonObject pins foldConvenienceBody's
// non-object arm (handlers.go:136-139). A convenience tool whose body
// arg arrives as a scalar MUST be refused locally; folding it would
// send a malformed body upstream.
func TestFoldConvenienceBodyRejectsNonObject(t *testing.T) {
	args := map[string]any{"requestBody": "not-an-object"}
	err := foldConvenienceBody(&ConvenienceABI{BodyArg: "requestBody"}, args)
	if err == nil {
		t.Fatal("foldConvenienceBody(scalar body) err=nil; want the type refusal")
	}
	if !strings.Contains(err.Error(), "requestBody takes an object") {
		t.Errorf("err=%v; want the body-arg name in the message", err)
	}
	if _, still := args["requestBody"]; !still {
		t.Error("args lost requestBody; a refused fold must not mutate the args")
	}
}

// TestOnEmptyMessageOfReturnsConfiguredMessage pins the non-nil arm of
// onEmptyMessageOf (handlers.go:973). The profile's on_empty string is
// what the text block shows instead of a bare empty list.
func TestOnEmptyMessageOfReturnsConfiguredMessage(t *testing.T) {
	msg := "no rows matched"
	shaped := &dispatch.ShapedResponse{
		Expression: &dispatch.ExpressionMeta{OnEmptyMessage: &msg},
	}
	if got := onEmptyMessageOf(shaped); got != msg {
		t.Errorf("onEmptyMessageOf=%q; want %q", got, msg)
	}
}

// TestStructuredJSONResultPropagatesMarshalError pins the IsError
// short-circuit in structuredJSONResult (handlers.go:1111-1114). A
// value json cannot encode must come back as the error result, not as
// a success carrying nil structuredContent.
func TestStructuredJSONResultPropagatesMarshalError(t *testing.T) {
	res := structuredJSONResult(make(chan int))
	if res == nil || !res.IsError {
		t.Fatalf("structuredJSONResult(chan)=%+v; want IsError", res)
	}
	if res.StructuredContent != nil {
		t.Errorf("StructuredContent=%v; want nil on the error path", res.StructuredContent)
	}
}

// TestHandleGainRejectsUnresolvableLedgerPath pins handleGain's
// GAIN_LEDGER_UNAVAILABLE arm (handlers.go:631-637). gain.DefaultPath
// resolves <XDG_DATA_HOME>/gum/<profile> and falls back to $HOME; with
// neither set it cannot name a ledger, and the handler MUST return the
// terminal error rather than an empty savings report.
func TestHandleGainRejectsUnresolvableLedgerPath(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("HOME", "")

	s := &Server{profile: "default"}
	res, err := s.handleGain(context.Background(), nil)
	if err != nil {
		t.Fatalf("handleGain err=%v; want the envelope result", err)
	}
	if res == nil || !res.IsError {
		t.Fatalf("handleGain res=%+v; want IsError", res)
	}
	if body := resultText(t, res); !strings.Contains(body, "GAIN_LEDGER_UNAVAILABLE") {
		t.Errorf("body=%q; want GAIN_LEDGER_UNAVAILABLE", body)
	}
}

// TestProfileNameForRequestUsesOverrideBindings pins both binding hits
// in profileNameForRequest (handlers.go:889-896). A variant pin wins
// over the op-level binding, which is what spec §9.2 orders.
func TestProfileNameForRequestUsesOverrideBindings(t *testing.T) {
	// Isolate the user layer so only the project file contributes.
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	root := t.TempDir()
	dir := filepath.Join(root, ".gum", "profiles")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir profiles: %v", err)
	}
	body := "[override_bindings]\n" +
		"\"drive.files.list\" = \"op_profile\"\n" +
		"\"drive.files.list@v2\" = \"variant_profile\"\n"
	if err := os.WriteFile(filepath.Join(dir, "bindings.toml"), []byte(body), 0o600); err != nil {
		t.Fatalf("write bindings: %v", err)
	}

	s := &Server{}
	variantPinned := &dispatch.Invocation{
		OpID:               "drive.files.list",
		RequestedVariantID: "drive.files.list@v2",
	}
	if got := s.profileNameForRequest(root, variantPinned); got != "variant_profile" {
		t.Errorf("variant binding=%q; want variant_profile", got)
	}

	opOnly := &dispatch.Invocation{OpID: "drive.files.list"}
	if got := s.profileNameForRequest(root, opOnly); got != "op_profile" {
		t.Errorf("op binding=%q; want op_profile", got)
	}
}

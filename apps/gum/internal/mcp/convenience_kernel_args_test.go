package mcp

// gum-n1gi acceptance: a convenience tool call made with the advertised
// arguments must reach the upstream request with those values mapped to the
// op's real params. These tests drive the handler through the real dispatch
// kernel — not a stub dispatcher — over the ops' real RequestFields, and assert
// the args the executor receives.

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/ehmo/gum/internal/adapters"
	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/dispatch"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

const convenienceTestAdapterKey = "convenience.args.test"

// argCaptureAdapter records the Invocation the kernel hands the executor, after
// validateParams and body assembly have run.
type argCaptureAdapter struct {
	last *dispatch.Invocation
}

func (a *argCaptureAdapter) Execute(_ context.Context, inv *dispatch.Invocation, _ *dispatch.ResolvedVariant, _ *dispatch.Credentials) (*dispatch.Response, error) {
	a.last = inv
	return &dispatch.Response{
		Body:       json.RawMessage(`{"ok":true}`),
		Format:     "json",
		StatusCode: 200,
	}, nil
}

// catalogWithRealFields lifts the named ops out of the embedded catalog so the
// kernel sees their real RequestFields, then repoints each default variant at
// the capture adapter and drops the auth requirement so the test stays
// hermetic. Nothing that drives argument validation or body assembly changes.
func catalogWithRealFields(t *testing.T, opIDs ...string) *catalog.Catalog {
	t.Helper()
	src := embeddedCatalogForTest(t)
	ops := make([]catalog.Op, 0, len(opIDs))
	for _, id := range opIDs {
		found := opByIDInCatalog(src, id)
		if found == nil {
			t.Skipf("op %s absent from this catalog build", id)
		}
		op := *found
		variants := make([]catalog.Variant, 0, len(op.Variants))
		for _, v := range op.Variants {
			if v.VariantID != op.DefaultVariantID {
				continue
			}
			v.AuthStrategy = catalog.AuthStrategyNone
			v.Scopes = nil
			b := *v.Binding
			b.AdapterKey = convenienceTestAdapterKey
			v.Binding = &b
			variants = append(variants, v)
		}
		op.Variants = variants
		ops = append(ops, op)
	}
	return minimalCatalog(ops...)
}

func callConvenience(t *testing.T, srv *Server, tool string, args map[string]any) *sdkmcp.CallToolResult {
	t.Helper()
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}
	res, err := srv.makeConvenienceHandler(tool)(context.Background(), &sdkmcp.CallToolRequest{
		Params: &sdkmcp.CallToolParamsRaw{Name: tool, Arguments: raw},
	})
	if err != nil {
		t.Fatalf("%s handler: %v", tool, err)
	}
	return res
}

// TestConvenienceReadArgsReachExecutor: gmail_search advertises `q`, the name
// gmail.users.messages.list declares. Before gum-n1gi it advertised `query`,
// which the kernel rejected as unknown before any upstream request.
func TestConvenienceReadArgsReachExecutor(t *testing.T) {
	snap := catalogWithRealFields(t, "gmail.users.messages.list")
	cap := &argCaptureAdapter{}
	disp := dispatch.NewDispatcher(snap, map[string]dispatch.Adapter{convenienceTestAdapterKey: cap})
	srv := NewServerWithCatalog(disp, snap)

	res := callConvenience(t, srv, "gmail_search", map[string]any{
		"userId":     "me",
		"q":          "from:alice",
		"maxResults": 10,
		"format":     "json",
	})
	if res.IsError {
		t.Fatalf("gmail_search returned an error: %s", firstText(res))
	}
	if cap.last == nil {
		t.Fatal("executor never ran")
	}
	if got := cap.last.Args["userId"]; got != "me" {
		t.Errorf("userId = %v; want \"me\"", got)
	}
	if got := cap.last.Args["q"]; got != "from:alice" {
		t.Errorf("q = %v; want \"from:alice\"", got)
	}
	if _, leaked := cap.last.Args["format"]; leaked {
		t.Error("the output-format control reached the op args")
	}
	if cap.last.Format != "json" {
		t.Errorf("inv.Format = %q; want \"json\"", cap.last.Format)
	}
}

// TestConvenienceWriteBodyArgReachesExecutor: gmail_send advertises the Message
// object under `message` plus a top-level `threadId`. The kernel wants both
// inside the reserved "body" key, because gmail.users.messages.send declares
// `raw` and `threadId` as location=body fields.
func TestConvenienceWriteBodyArgReachesExecutor(t *testing.T) {
	snap := catalogWithRealFields(t, "gmail.users.messages.send")
	cap := &argCaptureAdapter{}
	disp := dispatch.NewDispatcher(snap, map[string]dispatch.Adapter{convenienceTestAdapterKey: cap})
	srv := NewServerWithCatalog(disp, snap)

	args := map[string]any{
		"userId":   "me",
		"message":  map[string]any{"raw": "UmF3IE1JTUU="},
		"threadId": "t-42",
	}
	first := callConvenience(t, srv, "gmail_send", args)
	var env map[string]any
	if err := json.Unmarshal([]byte(firstText(first)), &env); err != nil {
		t.Fatalf("confirmation envelope is not JSON: %v\n%s", err, firstText(first))
	}
	token, _ := env["confirmation_token"].(string)
	if token == "" {
		t.Fatalf("no confirmation_token in %s", firstText(first))
	}

	args["confirmed"] = true
	args["confirmation_token"] = token
	res := callConvenience(t, srv, "gmail_send", args)
	if res.IsError {
		t.Fatalf("confirmed gmail_send returned an error: %s", firstText(res))
	}
	if cap.last == nil {
		t.Fatal("executor never ran")
	}
	if got := cap.last.Args["userId"]; got != "me" {
		t.Errorf("userId = %v; want \"me\"", got)
	}
	body, ok := cap.last.Args[adapters.BodyArgKey].(map[string]any)
	if !ok {
		t.Fatalf("args[%q] = %#v; want a map", adapters.BodyArgKey, cap.last.Args[adapters.BodyArgKey])
	}
	if got := body["raw"]; got != "UmF3IE1JTUU=" {
		t.Errorf("body.raw = %v; want the value passed under message.raw", got)
	}
	if got := body["threadId"]; got != "t-42" {
		t.Errorf("body.threadId = %v; want \"t-42\"", got)
	}
	if _, leaked := cap.last.Args["message"]; leaked {
		t.Error("the advertised body arg reached the op args unmapped")
	}
	if _, leaked := cap.last.Args["threadId"]; leaked {
		t.Error("threadId stayed at the top level instead of folding into body")
	}
}

// TestConvenienceBodyFieldArgReachesExecutor: sheets_write advertises `values`
// under its own name, and the kernel wants {"body": {"values": ...}}.
func TestConvenienceBodyFieldArgReachesExecutor(t *testing.T) {
	snap := catalogWithRealFields(t, "sheets.spreadsheets.values.update")
	cap := &argCaptureAdapter{}
	disp := dispatch.NewDispatcher(snap, map[string]dispatch.Adapter{convenienceTestAdapterKey: cap})
	srv := NewServerWithCatalog(disp, snap)

	args := map[string]any{
		"spreadsheetId":    "sheet-1",
		"range":            "A1:B2",
		"values":           []any{[]any{"a", "b"}},
		"valueInputOption": "RAW",
	}
	first := callConvenience(t, srv, "sheets_write", args)
	var env map[string]any
	if err := json.Unmarshal([]byte(firstText(first)), &env); err != nil {
		t.Fatalf("confirmation envelope is not JSON: %v\n%s", err, firstText(first))
	}
	token, _ := env["confirmation_token"].(string)
	if token == "" {
		t.Fatalf("no confirmation_token in %s", firstText(first))
	}

	args["confirmed"] = true
	args["confirmation_token"] = token
	res := callConvenience(t, srv, "sheets_write", args)
	if res.IsError {
		t.Fatalf("confirmed sheets_write returned an error: %s", firstText(res))
	}
	body, ok := cap.last.Args[adapters.BodyArgKey].(map[string]any)
	if !ok {
		t.Fatalf("args[%q] = %#v; want a map", adapters.BodyArgKey, cap.last.Args[adapters.BodyArgKey])
	}
	if _, has := body["values"]; !has {
		t.Errorf("body has no values key: %#v", body)
	}
	if got := cap.last.Args["valueInputOption"]; got != "RAW" {
		t.Errorf("valueInputOption = %v; want \"RAW\" at the top level", got)
	}
}

// TestConvenienceHandlerRejectsUnadvertisedArg proves the roster now runs behind
// validatedHandler: an argument the schema does not declare is refused before
// the handler builds an invocation, so no upstream request is made.
func TestConvenienceHandlerRejectsUnadvertisedArg(t *testing.T) {
	snap := catalogWithRealFields(t, "gmail.users.messages.list")
	cap := &argCaptureAdapter{}
	disp := dispatch.NewDispatcher(snap, map[string]dispatch.Adapter{convenienceTestAdapterKey: cap})
	srv := NewServerWithCatalog(disp, snap)

	res := callConvenience(t, srv, "gmail_search", map[string]any{
		"userId": "me",
		"query":  "from:alice", // the pre-gum-n1gi name; no longer advertised
	})
	if !res.IsError {
		t.Fatalf("unadvertised arg was accepted: %s", firstText(res))
	}
	if cap.last != nil {
		t.Error("the executor ran for a call the schema rejects")
	}
	var env map[string]any
	if err := json.Unmarshal([]byte(firstText(res)), &env); err != nil {
		t.Fatalf("rejection is not a JSON envelope: %v\n%s", err, firstText(res))
	}
	if env["error_code"] != string(dispatch.ErrCodeInvalidArgs) {
		t.Errorf("error_code = %v; want %s", env["error_code"], dispatch.ErrCodeInvalidArgs)
	}
}

// TestConvenienceHandlerRejectsMissingRequiredArg: gmail_search cannot build its
// URL without userId, and the schema now says so, so the gate refuses the call
// rather than letting the kernel emit a late INVALID_ARGS.
func TestConvenienceHandlerRejectsMissingRequiredArg(t *testing.T) {
	snap := catalogWithRealFields(t, "gmail.users.messages.list")
	cap := &argCaptureAdapter{}
	disp := dispatch.NewDispatcher(snap, map[string]dispatch.Adapter{convenienceTestAdapterKey: cap})
	srv := NewServerWithCatalog(disp, snap)

	res := callConvenience(t, srv, "gmail_search", map[string]any{"q": "from:alice"})
	if !res.IsError {
		t.Fatalf("call without userId was accepted: %s", firstText(res))
	}
	if cap.last != nil {
		t.Error("the executor ran for a call missing a required path param")
	}
}

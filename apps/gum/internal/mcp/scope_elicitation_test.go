package mcp

// Spec §13 "managed-scope re-consent", MCP half (docs/test-matrix.md).
//
// The contract this file pins: a SCOPE_MISSING refusal becomes ONE elicitation
// form when the client declares the capability; the form carries every binding
// the kernel minted; an approval is accepted only when it echoes those
// bindings back unchanged; and a grant reports SCOPE_GRANTED without re-running
// the operation the refusal blocked.

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/dispatch"
)

const (
	elicitOpID    = "test.scoped.read"
	elicitVariant = "test.scoped.read.v1"
	elicitProfile = "work"
	elicitScope   = "https://www.googleapis.com/auth/drive.readonly"
)

// countingAdapter reports how many times the executor actually ran. §13 forbids
// the auto-retry, so the count is what proves the grant did not replay the op.
type countingAdapter struct{ calls atomic.Int64 }

func (a *countingAdapter) Execute(context.Context, *dispatch.Invocation, *dispatch.ResolvedVariant, *dispatch.Credentials) (*dispatch.Response, error) {
	a.calls.Add(1)
	return &dispatch.Response{Body: []byte(`{"ok":true}`)}, nil
}

// elicitCatalog is one read op behind one byo_oauth scope.
func elicitCatalog() *catalog.Catalog {
	return minimalCatalog(catalog.Op{
		OpID:             elicitOpID,
		OpSchemaVersion:  1,
		Title:            "Scoped read",
		Summary:          "Used by the §13 elicitation tests.",
		DefaultVariantID: elicitVariant,
		Variants: []catalog.Variant{{
			VariantID:     elicitVariant,
			Stability:     catalog.StabilityStable,
			InterfaceKind: catalog.InterfaceKindSDKNative,
			BackendKind:   catalog.BackendKindTypedRestSDK,
			RiskClass:     catalog.RiskClassRead,
			AuthStrategy:  catalog.AuthStrategyBYOOAuth,
			Scopes:        []string{elicitScope},
			Binding: &catalog.Binding{
				BindingSchemaVersion: 1,
				AdapterKey:           "test.adapter",
				OperationKey:         elicitVariant + ".exec",
			},
		}},
	})
}

// newElicitServer builds a server over a real kernel with the §13 consent
// wired. The profile starts with no granted scopes, so the op is refused.
func newElicitServer(login dispatch.ScopeUpgradeLogin) (*Server, *countingAdapter) {
	adapter := &countingAdapter{}
	snapshot := elicitCatalog()
	disp := dispatch.NewDispatcherWithConfig(snapshot, map[string]dispatch.Adapter{"test.adapter": adapter}, dispatch.DispatcherConfig{
		ProfileName:       elicitProfile,
		ScopeUpgradeLogin: login,
	})
	return NewServerWithCatalog(disp, snapshot), adapter
}

// grantingElicitLogin returns a login that grants elicitScope and counts runs.
func grantingElicitLogin(runs *atomic.Int64) dispatch.ScopeUpgradeLogin {
	return func(context.Context, dispatch.ScopeUpgradeRequest) (dispatch.ScopeUpgradeGrant, error) {
		runs.Add(1)
		return dispatch.ScopeUpgradeGrant{GrantedScopes: []string{elicitScope}}, nil
	}
}

// elicitReq builds a tool call carrying the client's elicitation capability and
// any input responses. Capabilities travel in `_meta` under MCP 2026-07-28.
func elicitReq(withCap bool, responses sdkmcp.InputResponseMap) *sdkmcp.CallToolRequest {
	params := &sdkmcp.CallToolParamsRaw{InputResponses: responses}
	if withCap {
		params.Meta = sdkmcp.Meta{
			sdkmcp.MetaKeyClientCapabilities: map[string]any{"elicitation": map[string]any{}},
		}
	}
	return &sdkmcp.CallToolRequest{Session: new(sdkmcp.ServerSession), Params: params}
}

// elicitInv is the invocation the scoped op is called with.
func elicitInv() *dispatch.Invocation {
	return &dispatch.Invocation{OpID: elicitOpID, Args: map[string]any{"q": "report"}}
}

// askForm runs one dispatch that must produce the approval form, and returns
// the form's input request id and params.
func askForm(t *testing.T, s *Server) (string, *sdkmcp.ElicitParams) {
	t.Helper()

	res, err := s.dispatchAndShapeForRequest(context.Background(), elicitReq(true, nil), elicitInv())
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if len(res.InputRequests) != 1 {
		t.Fatalf("result carries %d input requests; §13 asks once", len(res.InputRequests))
	}
	for id, ir := range res.InputRequests {
		params, ok := ir.(*sdkmcp.ElicitParams)
		if !ok {
			t.Fatalf("input request is %T; want *sdkmcp.ElicitParams", ir)
		}
		return id, params
	}
	return "", nil
}

// formBindings reads the read-only binding defaults out of an approval form.
func formBindings(t *testing.T, params *sdkmcp.ElicitParams) map[string]string {
	t.Helper()

	schema, ok := params.RequestedSchema.(map[string]any)
	if !ok {
		t.Fatalf("RequestedSchema is %T; want map[string]any", params.RequestedSchema)
	}
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("schema properties are %T; want map[string]any", schema["properties"])
	}
	out := map[string]string{}
	for name, raw := range properties {
		prop, ok := raw.(map[string]any)
		if !ok {
			t.Fatalf("property %q is %T; want map[string]any", name, raw)
		}
		if def, ok := prop["default"].(string); ok {
			out[name] = def
		}
	}
	return out
}

// approval builds an accepted reply echoing every binding the form carried.
func approval(bindings map[string]string, approve bool) *sdkmcp.ElicitResult {
	content := map[string]any{"approve": approve}
	for k, v := range bindings {
		content[k] = v
	}
	return &sdkmcp.ElicitResult{Action: "accept", Content: content}
}

// replyWith re-dispatches carrying res as the answer to id.
func replyWith(t *testing.T, s *Server, id string, res *sdkmcp.ElicitResult) *sdkmcp.CallToolResult {
	t.Helper()

	out, err := s.dispatchAndShapeForRequest(context.Background(), elicitReq(true, sdkmcp.InputResponseMap{id: res}), elicitInv())
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	return out
}

// requireScopeMissing asserts the result is the unchanged §13 refusal envelope.
func requireScopeMissing(t *testing.T, res *sdkmcp.CallToolResult) {
	t.Helper()

	if len(res.InputRequests) > 0 {
		t.Fatalf("result asks again with %d input requests; a refused approval must not re-prompt", len(res.InputRequests))
	}
	env := parseErrorResult(t, res)
	if env["error_code"] != string(dispatch.ErrCodeScopeMissing) {
		t.Fatalf("error_code = %v; want %q", env["error_code"], dispatch.ErrCodeScopeMissing)
	}
}

func TestElicitationScopeUpgradeBinding(t *testing.T) {
	t.Run("a SCOPE_MISSING refusal becomes one bound approval form", func(t *testing.T) {
		var runs atomic.Int64
		s, adapter := newElicitServer(grantingElicitLogin(&runs))

		id, params := askForm(t, s)

		if !strings.HasPrefix(id, scopeUpgradeIDPrefix) {
			t.Errorf("input request id = %q; want the %q namespace", id, scopeUpgradeIDPrefix)
		}
		if params.Mode != "form" {
			t.Errorf("elicitation mode = %q; want form", params.Mode)
		}

		bindings := formBindings(t, params)
		want := map[string]string{
			"op_id":           elicitOpID,
			"variant_id":      elicitVariant,
			"profile":         elicitProfile,
			"required_scopes": elicitScope,
		}
		for k, v := range want {
			if bindings[k] != v {
				t.Errorf("form binding %s = %q; want %q", k, bindings[k], v)
			}
		}
		if bindings["request_hash"] == "" {
			t.Error("the form carries no request_hash; the approval would bind to nothing")
		}
		if got := strings.TrimPrefix(id, scopeUpgradeIDPrefix); got != bindings["request_hash"] {
			t.Errorf("input request id carries hash %q; the form binds %q", got, bindings["request_hash"])
		}

		schema := params.RequestedSchema.(map[string]any)
		required, _ := schema["required"].([]string)
		if len(required) != 1 || required[0] != "approve" {
			t.Errorf("schema requires %v; only approve may be required, because the SDK validates a reply before it applies defaults", required)
		}
		if !strings.Contains(params.Message, elicitScope) {
			t.Error("the prompt does not name the scopes being approved")
		}

		if runs.Load() != 0 {
			t.Errorf("the consent ran %d times before anyone approved it", runs.Load())
		}
		if adapter.calls.Load() != 0 {
			t.Errorf("the executor ran %d times behind a SCOPE_MISSING refusal", adapter.calls.Load())
		}
	})

	t.Run("an approved form grants without re-running the operation", func(t *testing.T) {
		var runs atomic.Int64
		s, adapter := newElicitServer(grantingElicitLogin(&runs))
		id, params := askForm(t, s)
		bindings := formBindings(t, params)

		res := replyWith(t, s, id, approval(bindings, true))

		if res.IsError {
			t.Fatalf("an approved, fully bound form produced an error result: %v", parseErrorResult(t, res))
		}
		if len(res.InputRequests) != 0 {
			t.Fatal("the grant asks another question; §13 ends the exchange")
		}
		text, ok := res.Content[0].(*sdkmcp.TextContent)
		if !ok {
			t.Fatalf("content[0] is %T; want *sdkmcp.TextContent", res.Content[0])
		}
		var outcome map[string]any
		if err := json.Unmarshal([]byte(text.Text), &outcome); err != nil {
			t.Fatalf("outcome is not JSON: %v; text: %s", err, text.Text)
		}
		if outcome["status"] != dispatch.ScopeUpgradeStatusGranted {
			t.Errorf("status = %v; want %q", outcome["status"], dispatch.ScopeUpgradeStatusGranted)
		}
		if outcome["op_id"] != elicitOpID {
			t.Errorf("op_id = %v; want %q", outcome["op_id"], elicitOpID)
		}
		if outcome["request_hash"] != bindings["request_hash"] {
			t.Errorf("request_hash = %v; want the approved %q", outcome["request_hash"], bindings["request_hash"])
		}
		if runs.Load() != 1 {
			t.Errorf("the consent ran %d times; want exactly 1", runs.Load())
		}
		if adapter.calls.Load() != 0 {
			t.Fatalf("the executor ran %d times; §13 grants the scope and stops, it does not replay the call", adapter.calls.Load())
		}

		// The caller re-issues the operation itself. It must now get through.
		out, err := s.dispatchAndShapeForRequest(context.Background(), elicitReq(true, nil), elicitInv())
		if err != nil {
			t.Fatalf("re-issued dispatch: %v", err)
		}
		if out.IsError {
			t.Fatalf("the re-issued call still fails: %v", parseErrorResult(t, out))
		}
		if adapter.calls.Load() != 1 {
			t.Errorf("executor ran %d times on the re-issued call; want 1", adapter.calls.Load())
		}
	})

	t.Run("an approval minted for another call is refused", func(t *testing.T) {
		var runs atomic.Int64
		s, adapter := newElicitServer(grantingElicitLogin(&runs))
		id, params := askForm(t, s)
		bindings := formBindings(t, params)
		bindings["request_hash"] = strings.Repeat("0", 64)

		requireScopeMissing(t, replyWith(t, s, id, approval(bindings, true)))

		if runs.Load() != 0 {
			t.Errorf("the consent ran %d times on a mismatched binding", runs.Load())
		}
		if adapter.calls.Load() != 0 {
			t.Errorf("the executor ran %d times", adapter.calls.Load())
		}
	})

	t.Run("an approval naming another op is refused", func(t *testing.T) {
		var runs atomic.Int64
		s, _ := newElicitServer(grantingElicitLogin(&runs))
		id, params := askForm(t, s)
		bindings := formBindings(t, params)
		bindings["op_id"] = "some.other.op"

		requireScopeMissing(t, replyWith(t, s, id, approval(bindings, true)))

		if runs.Load() != 0 {
			t.Errorf("the consent ran %d times for an approval naming another op", runs.Load())
		}
	})

	t.Run("a refusal leaves the original envelope in place", func(t *testing.T) {
		cases := []struct {
			name  string
			reply func(map[string]string) *sdkmcp.ElicitResult
		}{
			{"decline", func(map[string]string) *sdkmcp.ElicitResult { return &sdkmcp.ElicitResult{Action: "decline"} }},
			{"cancel", func(map[string]string) *sdkmcp.ElicitResult { return &sdkmcp.ElicitResult{Action: "cancel"} }},
			{"approve=false", func(b map[string]string) *sdkmcp.ElicitResult { return approval(b, false) }},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				var runs atomic.Int64
				s, adapter := newElicitServer(grantingElicitLogin(&runs))
				id, params := askForm(t, s)

				requireScopeMissing(t, replyWith(t, s, id, tc.reply(formBindings(t, params))))

				if runs.Load() != 0 {
					t.Errorf("the consent ran %d times after a %s", runs.Load(), tc.name)
				}
				if adapter.calls.Load() != 0 {
					t.Errorf("the executor ran %d times after a %s", adapter.calls.Load(), tc.name)
				}
			})
		}
	})

	t.Run("a reply under another id is not read as the approval", func(t *testing.T) {
		var runs atomic.Int64
		s, _ := newElicitServer(grantingElicitLogin(&runs))
		id, params := askForm(t, s)
		bindings := formBindings(t, params)

		// Same content, wrong key. Asking again would spin the SDK's retry
		// loop, so the flow takes it as a refusal.
		other := sdkmcp.InputResponseMap{id + ".stale": approval(bindings, true)}
		res, err := s.dispatchAndShapeForRequest(context.Background(), elicitReq(true, other), elicitInv())
		if err != nil {
			t.Fatalf("dispatch: %v", err)
		}
		requireScopeMissing(t, res)
		if runs.Load() != 0 {
			t.Errorf("the consent ran %d times for a reply under another id", runs.Load())
		}
	})

	t.Run("a client that cannot elicit is never asked", func(t *testing.T) {
		var runs atomic.Int64
		s, _ := newElicitServer(grantingElicitLogin(&runs))

		res, err := s.dispatchAndShapeForRequest(context.Background(), elicitReq(false, nil), elicitInv())
		if err != nil {
			t.Fatalf("dispatch: %v", err)
		}
		requireScopeMissing(t, res)
		if runs.Load() != 0 {
			t.Errorf("the consent ran %d times without an elicitation capability", runs.Load())
		}
	})

	t.Run("a kernel with no consent wired never asks", func(t *testing.T) {
		s, _ := newElicitServer(nil)

		res, err := s.dispatchAndShapeForRequest(context.Background(), elicitReq(true, nil), elicitInv())
		if err != nil {
			t.Fatalf("dispatch: %v", err)
		}
		requireScopeMissing(t, res)
	})

	t.Run("a request-less dispatch path never asks", func(t *testing.T) {
		var runs atomic.Int64
		s, _ := newElicitServer(grantingElicitLogin(&runs))

		res, err := s.dispatchAndShape(context.Background(), elicitInv())
		if err != nil {
			t.Fatalf("dispatch: %v", err)
		}
		requireScopeMissing(t, res)
	})
}

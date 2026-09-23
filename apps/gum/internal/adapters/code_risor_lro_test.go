package adapters_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/adapters"
	"github.com/ehmo/gum/internal/dispatch"
)

// lroDispatcher is a mockDispatcher that also answers the optional
// dispatch.LROClassifier capability, so the code-mode gate can see the
// classification the way the real kernel exposes it.
type lroDispatcher struct {
	mockDispatcher
	lro map[string]bool
}

func (d *lroDispatcher) ReturnsLRO(opID string) bool { return d.lro[opID] }

// TestLROUnsupportedInCode is the spec §6.1 acceptance (matrix row 242):
// gum_call refuses an op whose default variant is classified lro_return
// before dispatch, and returns the documented envelope.
func TestLROUnsupportedInCode(t *testing.T) {
	dispatched := false
	d := &lroDispatcher{
		mockDispatcher: mockDispatcher{fn: func(context.Context, *dispatch.Invocation) (*dispatch.ShapedResponse, error) {
			dispatched = true
			return &dispatch.ShapedResponse{Format: "json", StructuredContent: map[string]any{"ok": true}}, nil
		}},
		lro: map[string]bool{"cloudidentity.groups.create": true},
	}

	cr := adapters.NewCodeRunner().WithDispatcher(d)
	inv := &dispatch.Invocation{
		OpID: "gum.code",
		Args: map[string]any{
			"language": "risor",
			"source":   `gum_call("cloudidentity.groups.create", {})`,
		},
	}

	_, err := cr.Execute(context.Background(), inv, minimalCodeVariant(), nil)
	if err == nil {
		t.Fatal("Execute succeeded; want the LRO refusal")
	}
	if dispatched {
		t.Fatal("the kernel was called; the refusal must happen before dispatch")
	}

	var se *dispatch.StructuredError
	if !errors.As(err, &se) {
		t.Fatalf("err = %T %v; want *dispatch.StructuredError", err, err)
	}
	if se.ErrCode != dispatch.ErrCodeLROUnsupportedInCode {
		t.Fatalf("ErrCode = %q; want %q", se.ErrCode, dispatch.ErrCodeLROUnsupportedInCode)
	}
	if se.Detail["op_id"] != "cloudidentity.groups.create" {
		t.Fatalf("op_id detail = %v; want cloudidentity.groups.create", se.Detail["op_id"])
	}
	const want = "long-running operations are not callable from gum.code; run the op through the CLI or the matching gum.read / gum.write / gum.destructive tool, then poll it with gum.poll"
	if se.Message != want {
		t.Fatalf("Message = %q; want %q", se.Message, want)
	}
}

// TestLROGateLetsAnOrdinaryOpThrough pins the gate's negative arm: an op the
// classifier does not mark reaches the kernel unchanged.
func TestLROGateLetsAnOrdinaryOpThrough(t *testing.T) {
	d := &lroDispatcher{
		mockDispatcher: mockDispatcher{fn: func(context.Context, *dispatch.Invocation) (*dispatch.ShapedResponse, error) {
			return &dispatch.ShapedResponse{Format: "json", StructuredContent: map[string]any{"value": "through"}}, nil
		}},
		lro: map[string]bool{"cloudidentity.groups.create": true},
	}

	cr := adapters.NewCodeRunner().WithDispatcher(d)
	inv := &dispatch.Invocation{
		OpID: "gum.code",
		Args: map[string]any{
			"language": "risor",
			"source": `let r = gum_call("gmail.users.messages.list", {})
gum_print(r["value"])`,
		},
	}
	resp, err := cr.Execute(context.Background(), inv, minimalCodeVariant(), nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got := strings.TrimSpace(string(resp.Body)); got != "through" {
		t.Fatalf("printed = %q; want through", got)
	}
}

// TestLROGateTolerantOfAClassifierlessDispatcher pins the optional-capability
// contract: a dispatcher that does not answer LROClassifier dispatches as
// before rather than refusing everything.
func TestLROGateTolerantOfAClassifierlessDispatcher(t *testing.T) {
	mock := &mockDispatcher{fn: func(context.Context, *dispatch.Invocation) (*dispatch.ShapedResponse, error) {
		return &dispatch.ShapedResponse{Format: "json", StructuredContent: map[string]any{"value": "plain"}}, nil
	}}

	got := execScript(t, context.Background(), mock, `
let r = gum_call("cloudidentity.groups.create", {})
gum_print(r["value"])
`)
	if got != "plain" {
		t.Fatalf("printed = %q; want plain", got)
	}
}

// TestLROUnsupportedInParallel pins the same refusal on the other code-mode
// host function: a batch element naming an LRO op fails with the code-mode
// error envelope instead of reaching the kernel.
func TestLROUnsupportedInParallel(t *testing.T) {
	d := &lroDispatcher{
		mockDispatcher: mockDispatcher{fn: func(_ context.Context, inv *dispatch.Invocation) (*dispatch.ShapedResponse, error) {
			if inv.OpID == "cloudidentity.groups.create" {
				t.Error("the kernel was called for the LRO element")
			}
			return &dispatch.ShapedResponse{Format: "json", StructuredContent: map[string]any{"value": "ok"}}, nil
		}},
		lro: map[string]bool{"cloudidentity.groups.create": true},
	}

	cr := adapters.NewCodeRunner().WithDispatcher(d)
	inv := &dispatch.Invocation{
		OpID: "gum.code",
		Args: map[string]any{
			"language": "risor",
			"source": `let env = gum_parallel([{op: "gmail.users.messages.list"}, {op: "cloudidentity.groups.create"}])
gum_print(env["results"][1]["error"]["error_code"])`,
		},
	}
	resp, err := cr.Execute(context.Background(), inv, minimalCodeVariant(), nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got := strings.TrimSpace(string(resp.Body)); got != string(dispatch.ErrCodeLROUnsupportedInCode) {
		t.Fatalf("element error_code = %q; want %q", got, dispatch.ErrCodeLROUnsupportedInCode)
	}
}

package adapters

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/dispatch"
)

// riskMismatch builds the RISK_TOOL_MISMATCH error the policy kernel returns
// when a probe call needs a higher-risk tool than the caller asked for.
func riskMismatch(requiredTool string) error {
	return dispatch.NewStructuredError(dispatch.ErrCodeRiskToolMismatch, "risk mismatch").
		WithDetail("required_tool", requiredTool)
}

// needsConfirmation builds the REQUIRES_CONFIRMATION error that carries a
// confirmation token back to gum_call.
func needsConfirmation(token string) error {
	err := dispatch.NewStructuredError(dispatch.ErrCodeRequiresConfirmation, "confirm first")
	if token != "" {
		err = err.WithDetail("confirmation_token", token)
	}
	return err
}

// errCodeOf reads the structured error code, or "" for a plain error.
func errCodeOf(t *testing.T, err error) string {
	t.Helper()
	var se *dispatch.StructuredError
	if !errors.As(err, &se) {
		return ""
	}
	return string(se.ErrCode)
}

func TestConfirmFnRefusesWhenDestructiveIsNotAllowed(t *testing.T) {
	fn := buildConfirmFn(false, true, &destructiveState{})

	_, err := fn("gmail.delete")
	if got := errCodeOf(t, err); got != string(dispatch.ErrCodeRequiresConfirmation) {
		t.Fatalf("error code = %q; want REQUIRES_CONFIRMATION (err=%v)", got, err)
	}
	if !strings.Contains(err.Error(), "allow_destructive is false") {
		t.Errorf("error = %v; want it to name allow_destructive", err)
	}
}

func TestConfirmFnRefusesWhenNotConfirmed(t *testing.T) {
	ds := &destructiveState{}
	fn := buildConfirmFn(true, false, ds)

	_, err := fn("gmail.delete")
	if got := errCodeOf(t, err); got != string(dispatch.ErrCodeRequiresConfirmation) {
		t.Fatalf("error code = %q; want REQUIRES_CONFIRMATION (err=%v)", got, err)
	}
	if ds.hasPending {
		t.Error("hasPending = true; a refused confirmation must not arm the gate")
	}
}

func TestParseCallInputRejections(t *testing.T) {
	cases := []struct {
		name string
		args []any
		want string
	}{
		{"no args", nil, "expected op_id"},
		{"non-string op_id", []any{42}, "op_id must be a string"},
		{"empty op_id", []any{""}, "op_id must be a string"},
		{"non-map args", []any{"gmail.list", 7}, "args must be a map"},
		{"non-string variant", []any{"gmail.list", map[string]any{}, 9}, "variant_id must be a string"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseCallInput(tc.args)
			if err == nil {
				t.Fatalf("parseCallInput(%v) error = nil; want %q", tc.args, tc.want)
			}
			if got := errCodeOf(t, err); got != string(dispatch.ErrCodeInvalidArgs) {
				t.Errorf("error code = %q; want INVALID_ARGS", got)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %v; want it to contain %q", err, tc.want)
			}
		})
	}
}

func TestParseCallInputAcceptsAVariantID(t *testing.T) {
	el, err := parseCallInput([]any{"gmail.list", map[string]any{"q": "x"}, "gmail.list.v2"})
	if err != nil {
		t.Fatalf("parseCallInput: %v", err)
	}
	if el.OpID != "gmail.list" || el.VariantID != "gmail.list.v2" || el.Args["q"] != "x" {
		t.Errorf("element = %+v; want op/args/variant preserved", el)
	}
}

func TestCallFnSurfacesAParseFailure(t *testing.T) {
	fn := buildCallFn(context.Background(), &whiteboxMockDispatcher{}, false, false, false, &destructiveState{})

	if _, err := fn(); err == nil {
		t.Fatal("gum_call() error = nil; want the parse rejection")
	}
}

func TestCallFnWithNoDispatcherStillChargesTheDestructiveGate(t *testing.T) {
	ds := &destructiveState{}
	fn := buildCallFn(context.Background(), nil, false, true, true, ds)

	// No gum_confirm_destructive ran, so the gate rejects before the
	// unsupported-capability error the nil dispatcher would otherwise raise.
	_, err := fn("gmail.delete")
	if got := errCodeOf(t, err); got != string(dispatch.ErrCodeRequiresConfirmation) {
		t.Fatalf("error code = %q; want REQUIRES_CONFIRMATION (err=%v)", got, err)
	}
}

func TestCallFnWithNoDispatcherReportsUnsupportedCapability(t *testing.T) {
	fn := buildCallFn(context.Background(), nil, false, false, false, &destructiveState{})

	_, err := fn("gmail.list")
	if got := errCodeOf(t, err); got != string(dispatch.ErrCodeUnsupportedCapability) {
		t.Fatalf("error code = %q; want UNSUPPORTED_CAPABILITY (err=%v)", got, err)
	}
}

func TestCallFnRetriesAWriteWithTheConfirmationToken(t *testing.T) {
	var sawToken string
	calls := 0
	disp := &whiteboxMockDispatcher{fn: func(_ context.Context, inv *dispatch.Invocation) (*dispatch.ShapedResponse, error) {
		calls++
		switch {
		case !inv.AllowWrite:
			return nil, riskMismatch("gum.write")
		case !inv.Confirmed:
			return nil, needsConfirmation("tok-1")
		default:
			sawToken = inv.ConfirmationToken
			return &dispatch.ShapedResponse{Body: []byte(`{"id":"m1"}`)}, nil
		}
	}}

	fn := buildCallFn(context.Background(), disp, true, false, true, &destructiveState{})
	got, err := fn("gmail.send", map[string]any{"to": "a@b.c"})
	if err != nil {
		t.Fatalf("gum_call: %v", err)
	}
	if sawToken != "tok-1" {
		t.Errorf("confirmation token = %q; want tok-1", sawToken)
	}
	if calls != 3 {
		t.Errorf("dispatch calls = %d; want 3 (probe, write, confirmed write)", calls)
	}
	m, ok := got.(map[string]any)
	if !ok || m["id"] != "m1" {
		t.Errorf("result = %#v; want the decoded body", got)
	}
}

func TestCallFnReturnsTheWriteErrorWhenNoTokenComesBack(t *testing.T) {
	disp := &whiteboxMockDispatcher{fn: func(_ context.Context, inv *dispatch.Invocation) (*dispatch.ShapedResponse, error) {
		if !inv.AllowWrite {
			return nil, riskMismatch("gum.write")
		}
		return nil, errors.New("upstream refused")
	}}

	fn := buildCallFn(context.Background(), disp, true, false, true, &destructiveState{})
	if _, err := fn("gmail.send"); err == nil || !strings.Contains(err.Error(), "upstream refused") {
		t.Fatalf("error = %v; want the upstream write failure", err)
	}
}

func TestCallFnRefusesAWriteWithoutTheCapability(t *testing.T) {
	disp := &whiteboxMockDispatcher{fn: func(_ context.Context, _ *dispatch.Invocation) (*dispatch.ShapedResponse, error) {
		return nil, riskMismatch("gum.write")
	}}

	fn := buildCallFn(context.Background(), disp, false, false, true, &destructiveState{})
	if got := errCodeOf(t, mustErr(t, fn, "gmail.send")); got != string(dispatch.ErrCodeRiskToolMismatch) {
		t.Errorf("error code = %q; want the RISK_TOOL_MISMATCH passed through", got)
	}
}

func TestCallFnRefusesAWriteWithoutConfirmation(t *testing.T) {
	disp := &whiteboxMockDispatcher{fn: func(_ context.Context, _ *dispatch.Invocation) (*dispatch.ShapedResponse, error) {
		return nil, riskMismatch("gum.write")
	}}

	fn := buildCallFn(context.Background(), disp, true, false, false, &destructiveState{})
	if got := errCodeOf(t, mustErr(t, fn, "gmail.send")); got != string(dispatch.ErrCodeRequiresConfirmation) {
		t.Errorf("error code = %q; want REQUIRES_CONFIRMATION", got)
	}
}

func TestCallFnRefusesADestructiveCallWithoutTheCapability(t *testing.T) {
	disp := &whiteboxMockDispatcher{fn: func(_ context.Context, _ *dispatch.Invocation) (*dispatch.ShapedResponse, error) {
		return nil, riskMismatch("gum.destructive")
	}}

	fn := buildCallFn(context.Background(), disp, false, false, true, &destructiveState{})
	if got := errCodeOf(t, mustErr(t, fn, "gmail.delete")); got != string(dispatch.ErrCodeRiskToolMismatch) {
		t.Errorf("error code = %q; want the RISK_TOOL_MISMATCH passed through", got)
	}
}

func TestCallFnRefusesADestructiveCallWithoutConfirmation(t *testing.T) {
	disp := &whiteboxMockDispatcher{fn: func(_ context.Context, _ *dispatch.Invocation) (*dispatch.ShapedResponse, error) {
		return nil, riskMismatch("gum.destructive")
	}}

	fn := buildCallFn(context.Background(), disp, false, true, false, &destructiveState{})
	err := mustErr(t, fn, "gmail.delete")
	if got := errCodeOf(t, err); got != string(dispatch.ErrCodeRequiresConfirmation) {
		t.Fatalf("error code = %q; want REQUIRES_CONFIRMATION (err=%v)", got, err)
	}
	if !strings.Contains(err.Error(), "confirmed=true") {
		t.Errorf("error = %v; want it to name confirmed=true", err)
	}
}

// armedState returns a destructiveState that has already passed
// gum_confirm_destructive for opID with one unit of budget.
func armedState(opID string) *destructiveState {
	return &destructiveState{budget: 1, hasPending: true, pendingOpID: opID}
}

func TestCallFnSurfacesTheDestructiveProbeError(t *testing.T) {
	disp := &whiteboxMockDispatcher{fn: func(_ context.Context, inv *dispatch.Invocation) (*dispatch.ShapedResponse, error) {
		if !inv.AllowDestructive {
			return nil, riskMismatch("gum.destructive")
		}
		return nil, errors.New("upstream refused")
	}}

	fn := buildCallFn(context.Background(), disp, false, true, true, armedState("gmail.delete"))
	if err := mustErr(t, fn, "gmail.delete"); !strings.Contains(err.Error(), "upstream refused") {
		t.Fatalf("error = %v; want the upstream destructive failure", err)
	}
}

func TestCallFnRejectsADestructiveCallThatReturnsNoToken(t *testing.T) {
	disp := &whiteboxMockDispatcher{fn: func(_ context.Context, inv *dispatch.Invocation) (*dispatch.ShapedResponse, error) {
		if !inv.AllowDestructive {
			return nil, riskMismatch("gum.destructive")
		}
		// The kernel answered the destructive attempt without demanding a
		// confirmation token, so gum_call has nothing to replay.
		return &dispatch.ShapedResponse{Body: []byte(`{}`)}, nil
	}}

	fn := buildCallFn(context.Background(), disp, false, true, true, armedState("gmail.delete"))
	err := mustErr(t, fn, "gmail.delete")
	if got := errCodeOf(t, err); got != string(dispatch.ErrCodeRequiresConfirmation) {
		t.Fatalf("error code = %q; want REQUIRES_CONFIRMATION (err=%v)", got, err)
	}
	if !strings.Contains(err.Error(), "did not return a confirmation token") {
		t.Errorf("error = %v; want it to name the missing token", err)
	}
}

func TestCallFnRetriesADestructiveCallWithTheToken(t *testing.T) {
	var sawToken string
	disp := &whiteboxMockDispatcher{fn: func(_ context.Context, inv *dispatch.Invocation) (*dispatch.ShapedResponse, error) {
		switch {
		case !inv.AllowDestructive:
			return nil, riskMismatch("gum.destructive")
		case !inv.Confirmed:
			return nil, needsConfirmation("tok-d")
		default:
			sawToken = inv.ConfirmationToken
			return &dispatch.ShapedResponse{StructuredContent: map[string]any{"deleted": true}}, nil
		}
	}}

	ds := armedState("gmail.delete")
	fn := buildCallFn(context.Background(), disp, false, true, true, ds)
	got, err := fn("gmail.delete")
	if err != nil {
		t.Fatalf("gum_call: %v", err)
	}
	if sawToken != "tok-d" {
		t.Errorf("confirmation token = %q; want tok-d", sawToken)
	}
	if ds.budget != 0 {
		t.Errorf("budget = %d; want 0 after one destructive call", ds.budget)
	}
	m, ok := got.(map[string]any)
	if !ok || m["deleted"] != true {
		t.Errorf("result = %#v; want the structured content", got)
	}
}

func TestCallFnPassesThroughANonRiskError(t *testing.T) {
	disp := &whiteboxMockDispatcher{fn: func(_ context.Context, _ *dispatch.Invocation) (*dispatch.ShapedResponse, error) {
		return nil, errors.New("network down")
	}}

	fn := buildCallFn(context.Background(), disp, true, true, true, &destructiveState{})
	if err := mustErr(t, fn, "gmail.list"); !strings.Contains(err.Error(), "network down") {
		t.Fatalf("error = %v; want the dispatcher error verbatim", err)
	}
}

func TestCallResultValueShapes(t *testing.T) {
	if got := callResultValue(nil); got != nil {
		t.Errorf("nil response = %#v; want nil", got)
	}
	if got := callResultValue(&dispatch.ShapedResponse{}); got != nil {
		t.Errorf("empty body = %#v; want nil", got)
	}
	if got := callResultValue(&dispatch.ShapedResponse{Body: []byte("not json")}); got != "not json" {
		t.Errorf("undecodable body = %#v; want the raw string", got)
	}
	got := callResultValue(&dispatch.ShapedResponse{Body: mustJSON(t, map[string]any{"n": 1})})
	m, ok := got.(map[string]any)
	if !ok || m["n"] != float64(1) {
		t.Errorf("json body = %#v; want the decoded map", got)
	}
}

func TestRequiredToolFromRiskMismatchIgnoresOtherErrors(t *testing.T) {
	if got := requiredToolFromRiskMismatch(errors.New("plain")); got != "" {
		t.Errorf("plain error = %q; want \"\"", got)
	}
	if got := requiredToolFromRiskMismatch(needsConfirmation("t")); got != "" {
		t.Errorf("other structured code = %q; want \"\"", got)
	}
	if got := requiredToolFromRiskMismatch(riskMismatch("gum.write")); got != "gum.write" {
		t.Errorf("required tool = %q; want gum.write", got)
	}
}

// mustErr calls fn and fails the test when it returns no error.
func mustErr(t *testing.T, fn func(...any) (any, error), args ...any) error {
	t.Helper()
	_, err := fn(args...)
	if err == nil {
		t.Fatal("call error = nil; want a rejection")
	}
	return err
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

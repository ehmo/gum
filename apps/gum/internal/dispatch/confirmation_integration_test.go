// Package dispatch_test — confirmation integration tests (G4.8).
//
// These tests exercise the destructive gate without making live API calls.
// The live trash test is tagged //go:build live.
package dispatch_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"go.uber.org/goleak"

	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/dispatch"
)

// confirmCountingAdapter is a fake dispatch.Adapter that counts calls and returns an
// empty JSON body. Used to verify that the executor WAS reached.
type confirmCountingAdapter struct {
	calls int
}

func (a *confirmCountingAdapter) Execute(_ context.Context, _ *dispatch.Invocation, _ *dispatch.ResolvedVariant, _ *dispatch.Credentials) (*dispatch.Response, error) {
	a.calls++
	return &dispatch.Response{
		Body:       []byte(`{"ok":true}`),
		Format:     "json",
		StatusCode: 200,
	}, nil
}

// destructiveCatalog returns a minimal catalog with gmail.users.messages.trash
// wired to the counting adapter as a destructive op.
func destructiveCatalog() *catalog.Catalog {
	return &catalog.Catalog{
		CatalogSchemaVersion: 1,
		GeneratedAt:          time.Now().UTC().Format(time.RFC3339),
		GeneratorVersion:     "test-confirmation",
		Ops: []catalog.Op{
			{
				OpID:             "gmail.users.messages.trash",
				OpSchemaVersion:  1,
				Title:            "Trash Gmail message",
				Summary:          "Move a Gmail message to Trash.",
				Service:          "gmail",
				ServiceFamily:    "workspace",
				DefaultVariantID: "gmail.v1.rest.users.messages.trash",
				Variants: []catalog.Variant{
					{
						VariantID:     "gmail.v1.rest.users.messages.trash",
						Stability:     catalog.StabilityStable,
						InterfaceKind: catalog.InterfaceKindDiscoveryREST,
						BackendKind:   catalog.BackendKindTypedRestSDK,
						Preferred:     true,
						RiskClass:     catalog.RiskClassDestructive,
						AuthStrategy:  catalog.AuthStrategyBYOOAuth,
						Binding: &catalog.Binding{
							BindingSchemaVersion: 1,
							AdapterKey:           "test.counting",
							OperationKey:         "gmail.users.messages.trash",
							HTTP: &catalog.HTTPBinding{
								Method: "POST",
								Path:   "/gmail/v1/users/{userId}/messages/{id}/trash",
							},
							GoPkg:  "google.golang.org/api/gmail/v1",
							GoCall: "Users.Messages.Trash",
						},
					},
				},
			},
		},
	}
}

// TestDestructiveRefusesWithoutToken (G4.8 unit):
// Calls gum.destructive for gmail.users.messages.trash WITHOUT a confirmation token
// and asserts the response carries a CONFIRMATION_REQUIRED error.
func TestDestructiveRefusesWithoutToken(t *testing.T) {
	defer goleak.VerifyNone(t)

	adapter := &confirmCountingAdapter{}
	cat := destructiveCatalog()
	disp := dispatch.NewDispatcher(cat, map[string]dispatch.Adapter{
		"test.counting": adapter,
	})

	// Destructive call WITHOUT confirmed=true or confirmation_token.
	inv := &dispatch.Invocation{
		OpID:      "gmail.users.messages.trash",
		Args:      map[string]any{"userId": "me", "id": "msg001"},
		Format:    "json",
		RequestID: "test-destructive-refuse",
		// Confirmed: false (default)
		// ConfirmationToken: "" (default)
	}

	var err error
	msg, panicked := catchPanic(func() {
		_, err = disp.Dispatch(context.Background(), inv)
	})
	if panicked {
		t.Fatalf("Dispatch panicked: %s — green team must implement destructive gate in evaluatePolicy", msg)
	}

	if err == nil {
		t.Fatal("Dispatch destructive without token: expected error, got nil")
	}
	if !strings.Contains(err.Error(), "CONFIRMATION_REQUIRED") {
		t.Errorf("expected error to contain CONFIRMATION_REQUIRED; got: %v", err)
	}

	// Executor must NOT have been called.
	if adapter.calls > 0 {
		t.Errorf("counting adapter was called %d times; want 0 (executor must not run without confirmation)", adapter.calls)
	}
}

// firstContactToken runs a Confirmed=false destructive dispatch and returns the
// confirmation_token the dispatcher issued in the REQUIRES_CONFIRMATION
// envelope. This is the real flow: the dispatcher (not the caller) mints the
// token, bound to the op + variant + args, and the caller echoes it back.
func firstContactToken(t *testing.T, disp dispatch.Dispatcher, opID string, args map[string]any) string {
	t.Helper()
	_, err := disp.Dispatch(context.Background(), &dispatch.Invocation{
		OpID:             opID,
		Args:             args,
		Format:           "json",
		RequestID:        "first-contact",
		AllowDestructive: true, // gum.destructive tool, not yet confirmed
	})
	if err == nil {
		t.Fatal("first contact: expected REQUIRES_CONFIRMATION error, got nil")
	}
	se, ok := err.(*dispatch.StructuredError)
	if !ok {
		t.Fatalf("first contact: error is %T, want *dispatch.StructuredError", err)
	}
	tok, _ := se.Detail["confirmation_token"].(string)
	if tok == "" {
		t.Fatalf("first contact: no confirmation_token in error detail: %v", se.Detail)
	}
	return tok
}

func TestDestructiveSucceedsWithValidStubToken(t *testing.T) {
	defer goleak.VerifyNone(t)

	adapter := &confirmCountingAdapter{}
	cat := destructiveCatalog()
	disp := dispatch.NewDispatcher(cat, map[string]dispatch.Adapter{
		"test.counting": adapter,
	})

	args := map[string]any{"userId": "me", "id": "msg001"}
	tok := firstContactToken(t, disp, "gmail.users.messages.trash", args)

	inv := &dispatch.Invocation{
		OpID:              "gmail.users.messages.trash",
		Args:              args,
		Format:            "json",
		RequestID:         "test-destructive-with-token",
		Confirmed:         true,
		ConfirmationToken: tok,
	}

	var resp *dispatch.ShapedResponse
	var err error
	msg, panicked := catchPanic(func() {
		resp, err = disp.Dispatch(context.Background(), inv)
	})
	if panicked {
		t.Fatalf("Dispatch with token panicked: %s", msg)
	}
	if err != nil {
		t.Fatalf("Dispatch with token: %v", err)
	}
	if resp == nil {
		t.Fatal("Dispatch with token: response is nil")
	}
	if adapter.calls != 1 {
		t.Errorf("counting adapter calls = %d; want 1", adapter.calls)
	}
}

// TestDestructiveTokenBoundToArgs pins the audit fix: a confirmation token
// issued for one target (id=msg001) must NOT verify when the destructive op is
// re-dispatched against a different target (id=msg999). Before the fix the token
// was bound only to (op_id, variant_id, purpose) and was replayable across args.
func TestDestructiveTokenBoundToArgs(t *testing.T) {
	defer goleak.VerifyNone(t)

	adapter := &confirmCountingAdapter{}
	cat := destructiveCatalog()
	disp := dispatch.NewDispatcher(cat, map[string]dispatch.Adapter{
		"test.counting": adapter,
	})

	tok := firstContactToken(t, disp, "gmail.users.messages.trash", map[string]any{"userId": "me", "id": "msg001"})

	// Replay the token against a DIFFERENT message id.
	_, err := disp.Dispatch(context.Background(), &dispatch.Invocation{
		OpID:              "gmail.users.messages.trash",
		Args:              map[string]any{"userId": "me", "id": "msg999"},
		Format:            "json",
		RequestID:         "test-destructive-replay",
		Confirmed:         true,
		ConfirmationToken: tok,
	})
	if err == nil {
		t.Fatal("reusing a token across different args succeeded; want CONFIRMATION_TOKEN_INVALID")
	}
	if !strings.Contains(err.Error(), "CONFIRMATION_TOKEN_INVALID") {
		t.Errorf("error = %v; want CONFIRMATION_TOKEN_INVALID (token must be bound to args)", err)
	}
	if adapter.calls != 0 {
		t.Errorf("adapter called %d times; want 0 (cross-target replay must be rejected before execution)", adapter.calls)
	}
}

// TestPinnedHigherRiskVariantIsGated pins the audit fix for the latent
// variant-mismatch bypass: an op whose DEFAULT variant is read but which also
// has a DESTRUCTIVE variant must gate on the destructive variant when the caller
// pins it via variant_id — not on the (lower-risk) default. Before the fix the
// risk gate ran on the default (read) variant, so the pinned destructive variant
// executed with no allow_destructive / confirmation.
func TestPinnedHigherRiskVariantIsGated(t *testing.T) {
	defer goleak.VerifyNone(t)

	adapter := &confirmCountingAdapter{}
	cat := &catalog.Catalog{
		CatalogSchemaVersion: 1,
		GeneratedAt:          time.Now().UTC().Format(time.RFC3339),
		GeneratorVersion:     "test-mixed-risk",
		Ops: []catalog.Op{{
			OpID:             "test.mixed",
			OpSchemaVersion:  1,
			Title:            "Mixed-risk op",
			DefaultVariantID: "test.mixed.read",
			Variants: []catalog.Variant{
				{
					VariantID: "test.mixed.read", Stability: catalog.StabilityStable,
					InterfaceKind: catalog.InterfaceKindDiscoveryREST, BackendKind: catalog.BackendKindTypedRestSDK,
					RiskClass: catalog.RiskClassRead, AuthStrategy: catalog.AuthStrategyBYOOAuth,
					Binding: &catalog.Binding{BindingSchemaVersion: 1, AdapterKey: "test.counting", OperationKey: "test.mixed",
						HTTP: &catalog.HTTPBinding{Method: "GET", Path: "/test/mixed"}},
				},
				{
					VariantID: "test.mixed.destructive", Stability: catalog.StabilityStable,
					InterfaceKind: catalog.InterfaceKindDiscoveryREST, BackendKind: catalog.BackendKindTypedRestSDK,
					RiskClass: catalog.RiskClassDestructive, AuthStrategy: catalog.AuthStrategyBYOOAuth,
					Binding: &catalog.Binding{BindingSchemaVersion: 1, AdapterKey: "test.counting", OperationKey: "test.mixed",
						HTTP: &catalog.HTTPBinding{Method: "DELETE", Path: "/test/mixed"}},
				},
			},
		}},
	}
	disp := dispatch.NewDispatcher(cat, map[string]dispatch.Adapter{"test.counting": adapter})

	// Pin the destructive variant but supply NO allow_destructive / confirmation,
	// as if it were a read call. The destructive gate must fire.
	_, err := disp.Dispatch(context.Background(), &dispatch.Invocation{
		OpID:               "test.mixed",
		RequestedVariantID: "test.mixed.destructive",
		Args:               map[string]any{"x": "1"},
		Format:             "json",
		RequestID:          "test-pinned-risk",
	})
	if err == nil {
		t.Fatal("pinned destructive variant executed without a risk gate; want RISK_TOOL_MISMATCH")
	}
	if !strings.Contains(err.Error(), "RISK_TOOL_MISMATCH") {
		t.Errorf("error = %v; want RISK_TOOL_MISMATCH gated on the pinned destructive variant", err)
	}
	if adapter.calls != 0 {
		t.Errorf("adapter called %d times; want 0 (the destructive gate must reject before execution)", adapter.calls)
	}
}

// TestUnknownRiskClassFailsClosed pins the audit fix: a variant with an
// unrecognized risk_class is REFUSED at the policy gate (fail closed) rather
// than executing with no risk gate at all.
func TestUnknownRiskClassFailsClosed(t *testing.T) {
	defer goleak.VerifyNone(t)

	adapter := &confirmCountingAdapter{}
	cat := &catalog.Catalog{
		CatalogSchemaVersion: 1,
		GeneratedAt:          time.Now().UTC().Format(time.RFC3339),
		GeneratorVersion:     "test-bad-risk",
		Ops: []catalog.Op{{
			OpID:             "test.badrisk",
			OpSchemaVersion:  1,
			Title:            "Bad risk op",
			DefaultVariantID: "test.badrisk.v1",
			Variants: []catalog.Variant{{
				VariantID: "test.badrisk.v1", Stability: catalog.StabilityStable,
				InterfaceKind: catalog.InterfaceKindDiscoveryREST, BackendKind: catalog.BackendKindTypedRestSDK,
				RiskClass: catalog.RiskClass("bogus"), AuthStrategy: catalog.AuthStrategyBYOOAuth,
				Binding: &catalog.Binding{BindingSchemaVersion: 1, AdapterKey: "test.counting", OperationKey: "test.badrisk",
					HTTP: &catalog.HTTPBinding{Method: "GET", Path: "/x"}},
			}},
		}},
	}
	disp := dispatch.NewDispatcher(cat, map[string]dispatch.Adapter{"test.counting": adapter})

	_, err := disp.Dispatch(context.Background(), &dispatch.Invocation{OpID: "test.badrisk", Args: map[string]any{}, Format: "json"})
	if err == nil {
		t.Fatal("unknown risk_class executed with no gate; want RISK_TOOL_MISMATCH")
	}
	if !strings.Contains(err.Error(), "RISK_TOOL_MISMATCH") {
		t.Errorf("err = %v; want RISK_TOOL_MISMATCH (must fail closed on unknown risk_class)", err)
	}
	if adapter.calls != 0 {
		t.Errorf("adapter called %d times; want 0 (must not execute an unknown-risk op)", adapter.calls)
	}
}

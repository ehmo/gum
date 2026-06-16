package dispatch

import (
	"context"
	"errors"
	"testing"

	"github.com/ehmo/gum/internal/catalog"
)

// TestResolveAuthNilResolver verifies the resolveAuth step is a no-op when no
// auth.Resolver is wired into the dispatcher (Phase 2 stub behavior preserved).
// Acceptance: nil resolver returns nil creds without error.
func TestResolveAuthNilResolver(t *testing.T) {
	d := &dispatcher{}
	creds, err := d.resolveAuth(t.Context(), &Invocation{OpID: "any"}, &ResolvedVariant{})
	if err != nil {
		t.Fatalf("nil resolver: unexpected err: %v", err)
	}
	if creds != nil {
		t.Fatalf("nil resolver: want nil creds, got %+v", creds)
	}
}

// TestResolveAuthPopulatesCredentials verifies the resolved Credentials
// (Token + QuotaProjectID) flow through to step 7 via the resolveAuth return.
// Acceptance: Credentials populated from resolver response.
func TestResolveAuthPopulatesCredentials(t *testing.T) {
	r := &mockAuthResolver{
		creds: &Credentials{Token: "tok-123", QuotaProjectID: "proj-456"},
	}
	d := &dispatcher{auth: r}
	creds, err := d.resolveAuth(t.Context(), &Invocation{OpID: "x"}, &ResolvedVariant{})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if creds == nil {
		t.Fatal("want non-nil creds")
	}
	if creds.Token != "tok-123" {
		t.Errorf("Token=%q want tok-123", creds.Token)
	}
	if creds.QuotaProjectID != "proj-456" {
		t.Errorf("QuotaProjectID=%q want proj-456", creds.QuotaProjectID)
	}
}

// TestResolveAuthWrapsErrorAsAuthRequired verifies that a plain error from the
// auth.Resolver surfaces as a structured AUTH_REQUIRED error (spec §1421 stable
// runtime error codes; spec §233 step 5).
// Acceptance: resolver errors surface as AUTH_REQUIRED.
func TestResolveAuthWrapsErrorAsAuthRequired(t *testing.T) {
	r := &mockAuthResolver{err: errors.New("token endpoint 500")}
	d := &dispatcher{auth: r}
	_, err := d.resolveAuth(t.Context(), &Invocation{OpID: "x"}, &ResolvedVariant{})
	if err == nil {
		t.Fatal("want error, got nil")
	}
	if !IsStructuredError(err, ErrCodeAuthRequired) {
		t.Fatalf("err code mismatch: got %v, want AUTH_REQUIRED", err)
	}
}

// TestResolveAuthPreservesStructuredError verifies that when the resolver
// already returns a structured error (e.g. SCOPE_MISSING from a scope-gate
// failure), it is not re-wrapped — the structured code reaches the caller
// unchanged so dispatch can surface the correct spec §1421 code.
func TestResolveAuthPreservesStructuredError(t *testing.T) {
	scope := NewStructuredError(ErrCodeScopeMissing, "needs https://example/scope")
	r := &mockAuthResolver{err: scope}
	d := &dispatcher{auth: r}
	_, err := d.resolveAuth(t.Context(), &Invocation{OpID: "x"}, &ResolvedVariant{})
	if !IsStructuredError(err, ErrCodeScopeMissing) {
		t.Fatalf("structured err re-wrapped: got %v, want SCOPE_MISSING preserved", err)
	}
}

// TestResolveAuthContextCancellationPropagates verifies ctx.Err() flows through
// to the caller (so steps 6/7 can short-circuit on cancellation).
// Acceptance: context cancellation propagates.
func TestResolveAuthContextCancellationPropagates(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	r := &mockAuthResolver{err: ctx.Err()}
	d := &dispatcher{auth: r}
	_, err := d.resolveAuth(ctx, &Invocation{OpID: "x"}, &ResolvedVariant{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("ctx.Err() lost: got %v, want context.Canceled", err)
	}
}

// TestResolveAuthPassesInvocationAndVariant verifies the resolver receives the
// exact Invocation + ResolvedVariant the kernel was driving — so resolvers can
// pick the right strategy from variant.AuthMode.
func TestResolveAuthPassesInvocationAndVariant(t *testing.T) {
	r := &mockAuthResolver{creds: &Credentials{Token: "ok"}}
	d := &dispatcher{auth: r}
	inv := &Invocation{OpID: "gmail.messages.list"}
	rv := &ResolvedVariant{Variant: &catalog.Variant{VariantID: "v1"}}
	_, err := d.resolveAuth(t.Context(), inv, rv)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if r.gotInv != inv {
		t.Errorf("resolver got inv=%p, want %p", r.gotInv, inv)
	}
	if r.gotRV != rv {
		t.Errorf("resolver got rv=%p, want %p", r.gotRV, rv)
	}
}

type mockAuthResolver struct {
	creds  *Credentials
	err    error
	gotInv *Invocation
	gotRV  *ResolvedVariant
}

func (m *mockAuthResolver) ResolveAuth(_ context.Context, inv *Invocation, rv *ResolvedVariant) (*Credentials, error) {
	m.gotInv = inv
	m.gotRV = rv
	return m.creds, m.err
}

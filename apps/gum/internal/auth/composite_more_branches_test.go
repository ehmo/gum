package auth

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/dispatch"
)

// TestResolveAuthNilResolvedVariantReturnsNilNil pins the very first guard:
// when the caller passes a nil ResolvedVariant the composite must return
// (nil, nil) so the dispatcher's "no auth" path is exercised without
// blowing up on a nil Variant pointer.
func TestResolveAuthNilResolvedVariantReturnsNilNil(t *testing.T) {
	c := &CompositeResolver{}
	creds, err := c.ResolveAuth(context.Background(), &dispatch.Invocation{}, nil)
	if creds != nil || err != nil {
		t.Errorf("got creds=%v err=%v; want (nil, nil)", creds, err)
	}
}

// TestResolveAuthNilVariantReturnsNilNil pins the second arm of the same
// guard: an empty ResolvedVariant with nil Variant must also return
// (nil, nil), NOT panic on the AuthStrategy access.
func TestResolveAuthNilVariantReturnsNilNil(t *testing.T) {
	c := &CompositeResolver{}
	creds, err := c.ResolveAuth(context.Background(), &dispatch.Invocation{},
		&dispatch.ResolvedVariant{Variant: nil})
	if creds != nil || err != nil {
		t.Errorf("got creds=%v err=%v; want (nil, nil)", creds, err)
	}
}

// TestResolveAuthUnknownStrategyErrors pins the strategyFromCatalog error
// arm: a synthetic AuthStrategy that doesn't map to a known constant
// must surface an error rather than fall through to the default branch
// (which would have the wrong shape — AUTH_STRATEGY_NOT_IMPLEMENTED is
// the default branch, but an unrecognised raw value should fail earlier).
func TestResolveAuthUnknownStrategyErrors(t *testing.T) {
	c := &CompositeResolver{}
	rv := &dispatch.ResolvedVariant{
		Variant: &catalog.Variant{AuthStrategy: catalog.AuthStrategy("totally-bogus")},
	}
	_, err := c.ResolveAuth(context.Background(), &dispatch.Invocation{}, rv)
	if err == nil {
		t.Fatal("want error for unknown strategy; got nil")
	}
}

// TestResolveAuthAPIKeyMissingResolver pins the api_key path's
// AUTH_RESOLVER_NOT_CONFIGURED arm: when APIKey is nil the composite
// must surface the typed error with strategy="api_key", not panic.
func TestResolveAuthAPIKeyMissingResolver(t *testing.T) {
	c := &CompositeResolver{APIKey: nil}
	rv := &dispatch.ResolvedVariant{
		Variant: &catalog.Variant{AuthStrategy: catalog.AuthStrategyAPIKey},
	}
	_, err := c.ResolveAuth(context.Background(), &dispatch.Invocation{}, rv)
	var ae *AuthError
	if !errors.As(err, &ae) {
		t.Fatalf("err=%T %v; want *AuthError", err, err)
	}
	if ae.Code != "AUTH_RESOLVER_NOT_CONFIGURED" || ae.Strategy != "api_key" {
		t.Errorf("got Code=%q Strategy=%q; want AUTH_RESOLVER_NOT_CONFIGURED/api_key", ae.Code, ae.Strategy)
	}
}

// TestResolveAuthAPIKeyResolverErrorPropagates pins the api_key error
// propagation arm: a Resolve failure must bubble up verbatim, not be
// wrapped into an AUTH_RESOLVER_NOT_CONFIGURED.
func TestResolveAuthAPIKeyResolverErrorPropagates(t *testing.T) {
	want := errors.New("api key fetch failed")
	c := &CompositeResolver{APIKey: &stubResolver{err: want}}
	rv := &dispatch.ResolvedVariant{
		Variant: &catalog.Variant{AuthStrategy: catalog.AuthStrategyAPIKey},
	}
	_, err := c.ResolveAuth(context.Background(), &dispatch.Invocation{}, rv)
	if !errors.Is(err, want) {
		t.Errorf("err=%v; want propagated %v", err, want)
	}
}

// TestResolveAuthCompoundIncludesOpIDInSetupCommand pins the compound
// branch when an OpID is present on the Invocation: the SetupCommand
// MUST be "gum auth setup <op_id>" so operators can copy-paste the
// remediation verbatim.
func TestResolveAuthCompoundIncludesOpIDInSetupCommand(t *testing.T) {
	c := &CompositeResolver{}
	rv := &dispatch.ResolvedVariant{
		Variant: &catalog.Variant{AuthStrategy: catalog.AuthStrategyCompound},
	}
	_, err := c.ResolveAuth(context.Background(),
		&dispatch.Invocation{OpID: "gmail.messages.send"}, rv)
	var ae *AuthError
	if !errors.As(err, &ae) {
		t.Fatalf("err=%T %v; want *AuthError", err, err)
	}
	if ae.Code != "AUTH_REQUIRED" {
		t.Errorf("Code=%q; want AUTH_REQUIRED", ae.Code)
	}
	if !strings.Contains(ae.SetupCommand, "gum auth setup gmail.messages.send") {
		t.Errorf("SetupCommand=%q; want trailing op_id", ae.SetupCommand)
	}
	if ae.OpID != "gmail.messages.send" {
		t.Errorf("OpID=%q; want echoed back", ae.OpID)
	}
	if len(ae.MissingComponents) == 0 {
		t.Errorf("MissingComponents empty; want at least see_setup_command sentinel")
	}
}

package auth

import (
	"context"
	"errors"
	"testing"

	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/dispatch"
)

// TestResolveAuthADCResolveErrorPropagates pins the ADC `err != nil →
// return err` arm (composite.go:74-76). When ADC is wired but errors,
// the composite must surface the error verbatim (no fallback).
func TestResolveAuthADCResolveErrorPropagates(t *testing.T) {
	t.Parallel()
	boom := errors.New("adc-boom")
	c := &CompositeResolver{ADC: &stubResolver{err: boom}}
	rv := &dispatch.ResolvedVariant{
		Variant: &catalog.Variant{AuthStrategy: catalog.AuthStrategyADC},
	}
	_, err := c.ResolveAuth(context.Background(), &dispatch.Invocation{}, rv)
	if !errors.Is(err, boom) {
		t.Errorf("err=%v; want adc-boom", err)
	}
}

// TestResolveAuthBYOErrorNoADCPropagates pins the BYO `err && c.ADC == nil
// → return err` arm (composite.go:85-87). With BYO erroring AND ADC
// unwired, the composite returns the BYO error rather than trying ADC.
func TestResolveAuthBYOErrorNoADCPropagates(t *testing.T) {
	t.Parallel()
	boom := errors.New("byo-boom")
	c := &CompositeResolver{BYO: &stubResolver{err: boom}}
	rv := &dispatch.ResolvedVariant{
		Variant: &catalog.Variant{AuthStrategy: catalog.AuthStrategyBYOOAuth},
	}
	_, err := c.ResolveAuth(context.Background(), &dispatch.Invocation{}, rv)
	if !errors.Is(err, boom) {
		t.Errorf("err=%v; want byo-boom", err)
	}
}

// TestResolveAuthBYOErrorIgnoresWiredADC pins that even when ADC is wired, a
// byo_oauth failure is NOT rescued by it. The v0.1.0 fallthrough was removed:
// the BYO error (byo-first) must surface, and the wired ADC stub's error
// (which the old code would have surfaced instead) must never be reached.
func TestResolveAuthBYOErrorIgnoresWiredADC(t *testing.T) {
	t.Parallel()
	byoFirst := errors.New("byo-first")
	c := &CompositeResolver{
		BYO: &stubResolver{err: byoFirst},
		ADC: &stubResolver{err: errors.New("adc-boom-after-byo")},
	}
	rv := &dispatch.ResolvedVariant{
		Variant: &catalog.Variant{AuthStrategy: catalog.AuthStrategyBYOOAuth},
	}
	_, err := c.ResolveAuth(context.Background(), &dispatch.Invocation{}, rv)
	if !errors.Is(err, byoFirst) {
		t.Errorf("err=%v; want byo-first (no ADC fallthrough)", err)
	}
}

// TestResolveAuthSAHappyPathReturnsCreds pins the SA happy-path body
// (composite.go:111-115). A wired SA resolver that returns creds must
// surface those creds via ToDispatchCredentials.
func TestResolveAuthSAHappyPathReturnsCreds(t *testing.T) {
	t.Parallel()
	c := &CompositeResolver{
		SA: &stubResolver{creds: &Credentials{Token: "sa-token", StrategyName: "service_account_key"}},
	}
	rv := &dispatch.ResolvedVariant{
		Variant: &catalog.Variant{AuthStrategy: catalog.AuthStrategyServiceAccountKey},
	}
	got, err := c.ResolveAuth(context.Background(), &dispatch.Invocation{}, rv)
	if err != nil {
		t.Fatalf("ResolveAuth: %v", err)
	}
	if got == nil || got.Token != "sa-token" {
		t.Errorf("got=%+v; want Token=sa-token", got)
	}
}

// TestResolveAuthSAResolveErrorPropagates pins the SA `err → return err`
// arm (composite.go:112-114).
func TestResolveAuthSAResolveErrorPropagates(t *testing.T) {
	t.Parallel()
	boom := errors.New("sa-boom")
	c := &CompositeResolver{SA: &stubResolver{err: boom}}
	rv := &dispatch.ResolvedVariant{
		Variant: &catalog.Variant{AuthStrategy: catalog.AuthStrategyServiceAccountKey},
	}
	_, err := c.ResolveAuth(context.Background(), &dispatch.Invocation{}, rv)
	if !errors.Is(err, boom) {
		t.Errorf("err=%v; want sa-boom", err)
	}
}

// TestResolveAuthAPIKeyHappyPathReturnsCreds pins the APIKey happy-path
// body (composite.go:125-129). A wired APIKey resolver returning creds
// must surface them.
func TestResolveAuthAPIKeyHappyPathReturnsCreds(t *testing.T) {
	t.Parallel()
	c := &CompositeResolver{
		APIKey: &stubResolver{creds: &Credentials{APIKey: "ak123", StrategyName: "api_key"}},
	}
	rv := &dispatch.ResolvedVariant{
		Variant: &catalog.Variant{AuthStrategy: catalog.AuthStrategyAPIKey},
	}
	got, err := c.ResolveAuth(context.Background(), &dispatch.Invocation{}, rv)
	if err != nil {
		t.Fatalf("ResolveAuth: %v", err)
	}
	if got == nil || got.APIKey != "ak123" {
		t.Errorf("got=%+v; want APIKey=ak123", got)
	}
}

// TestResolveAuthGumOAuthHappyPathReturnsCreds pins the GumOAuth
// happy-path body (composite.go:140-144). Injecting a stub via
// c.GumOAuth bypasses the lazy NewGumOAuth() factory at line 138.
func TestResolveAuthGumOAuthHappyPathReturnsCreds(t *testing.T) {
	t.Parallel()
	c := &CompositeResolver{
		GumOAuth: &stubResolver{creds: &Credentials{Token: "gum-tok", StrategyName: "gum_oauth"}},
	}
	rv := &dispatch.ResolvedVariant{
		Variant: &catalog.Variant{AuthStrategy: catalog.AuthStrategyGUMOAuth},
	}
	got, err := c.ResolveAuth(context.Background(), &dispatch.Invocation{}, rv)
	if err != nil {
		t.Fatalf("ResolveAuth: %v", err)
	}
	if got == nil || got.Token != "gum-tok" {
		t.Errorf("got=%+v; want Token=gum-tok", got)
	}
}

// TestResolveAuthPluginManagedReturnsNoCreds pins the live-sweep fix: a
// plugin_managed variant resolves to (nil, nil) — the plugin owns its own auth,
// so the host returns no credentials and lets dispatch reach the plugin-mcp
// adapter. Before the fix this fell through to AUTH_STRATEGY_NOT_IMPLEMENTED,
// making the unofficial ops (flights/scholar/…) dead on discovery.
func TestResolveAuthPluginManagedReturnsNoCreds(t *testing.T) {
	t.Parallel()
	c := &CompositeResolver{}
	rv := &dispatch.ResolvedVariant{
		Variant: &catalog.Variant{AuthStrategy: catalog.AuthStrategyPluginManaged},
	}
	creds, err := c.ResolveAuth(context.Background(), &dispatch.Invocation{OpID: "scholar.search"}, rv)
	if err != nil {
		t.Fatalf("ResolveAuth(plugin_managed) err=%v; want nil (no host auth)", err)
	}
	if creds != nil {
		t.Errorf("creds=%v; want nil (plugin manages its own auth)", creds)
	}
}

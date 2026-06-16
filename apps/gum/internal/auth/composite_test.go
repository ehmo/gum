package auth

import (
	"context"
	"errors"
	"testing"

	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/dispatch"
)

// stubResolver lets tests force a specific Credentials result.
type stubResolver struct {
	creds *Credentials
	err   error
}

func (s *stubResolver) Resolve(_ context.Context, _ []string) (*Credentials, error) {
	return s.creds, s.err
}

// TestCompositeRoutesByStrategy verifies that the composite picks the BYO
// resolver for byo_oauth and the ADC resolver for adc.
func TestCompositeRoutesByStrategy(t *testing.T) {
	byoCalled, adcCalled := 0, 0
	c := &CompositeResolver{
		BYO: &stubResolver{creds: &Credentials{Token: "byo", StrategyName: "byo_oauth"}},
		ADC: &stubResolver{creds: &Credentials{Token: "adc", StrategyName: "adc"}},
	}
	// Wrap with counting resolvers.
	c.BYO = &stubResolver{creds: &Credentials{Token: "byo", StrategyName: "byo_oauth"}}
	c.ADC = &stubResolver{creds: &Credentials{Token: "adc", StrategyName: "adc"}}
	_ = byoCalled
	_ = adcCalled

	cases := []struct {
		name     string
		strategy catalog.AuthStrategy
		want     string
	}{
		{"adc-routes-to-adc", catalog.AuthStrategyADC, "adc"},
		{"byo-routes-to-byo", catalog.AuthStrategyBYOOAuth, "byo"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rv := &dispatch.ResolvedVariant{
				Variant: &catalog.Variant{AuthStrategy: tc.strategy},
			}
			got, err := c.ResolveAuth(context.Background(), &dispatch.Invocation{}, rv)
			if err != nil {
				t.Fatalf("ResolveAuth: %v", err)
			}
			if got == nil {
				t.Fatal("nil credentials")
			}
			if got.Token != tc.want {
				t.Errorf("Token = %q, want %q", got.Token, tc.want)
			}
		})
	}
}

// TestCompositeRejectsDisabledStrategies verifies that gum_oauth and the six
// stubbed strategies surface a clean AuthError instead of crashing.
func TestCompositeRejectsDisabledStrategies(t *testing.T) {
	c := &CompositeResolver{
		BYO: &stubResolver{},
		ADC: &stubResolver{},
	}
	for _, strat := range []catalog.AuthStrategy{
		catalog.AuthStrategyGUMOAuth,
		catalog.AuthStrategyAPIKey,
		catalog.AuthStrategyServiceAccountKey,
		catalog.AuthStrategyWorkloadIdentity,
		catalog.AuthStrategyImpersonation,
	} {
		t.Run(string(strat), func(t *testing.T) {
			rv := &dispatch.ResolvedVariant{
				Variant: &catalog.Variant{AuthStrategy: strat},
			}
			_, err := c.ResolveAuth(context.Background(), &dispatch.Invocation{}, rv)
			if err == nil {
				t.Errorf("expected error for strategy %q, got none", strat)
				return
			}
			var ae *AuthError
			if !errors.As(err, &ae) {
				t.Errorf("expected *AuthError for strategy %q, got %T", strat, err)
			}
		})
	}
}

// TestCompositeNoneStrategyNeedsNoCreds pins the auth_strategy=none contract:
// a variant that declares "none" (the gum.code meta-op, which re-dispatches its
// sub-calls with their own per-op auth inside the sandbox) needs no upstream
// credential resolution. ResolveAuth MUST return (nil, nil) — not an
// AUTH_STRATEGY_NOT_IMPLEMENTED error — so the dispatcher proceeds to the
// adapter. This is the auth half of the gum.code P0 (gum-7ras).
func TestCompositeNoneStrategyNeedsNoCreds(t *testing.T) {
	c := &CompositeResolver{} // no resolvers wired; none needs none
	rv := &dispatch.ResolvedVariant{
		Variant: &catalog.Variant{AuthStrategy: catalog.AuthStrategyNone},
	}
	creds, err := c.ResolveAuth(context.Background(), &dispatch.Invocation{}, rv)
	if err != nil {
		t.Fatalf("ResolveAuth(none) returned error: %v", err)
	}
	if creds != nil {
		t.Errorf("creds = %+v, want nil (none resolves no upstream credential)", creds)
	}
}

// TestCompositeBYOErrorPropagatesNoADCFallback pins that a byo_oauth failure
// surfaces verbatim and is NOT silently rescued by ADC/gcloud. The earlier
// v0.1.0 fallthrough was removed: byo_oauth is now self-contained (the user's
// own OAuth client + loopback flow), so a BYO error must reach the caller so
// the JIT layer can prompt for `gum login` instead of leaking into ADC.
func TestCompositeBYOErrorPropagatesNoADCFallback(t *testing.T) {
	byoBoom := errors.New("no refresh token configured")
	adcCalled := false
	c := &CompositeResolver{
		BYO: &stubResolver{err: byoBoom},
		ADC: &countingResolver{onResolve: func() { adcCalled = true }},
	}
	rv := &dispatch.ResolvedVariant{
		Variant: &catalog.Variant{AuthStrategy: catalog.AuthStrategyBYOOAuth},
	}
	_, err := c.ResolveAuth(context.Background(), &dispatch.Invocation{}, rv)
	if !errors.Is(err, byoBoom) {
		t.Errorf("err=%v; want byo error propagated verbatim", err)
	}
	if adcCalled {
		t.Error("ADC resolver was consulted; byo_oauth must not fall through to ADC/gcloud")
	}
}

// countingResolver records whether Resolve was invoked.
type countingResolver struct {
	onResolve func()
}

func (r *countingResolver) Resolve(context.Context, []string) (*Credentials, error) {
	if r.onResolve != nil {
		r.onResolve()
	}
	return &Credentials{Token: "should-not-be-used"}, nil
}

// TestCompositeReportsMissingResolver returns AUTH_RESOLVER_NOT_CONFIGURED when
// the matching strategy has no resolver wired.
func TestCompositeReportsMissingResolver(t *testing.T) {
	c := &CompositeResolver{} // both nil
	rv := &dispatch.ResolvedVariant{
		Variant: &catalog.Variant{AuthStrategy: catalog.AuthStrategyADC},
	}
	_, err := c.ResolveAuth(context.Background(), &dispatch.Invocation{}, rv)
	if err == nil {
		t.Fatal("expected error, got none")
	}
	var ae *AuthError
	if !errors.As(err, &ae) {
		t.Fatalf("expected *AuthError, got %T", err)
	}
	if ae.Code != "AUTH_RESOLVER_NOT_CONFIGURED" {
		t.Errorf("Code = %q, want AUTH_RESOLVER_NOT_CONFIGURED", ae.Code)
	}
}

// TestNormaliseScopesPrefixesShortForms verifies the catalog→URL transform.
func TestNormaliseScopesPrefixesShortForms(t *testing.T) {
	cases := []struct {
		in   []string
		want []string
	}{
		{
			in:   []string{"gmail.readonly"},
			want: []string{"https://www.googleapis.com/auth/gmail.readonly"},
		},
		{
			in:   []string{"https://www.googleapis.com/auth/calendar.readonly"},
			want: []string{"https://www.googleapis.com/auth/calendar.readonly"},
		},
		{
			in:   []string{"", "drive.metadata.readonly"},
			want: []string{"https://www.googleapis.com/auth/drive.metadata.readonly"},
		},
	}
	for _, tc := range cases {
		got := normaliseScopes(tc.in)
		if len(got) != len(tc.want) {
			t.Errorf("normaliseScopes(%v) = %v, want %v", tc.in, got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("normaliseScopes(%v)[%d] = %q, want %q", tc.in, i, got[i], tc.want[i])
			}
		}
	}
}

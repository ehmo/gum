package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	keyringlib "github.com/zalando/go-keyring"

	"github.com/ehmo/gum/internal/auth"
)

// TestNewAuthProbeCmdShape pins the cobra surface: the probe subcommand
// must exist with a --scopes flag defaulting to gmail.readonly so the
// `gum auth probe` smoke test stays one-keystroke for operators.
func TestNewAuthProbeCmdShape(t *testing.T) {
	cmd := newAuthProbeCmd()
	if cmd.Use != "probe" {
		t.Errorf("Use=%q; want probe", cmd.Use)
	}
	f := cmd.Flags().Lookup("scopes")
	if f == nil {
		t.Fatal("--scopes flag missing")
	}
	if !strings.Contains(f.DefValue, "gmail.readonly") {
		t.Errorf("--scopes default=%q; want it to contain gmail.readonly", f.DefValue)
	}
	sf := cmd.Flags().Lookup("strategy")
	if sf == nil {
		t.Fatal("--strategy flag missing")
	}
	if sf.DefValue != "auto" {
		t.Errorf("--strategy default=%q; want auto", sf.DefValue)
	}
}

func TestAuthProbeAutoPrefersStoredBYOOAuthClient(t *testing.T) {
	keyringlib.MockInit()
	t.Cleanup(keyringlib.MockInit)

	if err := auth.StoreByoClient(auth.NewOSKeyring(), auth.DefaultAPIKeyProfile, auth.ByoClient{
		ClientID:     "cid.apps.googleusercontent.com",
		ClientSecret: "csec",
	}); err != nil {
		t.Fatalf("StoreByoClient: %v", err)
	}

	var gotCfg auth.ByoOAuthConfig
	origBYO := newAuthProbeByoResolver
	origADC := newAuthProbeADCResolver
	t.Cleanup(func() {
		newAuthProbeByoResolver = origBYO
		newAuthProbeADCResolver = origADC
	})
	newAuthProbeByoResolver = func(cfg auth.ByoOAuthConfig) auth.Resolver {
		gotCfg = cfg
		return probeResolverFunc(func(context.Context, []string) (*auth.Credentials, error) {
			return &auth.Credentials{
				Token:              "at-byo",
				ExpiresAt:          time.Date(2026, 6, 18, 19, 22, 55, 0, time.UTC),
				Scopes:             cfg.Scopes,
				StrategyName:       "byo_oauth",
				SubjectFingerprint: "fp",
			}, nil
		})
	}
	newAuthProbeADCResolver = func() auth.Resolver {
		t.Fatal("auto probe used ADC despite a stored BYO OAuth client")
		return nil
	}

	cmd := newAuthProbeCmd()
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetArgs([]string{"--scopes", "adwords"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var out map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("decode output: %v; raw=%q", err, stdout.String())
	}
	if out["strategy"] != "byo_oauth" {
		t.Errorf("strategy=%v; want byo_oauth", out["strategy"])
	}
	if gotCfg.ClientID != "cid.apps.googleusercontent.com" || gotCfg.ClientSecret != "csec" {
		t.Errorf("BYO cfg client = (%q, %q); want stored client", gotCfg.ClientID, gotCfg.ClientSecret)
	}
	if len(gotCfg.Scopes) != 1 || gotCfg.Scopes[0] != "https://www.googleapis.com/auth/adwords" {
		t.Errorf("BYO cfg scopes=%v; want normalized adwords scope", gotCfg.Scopes)
	}
}

func TestAuthProbeExplicitADCIgnoresStoredBYOOAuthClient(t *testing.T) {
	keyringlib.MockInit()
	t.Cleanup(keyringlib.MockInit)

	if err := auth.StoreByoClient(auth.NewOSKeyring(), auth.DefaultAPIKeyProfile, auth.ByoClient{ClientID: "cid"}); err != nil {
		t.Fatalf("StoreByoClient: %v", err)
	}

	origBYO := newAuthProbeByoResolver
	origADC := newAuthProbeADCResolver
	t.Cleanup(func() {
		newAuthProbeByoResolver = origBYO
		newAuthProbeADCResolver = origADC
	})
	newAuthProbeByoResolver = func(auth.ByoOAuthConfig) auth.Resolver {
		t.Fatal("--strategy adc should not construct a BYO resolver")
		return nil
	}
	newAuthProbeADCResolver = func() auth.Resolver {
		return probeResolverFunc(func(_ context.Context, scopes []string) (*auth.Credentials, error) {
			return &auth.Credentials{
				Token:        "at-adc",
				ExpiresAt:    time.Date(2026, 6, 18, 19, 22, 55, 0, time.UTC),
				Scopes:       scopes,
				StrategyName: "adc",
			}, nil
		})
	}

	cmd := newAuthProbeCmd()
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetArgs([]string{"--strategy", "adc", "--scopes", "adwords"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var out map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("decode output: %v; raw=%q", err, stdout.String())
	}
	if out["strategy"] != "adc" {
		t.Errorf("strategy=%v; want adc", out["strategy"])
	}
}

type probeResolverFunc func(context.Context, []string) (*auth.Credentials, error)

func (f probeResolverFunc) Resolve(ctx context.Context, scopes []string) (*auth.Credentials, error) {
	return f(ctx, scopes)
}

// TestNewAuthLoginCmdNoClientPointsToSetup pins the redesigned ergonomics:
// `gum auth login` no longer demands an explicit --scope. With no OAuth client
// registered yet it must point the operator at `gum auth use-oauth-client`
// rather than complaining about scopes — the user shouldn't have to guess
// scope strings.
func TestNewAuthLoginCmdNoClientPointsToSetup(t *testing.T) {
	keyringlib.MockInit()
	t.Cleanup(keyringlib.MockInit)

	cmd := newAuthLoginCmd()
	cmd.SetArgs(nil)
	cmd.SetOut(&strings.Builder{})
	cmd.SetErr(&strings.Builder{})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when no OAuth client is configured")
	}
	if !strings.Contains(err.Error(), "use-oauth-client") {
		t.Errorf("err=%q; want pointer to `gum auth use-oauth-client`", err)
	}
	if strings.Contains(err.Error(), "--scope is required") {
		t.Errorf("err=%q; should NOT demand an explicit --scope anymore", err)
	}
}

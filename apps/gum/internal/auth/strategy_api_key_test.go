package auth

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// TestAPIKeyResolverFromEnv pins the env-var fallback: with no keychain entry
// the lookup returns the key verbatim and never echoes Token. The keychain
// branch is the default read and is covered by
// strategy_api_key_lookup_internal_test.go.
func TestAPIKeyResolverFromEnv(t *testing.T) {
	t.Setenv(EnvAPIKeyVar, "AIza-test-key")
	r := NewAPIKeyResolver()
	creds, err := r.Resolve(context.Background(), []string{"any.scope"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if creds.APIKey != "AIza-test-key" {
		t.Errorf("APIKey = %q; want AIza-test-key", creds.APIKey)
	}
	if creds.Token != "" {
		t.Errorf("Token = %q; want empty (Bearer + api_key are mutually exclusive)", creds.Token)
	}
	if creds.StrategyName != "api_key" {
		t.Errorf("StrategyName = %q; want api_key", creds.StrategyName)
	}
	if creds.SubjectFingerprint == "" {
		t.Error("SubjectFingerprint empty; spec §10.0.1 requires per-subject scoping")
	}
	if strings.Contains(creds.SubjectFingerprint, "AIza") {
		t.Errorf("SubjectFingerprint leaks raw key material: %q", creds.SubjectFingerprint)
	}
}

// TestAPIKeyResolverMissing pins the actionable error envelope: an absent
// key surfaces AUTH_API_KEY_MISSING with the GUM_API_KEY hint so the
// operator can fix it without reading the Google docs.
func TestAPIKeyResolverMissing(t *testing.T) {
	t.Setenv(EnvAPIKeyVar, "")
	r := NewAPIKeyResolver()
	_, err := r.Resolve(context.Background(), nil)
	if err == nil {
		t.Fatal("Resolve with empty env returned nil error")
	}
	var ae *AuthError
	if !errors.As(err, &ae) {
		t.Fatalf("err is not *AuthError: %T", err)
	}
	if ae.Code != "AUTH_API_KEY_MISSING" {
		t.Errorf("Code = %q; want AUTH_API_KEY_MISSING", ae.Code)
	}
	if !strings.Contains(ae.HumanRemediation, "GUM_API_KEY") {
		t.Errorf("HumanRemediation = %q; want mention of GUM_API_KEY env var", ae.HumanRemediation)
	}
}

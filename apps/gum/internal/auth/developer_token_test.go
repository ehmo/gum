package auth

import (
	"errors"
	"testing"
)

func TestDeveloperTokenKeyringKey(t *testing.T) {
	if got := developerTokenKeyringKey(""); got != "gum.google_ads.developer_token."+DefaultAPIKeyProfile {
		t.Errorf("empty profile key = %q", got)
	}
	if got := developerTokenKeyringKey("  work  "); got != "gum.google_ads.developer_token.work" {
		t.Errorf("trimmed key = %q", got)
	}
}

func TestStoreDeveloperTokenNilBackend(t *testing.T) {
	err := StoreDeveloperToken(nil, "default", "tok")
	var ae *AuthError
	if !errors.As(err, &ae) || ae.Code != "AUTH_KEYCHAIN_UNAVAILABLE" {
		t.Fatalf("err = %v; want AUTH_KEYCHAIN_UNAVAILABLE AuthError", err)
	}
}

func TestStoreLookupDeleteDeveloperToken(t *testing.T) {
	kb := &mockKeyring{data: map[string]string{}}
	if err := StoreDeveloperToken(kb, "work", "  dev-tok  "); err != nil {
		t.Fatalf("Store: %v", err)
	}
	if got := LookupDeveloperToken(kb, "work"); got != "dev-tok" {
		t.Errorf("Lookup = %q; want trimmed dev-tok", got)
	}
	if err := DeleteDeveloperToken(kb, "work"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if got := LookupDeveloperToken(kb, "work"); got != "" {
		t.Errorf("Lookup after delete = %q; want empty", got)
	}
}

func TestLookupDeveloperTokenEnvFallback(t *testing.T) {
	t.Setenv(EnvGoogleAdsDeveloperToken, "env-tok")
	// nil backend → env fallback
	if got := LookupDeveloperToken(nil, "default"); got != "env-tok" {
		t.Errorf("nil-backend lookup = %q; want env-tok", got)
	}
	// empty keychain → env fallback
	kb := &mockKeyring{data: map[string]string{}}
	if got := LookupDeveloperToken(kb, "default"); got != "env-tok" {
		t.Errorf("empty-keychain lookup = %q; want env-tok", got)
	}
}

func TestDeleteDeveloperTokenNilBackend(t *testing.T) {
	if err := DeleteDeveloperToken(nil, "default"); err != nil {
		t.Errorf("nil-backend delete = %v; want nil", err)
	}
}

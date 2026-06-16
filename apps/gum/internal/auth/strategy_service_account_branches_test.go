package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// TestNewServiceAccountResolverUnreadableFileSurfacesUnreadable pins
// the `os.ReadFile err && !IsNotExist → AUTH_SA_KEY_UNREADABLE` arm
// (strategy_service_account.go:71-75). Reached by chmod-ing the file
// to 000 so ReadFile returns EACCES (which isn't IsNotExist).
// Skipped under euid 0.
func TestNewServiceAccountResolverUnreadableFileSurfacesUnreadable(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("EACCES not surfaced when running as root")
	}
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "key.json")
	if err := os.WriteFile(keyPath, []byte("{}"), 0o600); err != nil {
		t.Fatalf("plant: %v", err)
	}
	if err := os.Chmod(keyPath, 0o000); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(keyPath, 0o600) })

	_, err := NewServiceAccountResolver(keyPath)
	if err == nil {
		t.Fatal("NewServiceAccountResolver(unreadable) err=nil; want UNREADABLE")
	}
	var ae *AuthError
	if !errors.As(err, &ae) || ae.Code != "AUTH_SA_KEY_UNREADABLE" {
		t.Errorf("err=%v; want AUTH_SA_KEY_UNREADABLE", err)
	}
}

// TestNewServiceAccountResolverEmptyClientEmailSurfacesInvalid pins
// the `cfg.Email == "" → AUTH_SA_KEY_INVALID` arm
// (strategy_service_account.go:87-93). Reached by feeding a key JSON
// with `client_email: ""` — JWTConfigFromJSON accepts the shape but
// the Email field is empty.
func TestNewServiceAccountResolverEmptyClientEmailSurfacesInvalid(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "key.json")
	data := makeStubServiceAccountJSON(t, "", "https://example.invalid/token")
	if err := os.WriteFile(keyPath, data, 0o600); err != nil {
		t.Fatalf("plant: %v", err)
	}
	_, err := NewServiceAccountResolver(keyPath)
	if err == nil {
		t.Fatal("NewServiceAccountResolver(empty email) err=nil; want INVALID")
	}
	var ae *AuthError
	if !errors.As(err, &ae) || ae.Code != "AUTH_SA_KEY_INVALID" {
		t.Errorf("err=%v; want AUTH_SA_KEY_INVALID", err)
	}
}

// TestServiceAccountResolverResolveReparseErrorSurfacesInvalid pins
// Resolve's `JWTConfigFromJSON err → AUTH_SA_KEY_INVALID` arm
// (strategy_service_account.go:113-119). Reached by mutating the
// cached keyBytes to garbage after a successful construction — the
// reparse with scopes then trips JWTConfigFromJSON.
func TestServiceAccountResolverResolveReparseErrorSurfacesInvalid(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "key.json")
	data := makeStubServiceAccountJSON(t, "x@p.iam.gserviceaccount.com", "https://example.invalid/token")
	if err := os.WriteFile(keyPath, data, 0o600); err != nil {
		t.Fatalf("plant: %v", err)
	}
	r, err := NewServiceAccountResolver(keyPath)
	if err != nil {
		t.Fatalf("NewServiceAccountResolver: %v", err)
	}
	r.keyBytes = []byte("not-json-at-all")
	_, err = r.Resolve(context.Background(), []string{"scope"})
	if err == nil {
		t.Fatal("Resolve(reparse-broken) err=nil; want INVALID")
	}
	var ae *AuthError
	if !errors.As(err, &ae) || ae.Code != "AUTH_SA_KEY_INVALID" {
		t.Errorf("err=%v; want AUTH_SA_KEY_INVALID", err)
	}
}

// TestServiceAccountResolverTokenExchangeFailedSurfacesError pins
// Resolve's `cfg.TokenSource(ctx).Token() err → AUTH_SA_TOKEN_EXCHANGE_FAILED`
// arm (strategy_service_account.go:124-130). Reached by pointing
// TokenURLOverride at an httptest server that returns 500.
func TestServiceAccountResolverTokenExchangeFailedSurfacesError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = fmt.Fprint(w, "boom")
	}))
	defer srv.Close()

	dir := t.TempDir()
	keyPath := filepath.Join(dir, "key.json")
	data := makeStubServiceAccountJSON(t, "x@p.iam.gserviceaccount.com", srv.URL)
	if err := os.WriteFile(keyPath, data, 0o600); err != nil {
		t.Fatalf("plant: %v", err)
	}
	r, err := NewServiceAccountResolver(keyPath)
	if err != nil {
		t.Fatalf("NewServiceAccountResolver: %v", err)
	}
	r.TokenURLOverride = srv.URL
	_, err = r.Resolve(context.Background(), []string{"scope"})
	if err == nil {
		t.Fatal("Resolve(token-500) err=nil; want TOKEN_EXCHANGE_FAILED")
	}
	var ae *AuthError
	if !errors.As(err, &ae) || ae.Code != "AUTH_SA_TOKEN_EXCHANGE_FAILED" {
		t.Errorf("err=%v; want AUTH_SA_TOKEN_EXCHANGE_FAILED", err)
	}
}

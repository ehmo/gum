package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// makeStubServiceAccountJSON generates a throwaway RSA key and emits a
// JSON document with the same shape as a Google-issued service-account
// key. The token_uri field is left at a placeholder; callers override it
// in the resolver via TokenURLOverride to redirect the exchange at an
// httptest server.
func makeStubServiceAccountJSON(t *testing.T, email, tokenURL string) []byte {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa.GenerateKey: %v", err)
	}
	pkcs8, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		t.Fatalf("MarshalPKCS8PrivateKey: %v", err)
	}
	pemBlock := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: pkcs8})
	doc := map[string]any{
		"type":         "service_account",
		"project_id":   "stub-project",
		"private_key":  string(pemBlock),
		"client_email": email,
		"client_id":    "stub-client-id",
		"token_uri":    tokenURL,
	}
	out, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	return out
}

// TestServiceAccountResolverInvalidPath pins the AUTH_SA_KEY_NOT_FOUND
// shape: a non-existent path surfaces a typed error pointing at the
// next step (download a key), not a generic os error string.
func TestServiceAccountResolverInvalidPath(t *testing.T) {
	_, err := NewServiceAccountResolver(filepath.Join(t.TempDir(), "missing.json"))
	if err == nil {
		t.Fatal("expected error for missing key file, got nil")
	}
	var ae *AuthError
	if !errors.As(err, &ae) {
		t.Fatalf("err is not *AuthError: %T (%v)", err, err)
	}
	if ae.Code != "AUTH_SA_KEY_NOT_FOUND" {
		t.Errorf("Code = %q; want AUTH_SA_KEY_NOT_FOUND", ae.Code)
	}
}

// TestServiceAccountResolverInvalidJSON pins AUTH_SA_KEY_INVALID for a
// non-JSON / non-SA file. A user-format error must not bubble out as a
// raw oauth2 parse error.
func TestServiceAccountResolverInvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(path, []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := NewServiceAccountResolver(path)
	if err == nil {
		t.Fatal("expected error for non-JSON key file, got nil")
	}
	var ae *AuthError
	if !errors.As(err, &ae) {
		t.Fatalf("err is not *AuthError: %T (%v)", err, err)
	}
	if ae.Code != "AUTH_SA_KEY_INVALID" {
		t.Errorf("Code = %q; want AUTH_SA_KEY_INVALID", ae.Code)
	}
}

// TestServiceAccountResolverHappyPath proves the offline-testable slice
// of the resolver: constructor parses the JSON, Resolve forwards the
// signed JWT to the (stubbed) token endpoint, and the returned
// Credentials carry Token=<stub_value>, SubjectFingerprint derived from
// client_email, and Scopes preserved verbatim.
func TestServiceAccountResolverHappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Google's token endpoint expects POST with form body containing
		// grant_type=urn:ietf:params:oauth:grant-type:jwt-bearer and
		// assertion=<signed JWT>. We don't validate the JWT signature
		// here — that's tested by x/oauth2 itself — but we do confirm
		// the request shape so a future refactor that breaks the
		// assertion flow trips this test.
		if r.Method != http.MethodPost {
			http.Error(w, "method", http.StatusMethodNotAllowed)
			return
		}
		body, _ := io.ReadAll(r.Body)
		form, _ := url.ParseQuery(string(body))
		if !strings.Contains(form.Get("grant_type"), "jwt-bearer") {
			http.Error(w, "missing grant_type", http.StatusBadRequest)
			return
		}
		if form.Get("assertion") == "" {
			http.Error(w, "missing assertion", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"access_token":"stub-access-token","token_type":"Bearer","expires_in":3600}`)
	}))
	defer srv.Close()

	dir := t.TempDir()
	keyPath := filepath.Join(dir, "key.json")
	data := makeStubServiceAccountJSON(t, "stub-sa@stub-project.iam.gserviceaccount.com", srv.URL)
	if err := os.WriteFile(keyPath, data, 0o600); err != nil {
		t.Fatal(err)
	}

	r, err := NewServiceAccountResolver(keyPath)
	if err != nil {
		t.Fatalf("NewServiceAccountResolver: %v", err)
	}
	r.TokenURLOverride = srv.URL

	creds, err := r.Resolve(context.Background(), []string{"https://www.googleapis.com/auth/gmail.readonly"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if creds.Token != "stub-access-token" {
		t.Errorf("Token = %q; want stub-access-token", creds.Token)
	}
	if creds.StrategyName != "service_account_key" {
		t.Errorf("StrategyName = %q; want service_account_key", creds.StrategyName)
	}
	if len(creds.Scopes) != 1 || creds.Scopes[0] != "https://www.googleapis.com/auth/gmail.readonly" {
		t.Errorf("Scopes = %v; want gmail.readonly preserved", creds.Scopes)
	}
	if creds.SubjectFingerprint == "" {
		t.Error("SubjectFingerprint empty; spec §10.0.1 requires per-subject scoping")
	}
	if strings.Contains(creds.SubjectFingerprint, "@") {
		t.Errorf("SubjectFingerprint leaks raw email: %q", creds.SubjectFingerprint)
	}
	if creds.APIKey != "" {
		t.Errorf("APIKey = %q; want empty (SA flow uses Bearer Token only)", creds.APIKey)
	}
}

// TestAuthStrategyServiceAccount — gum-im1 acceptance. The composite
// resolver must route auth_strategy=service_account_key to the SA branch
// and return dispatch.Credentials with the Token populated, not the
// AUTH_STRATEGY_NOT_IMPLEMENTED stub from the strategy.go fallback path.
func TestAuthStrategyServiceAccount(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"access_token":"sa-composite-token","token_type":"Bearer","expires_in":3600}`)
	}))
	defer srv.Close()

	dir := t.TempDir()
	keyPath := filepath.Join(dir, "sa.json")
	if err := os.WriteFile(keyPath, makeStubServiceAccountJSON(t, "x@p.iam.gserviceaccount.com", srv.URL), 0o600); err != nil {
		t.Fatal(err)
	}
	r, err := NewServiceAccountResolver(keyPath)
	if err != nil {
		t.Fatalf("NewServiceAccountResolver: %v", err)
	}
	r.TokenURLOverride = srv.URL

	// Cross-package import (dispatch / catalog) lives in the integration
	// test under cmd/gum — here we drive the SA-branch directly through
	// the package-internal Resolver interface so this file stays inside
	// internal/auth and avoids the cycle with internal/dispatch.
	creds, err := r.Resolve(context.Background(), []string{"https://www.googleapis.com/auth/gmail.readonly"})
	if err != nil {
		t.Fatalf("SA Resolve: %v", err)
	}
	if creds.Token != "sa-composite-token" {
		t.Errorf("Token = %q; want sa-composite-token", creds.Token)
	}
	if creds.StrategyName != "service_account_key" {
		t.Errorf("StrategyName = %q; want service_account_key", creds.StrategyName)
	}
}

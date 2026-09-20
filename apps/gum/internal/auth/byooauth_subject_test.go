// Regression test for gum-mc67: the byo_oauth subject fingerprint was derived
// from the refresh token, so it changed on every re-login.
package auth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/zalando/go-keyring"
)

// rotatingTokenServer issues a new refresh token on every authorization-code
// exchange while keeping the account identity fixed, which is what Google does
// when the same user re-runs `gum login` with prompt=consent.
func rotatingTokenServer(t *testing.T, sub, email string) *httptest.Server {
	t.Helper()
	var issued int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.Form.Get("grant_type") {
		case "authorization_code":
			issued++
			_, _ = fmt.Fprintf(w,
				`{"access_token":"at-%d","refresh_token":"rt-%d","expires_in":3600,"token_type":"Bearer","id_token":%q}`,
				issued, issued, idTokenWithClaims(sub, email))
		case "refresh_token":
			_, _ = fmt.Fprintf(w, `{"access_token":"at-refreshed","expires_in":3600,"token_type":"Bearer"}`)
		default:
			http.Error(w, "unknown grant_type", http.StatusBadRequest)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func idTokenWithClaims(sub, email string) string {
	enc := base64.RawURLEncoding.EncodeToString
	header := enc([]byte(`{"alg":"none","typ":"JWT"}`))
	claims := map[string]string{"sub": sub}
	if email != "" {
		claims["email"] = email
	}
	payload, _ := json.Marshal(claims)
	return header + "." + enc(payload) + ".sig"
}

func newLoggedInByo(t *testing.T, tokenSrv *httptest.Server) *ByoOAuth {
	t.Helper()
	authSrv := newFakeAuthServer(t, "USE_GUM_STATE")
	t.Cleanup(authSrv.Close)
	b := NewByoOAuth(ByoOAuthConfig{
		ClientID:      "user-client-id",
		Scopes:        []string{"https://www.googleapis.com/auth/webmasters.readonly"},
		TokenEndpoint: tokenSrv.URL,
		AuthEndpoint:  authSrv.URL,
	}, NewOSKeyring())
	b.client = tokenSrv.Client()
	b.BrowserOpener = func(authURL string) error { go followAuthURL(authURL); return nil }
	return b
}

// Spec §10.0.1: the OAuth principal dimension is the lower-case account email,
// or the `sub` claim. Deriving it from the refresh token instead meant a second
// `gum login` for the same account produced a new fingerprint, so every
// semantic-cache row, result handle and gain-ledger row written before the
// re-login became unreachable.
func TestByoOAuthFingerprintSurvivesReLogin(t *testing.T) {
	keyring.MockInit()
	srv := rotatingTokenServer(t, "subject-1", "User@Example.COM")

	first := newLoggedInByo(t, srv)
	credsA, err := first.Login(context.Background())
	if err != nil {
		t.Fatalf("first Login: %v", err)
	}
	grantA, _ := first.kb.Get(first.keyringKey())

	second := newLoggedInByo(t, srv)
	credsB, err := second.Login(context.Background())
	if err != nil {
		t.Fatalf("second Login: %v", err)
	}
	grantB, _ := second.kb.Get(second.keyringKey())

	if strings.Contains(grantA, `"rt-2"`) || !strings.Contains(grantB, `"rt-2"`) {
		t.Fatalf("test setup did not rotate the refresh token: A=%s B=%s", grantA, grantB)
	}
	if credsA.SubjectFingerprint == "" {
		t.Fatal("first login produced an empty subject fingerprint")
	}
	if credsA.SubjectFingerprint != credsB.SubjectFingerprint {
		t.Errorf("fingerprint changed across re-login: %q then %q",
			credsA.SubjectFingerprint, credsB.SubjectFingerprint)
	}

	// The refresh path must land on the same principal as the login path, or
	// the first silent refresh moves the whole cache namespace again.
	second.cached = nil
	refreshed, err := second.Acquire(context.Background())
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if refreshed.SubjectFingerprint != credsB.SubjectFingerprint {
		t.Errorf("refresh fingerprint = %q; want %q",
			refreshed.SubjectFingerprint, credsB.SubjectFingerprint)
	}
}

// A different Google account must still land on a different fingerprint, or the
// per-principal cache partitioning §10.0.1 exists for would collapse.
func TestByoOAuthFingerprintDiffersPerAccount(t *testing.T) {
	keyring.MockInit()

	one := newLoggedInByo(t, rotatingTokenServer(t, "subject-1", "a@example.com"))
	credsA, err := one.Login(context.Background())
	if err != nil {
		t.Fatalf("Login one: %v", err)
	}
	two := newLoggedInByo(t, rotatingTokenServer(t, "subject-2", "b@example.com"))
	credsB, err := two.Login(context.Background())
	if err != nil {
		t.Fatalf("Login two: %v", err)
	}
	if credsA.SubjectFingerprint == credsB.SubjectFingerprint {
		t.Errorf("two accounts share fingerprint %q", credsA.SubjectFingerprint)
	}
}

// The email claim is the spec's first choice and is case-insensitive at Google,
// so the fingerprint must not move when the claim's case changes.
func TestByoOAuthFingerprintNormalizesEmailCase(t *testing.T) {
	keyring.MockInit()

	upper := newLoggedInByo(t, rotatingTokenServer(t, "subject-1", "User@Example.COM"))
	credsUpper, err := upper.Login(context.Background())
	if err != nil {
		t.Fatalf("Login upper: %v", err)
	}
	lower := newLoggedInByo(t, rotatingTokenServer(t, "subject-1", "user@example.com"))
	credsLower, err := lower.Login(context.Background())
	if err != nil {
		t.Fatalf("Login lower: %v", err)
	}
	if credsUpper.SubjectFingerprint != credsLower.SubjectFingerprint {
		t.Errorf("email case changed the fingerprint: %q vs %q",
			credsUpper.SubjectFingerprint, credsLower.SubjectFingerprint)
	}
}

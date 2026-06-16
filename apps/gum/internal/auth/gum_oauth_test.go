package auth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/zalando/go-keyring"
)

// TestManagedOAuthLiveCanaryRequired pins spec §7 lines 1212-1224: gum_oauth
// MUST refuse to start unless the requested scopes have been promoted to
// (active, verified, ready, passing) in docs/auth-managed-scopes.v1.json.
// The shipped manifest has all scopes in the planned/pending state, so a
// fresh Login or Resolve must fail with GUM_OAUTH_MANAGED_CLIENT_NOT_READY
// and the missing scopes echoed in MissingComponents.
func TestManagedOAuthLiveCanaryRequired(t *testing.T) {
	keyring.MockInit()
	g := &GumOAuth{Vault: NewCredentialVault(&OSKeyring{})}

	scopes := []string{"https://www.googleapis.com/auth/gmail.readonly"}
	_, err := g.Resolve(context.Background(), scopes)
	var ae *AuthError
	if !errors.As(err, &ae) {
		t.Fatalf("Resolve: want *AuthError, got %T: %v", err, err)
	}
	if ae.Code != "GUM_OAUTH_MANAGED_CLIENT_NOT_READY" {
		t.Errorf("Code = %q; want GUM_OAUTH_MANAGED_CLIENT_NOT_READY", ae.Code)
	}
	if ae.Strategy != "gum_oauth" {
		t.Errorf("Strategy = %q; want gum_oauth", ae.Strategy)
	}
	if len(ae.MissingComponents) == 0 {
		t.Errorf("MissingComponents empty; want offending scopes echoed")
	}
	if strings.Contains(ae.HumanRemediation, "gum auth login") {
		t.Errorf("not-ready hint wrongly suggests `gum auth login`: %s", ae.HumanRemediation)
	}

	// Empty scope set also fails the gate (active_scope_required sentinel).
	_, err = g.Login(context.Background(), nil)
	if !errors.As(err, &ae) || ae.Code != "GUM_OAUTH_MANAGED_CLIENT_NOT_READY" {
		t.Errorf("Login(nil scopes): want GUM_OAUTH_MANAGED_CLIENT_NOT_READY, got %v", err)
	}
}

// TestAuthLoopbackStateRequired pins spec §7's CSRF protection: the loopback
// callback MUST reject any request whose `state` parameter is missing or
// does not match the value GUM issued at authorization-URL build time.
// Without this, an attacker who can hit the loopback redirect could inject
// an attacker-controlled `code` and bind it to the user's vault.
func TestAuthLoopbackStateRequired(t *testing.T) {
	keyring.MockInit()
	manifest := promotedManifest("https://example.test/scope/a")
	tokenSrv, _ := newFakeTokenServer(t)
	defer tokenSrv.Close()
	authSrv := newFakeAuthServer(t, "wrong-state")
	defer authSrv.Close()

	g := &GumOAuth{
		Vault:            NewCredentialVault(&OSKeyring{}),
		AuthURL:          authSrv.URL,
		TokenURL:         tokenSrv.URL,
		HTTPClient:       tokenSrv.Client(),
		ManifestBody:     manifest,
		ClientIDOverride: "test-client-id",
		BrowserOpener: func(authURL string) error {
			// Drive the loopback as if the user completed the consent.
			// fakeAuthServer redirects with state=wrong-state, which will
			// not match GUM's issued state — the callback MUST reject it.
			go followAuthURL(authURL)
			return nil
		},
	}
	scopes := []string{"https://example.test/scope/a"}
	_, err := g.Login(context.Background(), scopes)
	var ae *AuthError
	if !errors.As(err, &ae) {
		t.Fatalf("Login: want *AuthError, got %T: %v", err, err)
	}
	if ae.Code != "AUTH_LOOPBACK_STATE_MISMATCH" {
		t.Errorf("Code = %q; want AUTH_LOOPBACK_STATE_MISMATCH", ae.Code)
	}
}

// TestAuthScopeUpgradeFullReconsent pins spec §7's full-reconsent
// requirement (manifest scope_expansion_mode=full_reconsent): the
// authorization URL MUST include prompt=consent so adding a scope forces
// the user through the full consent screen rather than the silent
// incremental upgrade Google offers by default.
func TestAuthScopeUpgradeFullReconsent(t *testing.T) {
	keyring.MockInit()
	manifest := promotedManifest("https://example.test/scope/a")

	captured := make(chan string, 1)
	authSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured <- r.URL.RawQuery
		// Don't redirect — we only need to capture the auth URL. Login
		// will time out below, which is fine; we assert on captured first.
		http.Error(w, "captured", http.StatusOK)
	}))
	defer authSrv.Close()

	g := &GumOAuth{
		Vault:            NewCredentialVault(&OSKeyring{}),
		AuthURL:          authSrv.URL,
		TokenURL:         "http://invalid.test",
		HTTPClient:       authSrv.Client(),
		ManifestBody:     manifest,
		ClientIDOverride: "test-client-id",
		BrowserOpener: func(authURL string) error {
			// Issue a GET so the authSrv captures the query params, but
			// don't redirect to the loopback — we cancel Login below
			// once the query is captured. DisableKeepAlives so the
			// transport does not park persistConns past goleak.
			tr := &http.Transport{DisableKeepAlives: true}
			go func() {
				defer tr.CloseIdleConnections()
				client := &http.Client{Transport: tr}
				if resp, err := client.Get(authURL); err == nil {
					_, _ = io.Copy(io.Discard, resp.Body)
					_ = resp.Body.Close()
				}
			}()
			return nil
		},
	}
	// Drive Login until the auth URL is captured, then cancel so the
	// background goroutine returns (otherwise the package-level goroutine
	// leak detector flags us).
	loginCtx, cancelLogin := context.WithCancel(context.Background())
	t.Cleanup(cancelLogin)
	loginDone := make(chan struct{})
	go func() {
		defer close(loginDone)
		_, _ = g.Login(loginCtx, []string{"https://example.test/scope/a"})
	}()

	raw := <-captured
	cancelLogin()
	<-loginDone
	q, err := url.ParseQuery(raw)
	if err != nil {
		t.Fatalf("parse captured query: %v", err)
	}
	if got := q.Get("prompt"); got != "consent" {
		t.Errorf("prompt = %q; want consent (manifest scope_expansion_mode=full_reconsent)", got)
	}
	if got := q.Get("access_type"); got != "offline" {
		t.Errorf("access_type = %q; want offline (so refresh_token is issued)", got)
	}
	if got := q.Get("code_challenge_method"); got != "S256" {
		t.Errorf("code_challenge_method = %q; want S256", got)
	}
	if got := q.Get("scope"); got != "https://example.test/scope/a openid" {
		t.Errorf("scope = %q; want requested scope plus openid for ID-token subject identity", got)
	}
	if q.Get("state") == "" {
		t.Errorf("state must be non-empty for CSRF protection")
	}
	if q.Get("code_challenge") == "" {
		t.Errorf("code_challenge must be non-empty (PKCE)")
	}
}

// TestGumOAuthLoginHappyPath runs the full PKCE + loopback + CSRF flow
// against a httptest fake provider, asserts the refresh token is stored in
// the vault, then asserts Resolve refreshes the access token without
// touching the consent screen again.
func TestGumOAuthLoginHappyPath(t *testing.T) {
	keyring.MockInit()
	manifest := promotedManifest("https://example.test/scope/a")
	tokenSrv, getRefreshCount := newFakeTokenServer(t)
	defer tokenSrv.Close()
	authSrv := newFakeAuthServer(t, "USE_GUM_STATE")
	defer authSrv.Close()

	g := &GumOAuth{
		Vault:            NewCredentialVault(&OSKeyring{}),
		AuthURL:          authSrv.URL,
		TokenURL:         tokenSrv.URL,
		HTTPClient:       tokenSrv.Client(),
		ManifestBody:     manifest,
		ClientIDOverride: "test-client-id",
		BrowserOpener: func(authURL string) error {
			go followAuthURL(authURL)
			return nil
		},
	}
	scopes := []string{"https://example.test/scope/a"}
	creds, err := g.Login(context.Background(), scopes)
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if creds.Token == "" || creds.StrategyName != "gum_oauth" {
		t.Errorf("creds = %+v; want non-empty Token + StrategyName=gum_oauth", creds)
	}

	// Resolve should refresh without re-running the browser flow.
	g.BrowserOpener = func(string) error { t.Fatal("Resolve must not open the browser"); return nil }
	refreshed, err := g.Resolve(context.Background(), scopes)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if refreshed.Token == "" {
		t.Errorf("refreshed token is empty")
	}
	if getRefreshCount() < 1 {
		t.Errorf("refresh exchange count = %d; want >= 1", getRefreshCount())
	}
}

func TestGumOAuthLoginPartitionsVaultByIDTokenSubject(t *testing.T) {
	keyring.MockInit()
	manifest := promotedManifest("https://example.test/scope/a")
	scopes := []string{"https://example.test/scope/a"}
	kb := &mockKeyring{data: map[string]string{}}

	loginAs := func(sub, refreshToken string) *Credentials {
		tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if err := r.ParseForm(); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			switch r.Form.Get("grant_type") {
			case "authorization_code":
				_, _ = fmt.Fprintf(w,
					`{"access_token":"at-%s","refresh_token":%q,"expires_in":3600,"token_type":"Bearer","id_token":%q}`,
					sub, refreshToken, fakeIDToken(sub))
			case "refresh_token":
				_, _ = fmt.Fprintf(w,
					`{"access_token":"refreshed-from-%s","expires_in":3600,"token_type":"Bearer"}`,
					r.Form.Get("refresh_token"))
			default:
				http.Error(w, "unknown grant_type", http.StatusBadRequest)
			}
		}))
		t.Cleanup(tokenSrv.Close)
		authSrv := newFakeAuthServer(t, "USE_GUM_STATE")
		t.Cleanup(authSrv.Close)

		g := &GumOAuth{
			Vault:            NewCredentialVault(kb),
			AuthURL:          authSrv.URL,
			TokenURL:         tokenSrv.URL,
			HTTPClient:       tokenSrv.Client(),
			ManifestBody:     manifest,
			ClientIDOverride: "test-client-id",
			BrowserOpener: func(authURL string) error {
				go followAuthURL(authURL)
				return nil
			},
		}
		creds, err := g.Login(context.Background(), scopes)
		if err != nil {
			t.Fatalf("Login(%s): %v", sub, err)
		}
		return creds
	}

	a := loginAs("account-A", "rt-account-A")
	b := loginAs("account-B", "rt-account-B")
	if a.SubjectFingerprint == b.SubjectFingerprint {
		t.Fatalf("same scopes for different subjects collided: %q", a.SubjectFingerprint)
	}

	v := NewCredentialVault(kb)
	aRT, err := v.LookupRefreshToken(gumOAuthStrategyName, a.SubjectFingerprint, scopes)
	if err != nil {
		t.Fatalf("lookup A token: %v", err)
	}
	bRT, err := v.LookupRefreshToken(gumOAuthStrategyName, b.SubjectFingerprint, scopes)
	if err != nil {
		t.Fatalf("lookup B token: %v", err)
	}
	if aRT != "rt-account-A" || bRT != "rt-account-B" {
		t.Fatalf("stored refresh tokens = %q/%q; want account-partitioned tokens", aRT, bRT)
	}
	current, err := v.LookupGumOAuthSubject(scopes)
	if err != nil {
		t.Fatalf("LookupGumOAuthSubject: %v", err)
	}
	if current != b.SubjectFingerprint {
		t.Errorf("current subject = %q; want latest login subject %q", current, b.SubjectFingerprint)
	}
}

// --- fakes ---

// promotedManifest returns a managed-scopes manifest JSON body where
// `scope` is fully promoted to (active, verified, ready, passing). Tests
// inject this via GumOAuth.ManifestBody so the gate accepts the scope.
func promotedManifest(scope string) []byte {
	body := map[string]any{
		"schema_version": 1,
		"client_policy": map[string]any{
			"flow":                   "installed_app_pkce",
			"embedded_client_secret": false,
			"redirect_method":        "loopback",
			"scope_expansion_mode":   "full_reconsent",
			"active_scope_rule":      "test-only manifest",
			"client_id":              "test-client-id",
		},
		"scopes": []map[string]any{
			{
				"scope":                  scope,
				"service":                "test",
				"category":               "sensitive",
				"status":                 "active",
				"verification_state":     "verified",
				"project_evidence_state": "ready",
				"live_canary_state":      "passing",
				"evidence":               "fake",
			},
		},
	}
	out, _ := json.Marshal(body)
	return out
}

// newFakeTokenServer returns an httptest server that handles
// authorization-code and refresh-token exchanges, returning a canned
// access_token + refresh_token. The second return value is a callable
// snapshot of how many refresh-token exchanges have happened.
func newFakeTokenServer(t *testing.T) (*httptest.Server, func() int) {
	t.Helper()
	var refreshCount int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		gt := r.Form.Get("grant_type")
		w.Header().Set("Content-Type", "application/json")
		switch gt {
		case "authorization_code":
			if r.Form.Get("code") == "" || r.Form.Get("code_verifier") == "" {
				http.Error(w, "missing code/verifier", 400)
				return
			}
			_, _ = fmt.Fprintf(w, `{"access_token":"at-1","refresh_token":"rt-1","expires_in":3600,"token_type":"Bearer","id_token":%q}`, fakeIDToken("subject-1"))
		case "refresh_token":
			refreshCount++
			if r.Form.Get("refresh_token") == "" {
				http.Error(w, "missing refresh_token", 400)
				return
			}
			_, _ = fmt.Fprintf(w, `{"access_token":"at-refreshed-%d","expires_in":3600,"token_type":"Bearer"}`, refreshCount)
		default:
			http.Error(w, "unknown grant_type", 400)
		}
	}))
	return srv, func() int { return refreshCount }
}

func fakeIDToken(sub string) string {
	enc := base64.RawURLEncoding.EncodeToString
	header := enc([]byte(`{"alg":"none","typ":"JWT"}`))
	payload, _ := json.Marshal(map[string]string{"sub": sub})
	return header + "." + enc(payload) + ".sig"
}

// newFakeAuthServer returns an httptest server that simulates Google's
// authorization endpoint by immediately redirecting the user-agent to the
// loopback redirect_uri with `code=fake-code` and the given state value.
//
// state="USE_GUM_STATE" is a sentinel: the fake echoes back the state the
// caller passed in the query string (so the happy path works). Any other
// value is sent verbatim so tests can simulate CSRF mismatch.
func newFakeAuthServer(t *testing.T, stateOverride string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		redirect := q.Get("redirect_uri")
		state := q.Get("state")
		if stateOverride != "USE_GUM_STATE" {
			state = stateOverride
		}
		dest := redirect + "?code=fake-code&state=" + url.QueryEscape(state)
		http.Redirect(w, r, dest, http.StatusFound)
	}))
}

// followAuthURL GETs the supplied authorization URL with redirects
// enabled, simulating a user who clicks "Approve" in the browser. The
// returned body is discarded; we only care that the loopback callback
// fires. Keep-alives are disabled so the test does not leak idle
// persistConn goroutines past goleak.VerifyNone.
func followAuthURL(authURL string) {
	tr := &http.Transport{DisableKeepAlives: true}
	defer tr.CloseIdleConnections()
	client := &http.Client{Transport: tr}
	resp, err := client.Get(authURL)
	if err == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}
}

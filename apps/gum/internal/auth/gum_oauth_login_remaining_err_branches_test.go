package auth_test

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

	"github.com/ehmo/gum/internal/auth"
)

// errStoreKeyring is a KeyringBackend whose Set fails with a sentinel;
// Get and Delete are no-ops. Drives Login past the happy-path token
// exchange into the Vault.StoreRefreshToken err arm.
type errStoreKeyring struct {
	setErr error
}

func (e errStoreKeyring) Get(string) (string, error) { return "", nil }
func (e errStoreKeyring) Set(string, string) error   { return e.setErr }
func (e errStoreKeyring) Delete(string) error        { return nil }

// promotedManifestFor returns a managed-scopes manifest JSON body
// where `scope` is fully promoted (mirrors the same-named helper in
// the auth package's internal test file; copied here because the
// internal helper isn't exported).
func promotedManifestFor(scope string) []byte {
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

func fakeIDTokenFor(sub string) string {
	enc := base64.RawURLEncoding.EncodeToString
	header := enc([]byte(`{"alg":"none","typ":"JWT"}`))
	payload, _ := json.Marshal(map[string]string{"sub": sub})
	return header + "." + enc(payload) + ".sig"
}

func newAuthRedirectSrv() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		dest := q.Get("redirect_uri") + "?code=fake-code&state=" + url.QueryEscape(q.Get("state"))
		http.Redirect(w, r, dest, http.StatusFound)
	}))
}

func followAuth(authURL string) {
	tr := &http.Transport{DisableKeepAlives: true}
	defer tr.CloseIdleConnections()
	client := &http.Client{Transport: tr}
	resp, err := client.Get(authURL)
	if err == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}
}

// TestGumOAuthLoginExchangeCodeFailureSurfacesErr pins Login's
// `exchangeCode err → return nil, err` arm (gum_oauth.go:215-217).
// Reached when the loopback callback fires successfully but the token
// endpoint rejects the authorization_code grant (revoked client,
// project disabled). Login MUST propagate postToken's err verbatim so
// operators see GUM_OAUTH_TOKEN_EXCHANGE_FAILED rather than a generic
// "login failed" — the typed err carries the upstream status code.
func TestGumOAuthLoginExchangeCodeFailureSurfacesErr(t *testing.T) {
	tokSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":"invalid_request","error_description":"bad code"}`)
	}))
	t.Cleanup(tokSrv.Close)
	authSrv := newAuthRedirectSrv()
	t.Cleanup(authSrv.Close)

	g := &auth.GumOAuth{
		Vault:            auth.NewCredentialVault(errStoreKeyring{}),
		AuthURL:          authSrv.URL,
		TokenURL:         tokSrv.URL,
		HTTPClient:       tokSrv.Client(),
		ManifestBody:     promotedManifestFor("https://example.test/scope/a"),
		ClientIDOverride: "test-client-id",
		BrowserOpener: func(u string) error {
			go followAuth(u)
			return nil
		},
	}
	_, err := g.Login(context.Background(), []string{"https://example.test/scope/a"})
	if err == nil {
		t.Fatal("Login(token endpoint 400)=nil err; want exchangeCode err propagation")
	}
	var ae *auth.AuthError
	if !errors.As(err, &ae) {
		t.Fatalf("err type=%T %v; want *AuthError from postToken", err, err)
	}
	if ae.Code != "GUM_OAUTH_TOKEN_EXCHANGE_FAILED" {
		t.Errorf("Code=%q; want GUM_OAUTH_TOKEN_EXCHANGE_FAILED", ae.Code)
	}
}

// TestGumOAuthLoginNoRefreshTokenSurfacesTypedErr pins Login's
// `tok.RefreshToken == "" → GUM_OAUTH_NO_REFRESH_TOKEN` arm
// (gum_oauth.go:218-224). Reached when the token endpoint returns 200
// + an access_token but omits refresh_token (provider misconfig where
// prompt=consent or access_type=offline got lost). Login MUST refuse
// to persist a half-credential set; without the refresh token, the
// next Resolve would force another browser flow — surfacing this
// surface lets operators fix the request params instead.
func TestGumOAuthLoginNoRefreshTokenSurfacesTypedErr(t *testing.T) {
	tokSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"access_token":"at-1","expires_in":3600,"token_type":"Bearer"}`)
	}))
	t.Cleanup(tokSrv.Close)
	authSrv := newAuthRedirectSrv()
	t.Cleanup(authSrv.Close)

	g := &auth.GumOAuth{
		Vault:            auth.NewCredentialVault(errStoreKeyring{}),
		AuthURL:          authSrv.URL,
		TokenURL:         tokSrv.URL,
		HTTPClient:       tokSrv.Client(),
		ManifestBody:     promotedManifestFor("https://example.test/scope/a"),
		ClientIDOverride: "test-client-id",
		BrowserOpener: func(u string) error {
			go followAuth(u)
			return nil
		},
	}
	_, err := g.Login(context.Background(), []string{"https://example.test/scope/a"})
	if err == nil {
		t.Fatal("Login(no refresh_token)=nil err; want GUM_OAUTH_NO_REFRESH_TOKEN")
	}
	var ae *auth.AuthError
	if !errors.As(err, &ae) {
		t.Fatalf("err type=%T %v; want *AuthError", err, err)
	}
	if ae.Code != "GUM_OAUTH_NO_REFRESH_TOKEN" {
		t.Errorf("Code=%q; want GUM_OAUTH_NO_REFRESH_TOKEN", ae.Code)
	}
	if !strings.Contains(ae.HumanRemediation, "access_type=offline") {
		t.Errorf("HumanRemediation=%q; want 'access_type=offline' hint for the operator", ae.HumanRemediation)
	}
}

// TestGumOAuthLoginVaultStoreFailurePropagates pins Login's
// `Vault.StoreRefreshToken err → return nil, err` arm
// (gum_oauth.go:226-228). Reached when the token endpoint returns a
// well-formed refresh_token but the keychain rejects the Set (locked
// keychain on macOS, dbus down on Linux). Login MUST surface the
// vault err so operators see a recoverable "keychain unavailable"
// surface rather than silently dropping the refresh token — which
// would leave the next Resolve forcing another browser flow.
func TestGumOAuthLoginVaultStoreFailurePropagates(t *testing.T) {
	tokSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"access_token":"at-1","refresh_token":"rt-1","expires_in":3600,"token_type":"Bearer","id_token":%q}`, fakeIDTokenFor("subject-store-failure"))
	}))
	t.Cleanup(tokSrv.Close)
	authSrv := newAuthRedirectSrv()
	t.Cleanup(authSrv.Close)

	sentinel := errors.New("keychain locked")
	g := &auth.GumOAuth{
		Vault:            auth.NewCredentialVault(errStoreKeyring{setErr: sentinel}),
		AuthURL:          authSrv.URL,
		TokenURL:         tokSrv.URL,
		HTTPClient:       tokSrv.Client(),
		ManifestBody:     promotedManifestFor("https://example.test/scope/a"),
		ClientIDOverride: "test-client-id",
		BrowserOpener: func(u string) error {
			go followAuth(u)
			return nil
		},
	}
	_, err := g.Login(context.Background(), []string{"https://example.test/scope/a"})
	if err == nil {
		t.Fatal("Login(vault.Set errs)=nil err; want vault err propagation")
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("err=%v; want errors.Is(err, sentinel) — Login must propagate vault err verbatim", err)
	}
}

// TestGumOAuthLoginOpenBrowserErrorWraps pins Login's
// `opener err → return nil, fmt.Errorf("gum_oauth: open browser: ...")`
// arm (gum_oauth.go:205-207). Reached when the BrowserOpener fails
// (no display server, sandboxed CI without X). The wrap label
// "gum_oauth: open browser:" is the operator's grep handle to
// distinguish browser-launch failure from token-endpoint failure
// downstream — without the wrap the raw err's origin is ambiguous.
func TestGumOAuthLoginOpenBrowserErrorWraps(t *testing.T) {
	tokSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"access_token":"at-1"}`)
	}))
	t.Cleanup(tokSrv.Close)
	authSrv := newAuthRedirectSrv()
	t.Cleanup(authSrv.Close)

	g := &auth.GumOAuth{
		Vault:            auth.NewCredentialVault(errStoreKeyring{}),
		AuthURL:          authSrv.URL,
		TokenURL:         tokSrv.URL,
		HTTPClient:       tokSrv.Client(),
		ManifestBody:     promotedManifestFor("https://example.test/scope/a"),
		ClientIDOverride: "test-client-id",
		BrowserOpener: func(string) error {
			return errors.New("xdg-open not found")
		},
	}
	_, err := g.Login(context.Background(), []string{"https://example.test/scope/a"})
	if err == nil {
		t.Fatal("Login(opener errs)=nil err; want wrapped open-browser err")
	}
	if !strings.Contains(err.Error(), "gum_oauth: open browser") {
		t.Errorf("err=%q; want 'gum_oauth: open browser' wrap (distinguishes from token err)", err)
	}
	if !strings.Contains(err.Error(), "xdg-open not found") {
		t.Errorf("err=%q; want underlying opener err visible in chain", err)
	}
}

// TestGumOAuthPostTokenBadURLSurfacesTokenRequestWrap pins postToken's
// `http.NewRequestWithContext err → "gum_oauth: token request:" wrap`
// arm (gum_oauth.go:311-313). Reached via a malformed TokenURL with
// an embedded DEL char that http.NewRequest rejects. Exercised through
// Resolve's exchangeRefresh path so the err chain Login/Resolve
// surface is end-to-end testable. The wrap label is operator-readable
// and disambiguates request-build failure from token-do failure.
func TestGumOAuthPostTokenBadURLSurfacesTokenRequestWrap(t *testing.T) {
	g := &auth.GumOAuth{
		Vault:        auth.NewCredentialVault(hitTokenKeyring{rt: "stale-rt"}),
		TokenURL:     "http://example.com/\x7ftoken",
		ManifestBody: promotedManifestFor("https://example.test/scope/a"),
	}
	_, err := g.Resolve(context.Background(), []string{"https://example.test/scope/a"})
	if err == nil {
		t.Fatal("Resolve(bad TokenURL)=nil err; want postToken NewRequest wrap")
	}
	if !strings.Contains(err.Error(), "gum_oauth: token request") {
		t.Errorf("err=%q; want 'gum_oauth: token request' wrap", err)
	}
}

// hitTokenKeyring returns a fixed refresh token so Resolve drives past
// the rt=="" guard into postToken — same pattern as hitKeyring in the
// internal test file, repeated here because cross-package tests can't
// reach unexported test helpers.
type hitTokenKeyring struct{ rt string }

func (h hitTokenKeyring) Get(string) (string, error) { return h.rt, nil }
func (h hitTokenKeyring) Set(string, string) error   { return nil }
func (h hitTokenKeyring) Delete(string) error        { return nil }

// Ensure fmt is used (silences importer if any of the above test bodies
// drop their fmt usage in a future edit). Pinning fmt usage here is
// cheap insurance for a long-lived file.
var _ = fmt.Sprintf

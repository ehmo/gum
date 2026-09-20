package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// sealedKeyring fails every write. Login's storeLoginGrant step is the only
// caller that can surface the failure.
type sealedKeyring struct{ held map[string]string }

func (k *sealedKeyring) Get(key string) (string, error) { return k.held[key], nil }
func (k *sealedKeyring) Set(string, string) error       { return errors.New("keyring sealed") }
func (k *sealedKeyring) Delete(string) error            { return nil }

// byoLoginFixture wires a ByoOAuth against a token endpoint the caller
// controls and an auth endpoint that redirects straight back to the loopback.
func byoLoginFixture(t *testing.T, token http.HandlerFunc) *ByoOAuth {
	t.Helper()
	tokenSrv := httptest.NewServer(token)
	t.Cleanup(tokenSrv.Close)
	authSrv := newFakeAuthServer(t, "USE_GUM_STATE")
	t.Cleanup(authSrv.Close)

	b := NewByoOAuth(ByoOAuthConfig{
		ClientID:      "user-client-id",
		Scopes:        []string{"https://www.googleapis.com/auth/webmasters.readonly"},
		TokenEndpoint: tokenSrv.URL,
		AuthEndpoint:  authSrv.URL,
	}, &sealedKeyring{held: map[string]string{}})
	b.client = tokenSrv.Client()
	b.BrowserOpener = func(authURL string) error {
		go followAuthURL(authURL)
		return nil
	}
	return b
}

func TestByoOAuthAuthEndpointDefaults(t *testing.T) {
	b := NewByoOAuth(ByoOAuthConfig{ClientID: "id"}, &sealedKeyring{})
	if got := b.authEndpoint(); got != defaultGumOAuthAuthURL {
		t.Fatalf("authEndpoint() = %q; want the packaged default %q", got, defaultGumOAuthAuthURL)
	}
}

// A nil BrowserOpener means "do not open a browser": Login still runs the
// loopback wait, so a cancelled context is what ends it.
func TestByoOAuthLoginNilOpenerWaitsForCallback(t *testing.T) {
	b := byoLoginFixture(t, func(w http.ResponseWriter, _ *http.Request) {})
	b.BrowserOpener = nil

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := b.Login(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Login = %v; want the cancelled context to end the loopback wait", err)
	}
}

func TestByoOAuthLoginBrowserOpenFailureWraps(t *testing.T) {
	b := byoLoginFixture(t, func(w http.ResponseWriter, _ *http.Request) {})
	b.BrowserOpener = func(string) error { return errors.New("no display") }

	_, err := b.Login(context.Background())
	if err == nil || !strings.Contains(err.Error(), "open browser") {
		t.Fatalf("Login = %v; want the browser-open failure named", err)
	}
}

func TestByoOAuthLoginExchangeFailureSurfaces(t *testing.T) {
	b := byoLoginFixture(t, func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "nope", http.StatusBadRequest)
	})
	_, err := b.Login(context.Background())
	var ae *AuthError
	if !errors.As(err, &ae) || ae.Code != "BYO_OAUTH_TOKEN_EXCHANGE_FAILED" {
		t.Fatalf("Login = %v; want BYO_OAUTH_TOKEN_EXCHANGE_FAILED", err)
	}
}

func TestByoOAuthLoginWithoutRefreshTokenIsAnError(t *testing.T) {
	b := byoLoginFixture(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"at-1","expires_in":3600,"token_type":"Bearer"}`))
	})
	_, err := b.Login(context.Background())
	var ae *AuthError
	if !errors.As(err, &ae) || ae.Code != "BYO_OAUTH_NO_REFRESH_TOKEN" {
		t.Fatalf("Login = %v; want BYO_OAUTH_NO_REFRESH_TOKEN", err)
	}
}

func TestByoOAuthLoginGrantStoreFailureSurfaces(t *testing.T) {
	b := byoLoginFixture(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"at-1","refresh_token":"rt-1","expires_in":3600,"token_type":"Bearer"}`))
	})
	_, err := b.Login(context.Background())
	if err == nil || !strings.Contains(err.Error(), "keyring sealed") {
		t.Fatalf("Login = %v; want the keyring write failure to abort the login", err)
	}
}

// exchangeAuthCode owns every token-endpoint failure mode. Driving it
// directly skips the loopback dance those arms do not depend on.
func exchangeFixture(t *testing.T, secret string, h http.HandlerFunc) (*ByoOAuth, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	b := NewByoOAuth(ByoOAuthConfig{
		ClientID:      "user-client-id",
		ClientSecret:  secret,
		Scopes:        []string{"https://www.googleapis.com/auth/webmasters.readonly"},
		TokenEndpoint: srv.URL,
	}, &sealedKeyring{})
	b.client = srv.Client()
	return b, srv
}

func TestExchangeAuthCodeSendsConfiguredClientSecret(t *testing.T) {
	var gotSecret string
	b, _ := exchangeFixture(t, "s3cret", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		gotSecret = r.FormValue("client_secret")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"at-1","refresh_token":"rt-1","expires_in":3600}`))
	})
	if _, err := b.exchangeAuthCode(context.Background(), "code", "verifier", "http://127.0.0.1/cb"); err != nil {
		t.Fatalf("exchangeAuthCode = %v", err)
	}
	if gotSecret != "s3cret" {
		t.Fatalf("client_secret = %q; want the configured secret forwarded", gotSecret)
	}
}

func TestExchangeAuthCodeRejectsUnbuildableRequest(t *testing.T) {
	b := NewByoOAuth(ByoOAuthConfig{ClientID: "id", TokenEndpoint: "http://\x7f/bad"}, &sealedKeyring{})
	_, err := b.exchangeAuthCode(context.Background(), "code", "verifier", "http://127.0.0.1/cb")
	if err == nil || !strings.Contains(err.Error(), "token request") {
		t.Fatalf("exchangeAuthCode = %v; want the request build failure named", err)
	}
}

func TestExchangeAuthCodeTransportFailureSurfaces(t *testing.T) {
	b, srv := exchangeFixture(t, "", func(w http.ResponseWriter, _ *http.Request) {})
	srv.Close() // the endpoint is gone before the POST

	_, err := b.exchangeAuthCode(context.Background(), "code", "verifier", "http://127.0.0.1/cb")
	var ae *AuthError
	if !errors.As(err, &ae) || !strings.Contains(ae.HumanRemediation, "token POST failed") {
		t.Fatalf("exchangeAuthCode = %v; want the transport failure named", err)
	}
}

func TestExchangeAuthCodeRejectsOversizedBody(t *testing.T) {
	b, _ := exchangeFixture(t, "", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(make([]byte, (1<<20)+1))
	})
	_, err := b.exchangeAuthCode(context.Background(), "code", "verifier", "http://127.0.0.1/cb")
	var ae *AuthError
	if !errors.As(err, &ae) || !strings.Contains(ae.HumanRemediation, "too large") {
		t.Fatalf("exchangeAuthCode = %v; want the 1 MiB cap enforced", err)
	}
}

func TestExchangeAuthCodeMapsKnownUpstreamError(t *testing.T) {
	b, _ := exchangeFixture(t, "", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
	})
	_, err := b.exchangeAuthCode(context.Background(), "code", "verifier", "http://127.0.0.1/cb")
	var ae *AuthError
	if !errors.As(err, &ae) {
		t.Fatalf("exchangeAuthCode = %v; want *AuthError", err)
	}
	if !strings.Contains(ae.HumanRemediation, "Refresh token revoked or expired") {
		t.Fatalf("remediation = %q; want the invalid_grant text, not the raw body", ae.HumanRemediation)
	}
}

func TestExchangeAuthCodeRejectsUndecodableBody(t *testing.T) {
	b, _ := exchangeFixture(t, "", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("not json"))
	})
	_, err := b.exchangeAuthCode(context.Background(), "code", "verifier", "http://127.0.0.1/cb")
	var ae *AuthError
	if !errors.As(err, &ae) || !strings.Contains(ae.HumanRemediation, "decode token response") {
		t.Fatalf("exchangeAuthCode = %v; want the decode failure named", err)
	}
}

func TestExchangeAuthCodeSurfacesInBodyError(t *testing.T) {
	b, _ := exchangeFixture(t, "", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"error":"unauthorized_client","error_description":"client is not authorized"}`))
	})
	_, err := b.exchangeAuthCode(context.Background(), "code", "verifier", "http://127.0.0.1/cb")
	var ae *AuthError
	if !errors.As(err, &ae) || !strings.Contains(ae.HumanRemediation, "unauthorized_client") {
		t.Fatalf("exchangeAuthCode = %v; want the 200-with-error body surfaced", err)
	}
}

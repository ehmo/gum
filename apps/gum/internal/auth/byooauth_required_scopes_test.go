// Spec §13 managed-scope re-consent, consent half. A re-consent asks for the
// exact scope set an operation needs. Google lets a user untick permissions on
// the consent screen, so the grant can come back narrower than what was asked
// for. Storing that narrow grant would replace a working one and still fail
// the operation, so Login refuses it and writes nothing.
//
// RequiredScopes is opt-in: the interactive CLI leaves it empty, because there
// the operator sees on screen what they granted.
package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/zalando/go-keyring"
)

const (
	scopeDrive    = "https://www.googleapis.com/auth/drive"
	scopeCalendar = "https://www.googleapis.com/auth/calendar"
)

// partialConsentServer returns grantedScope verbatim in the token response.
// An empty grantedScope omits the field, which RFC 6749 reads as "granted ==
// requested".
func partialConsentServer(t *testing.T, grantedScope string) *httptest.Server {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		scopeField := ""
		if grantedScope != "" {
			scopeField = fmt.Sprintf(`"scope":%q,`, grantedScope)
		}
		_, _ = fmt.Fprintf(w,
			`{"access_token":"at-1","refresh_token":"rt-new",%s"expires_in":3600,"token_type":"Bearer","id_token":%q}`,
			scopeField, idTokenWithClaims("subject-1", "user@example.com"))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// newRequiredScopesByo asks for drive and calendar, requiring required.
func newRequiredScopesByo(t *testing.T, tokenSrv *httptest.Server, required []string) *ByoOAuth {
	t.Helper()

	authSrv := newFakeAuthServer(t, "USE_GUM_STATE")
	t.Cleanup(authSrv.Close)
	b := NewByoOAuth(ByoOAuthConfig{
		ClientID:       "user-client-id",
		Scopes:         []string{scopeDrive, scopeCalendar},
		RequiredScopes: required,
		TokenEndpoint:  tokenSrv.URL,
		AuthEndpoint:   authSrv.URL,
	}, NewOSKeyring())
	b.client = tokenSrv.Client()
	b.BrowserOpener = func(authURL string) error { go followAuthURL(authURL); return nil }
	return b
}

func TestByoOAuthRequiredScopes(t *testing.T) {
	t.Run("a partial consent is refused and stores nothing", func(t *testing.T) {
		keyring.MockInit()
		b := newRequiredScopesByo(t, partialConsentServer(t, scopeDrive), []string{scopeDrive, scopeCalendar})
		// A working grant is already in place. The refusal must leave it alone.
		before := `{"refresh_token":"rt-old","scopes":["` + scopeDrive + `","` + scopeCalendar + `"],"subject":"subject-1"}`
		if err := b.kb.Set(b.keyringKey(), before); err != nil {
			t.Fatalf("seed keyring: %v", err)
		}

		_, err := b.Login(context.Background())
		if err == nil {
			t.Fatal("Login accepted a consent that granted only one of two required scopes")
		}
		var ae *AuthError
		if !errors.As(err, &ae) {
			t.Fatalf("err = %v (%T); want *AuthError", err, err)
		}
		if ae.Code != "BYO_OAUTH_SCOPE_NOT_GRANTED" {
			t.Errorf("error code = %q; want BYO_OAUTH_SCOPE_NOT_GRANTED", ae.Code)
		}
		if !strings.Contains(ae.HumanRemediation, scopeCalendar) {
			t.Errorf("remediation does not name the missing scope: %q", ae.HumanRemediation)
		}

		after, _ := b.kb.Get(b.keyringKey())
		if after != before {
			t.Errorf("the stored grant changed on a refused consent:\n before %s\n after  %s", before, after)
		}
	})

	t.Run("a complete consent is stored", func(t *testing.T) {
		keyring.MockInit()
		b := newRequiredScopesByo(t, partialConsentServer(t, scopeDrive+" "+scopeCalendar), []string{scopeDrive, scopeCalendar})

		if _, err := b.Login(context.Background()); err != nil {
			t.Fatalf("Login: %v", err)
		}
		stored, _ := b.kb.Get(b.keyringKey())
		if !strings.Contains(stored, "rt-new") {
			t.Errorf("stored grant = %s; want the new refresh token", stored)
		}
	})

	t.Run("an omitted scope field counts as the requested set", func(t *testing.T) {
		// RFC 6749 §5.1: no scope in the response means the grant matches the
		// request. Refusing here would break every server that omits it.
		keyring.MockInit()
		b := newRequiredScopesByo(t, partialConsentServer(t, ""), []string{scopeDrive, scopeCalendar})

		if _, err := b.Login(context.Background()); err != nil {
			t.Fatalf("Login: %v", err)
		}
	})

	t.Run("no required scopes keeps the interactive behaviour", func(t *testing.T) {
		keyring.MockInit()
		b := newRequiredScopesByo(t, partialConsentServer(t, scopeDrive), nil)

		if _, err := b.Login(context.Background()); err != nil {
			t.Fatalf("Login refused a narrow consent with no RequiredScopes set: %v", err)
		}
		stored, _ := b.kb.Get(b.keyringKey())
		if strings.Contains(stored, scopeCalendar) {
			t.Errorf("stored grant = %s; want only the scope the consent returned", stored)
		}
	})

	t.Run("a broader grant subsumes a required scope", func(t *testing.T) {
		// Login deliberately prunes gmail.metadata from the requested union,
		// because its presence blocks format=FULL reads. Without subsumption a
		// §13 re-consent for an op declaring gmail.metadata could never
		// succeed: the consent is asked for it and never returns it.
		keyring.MockInit()
		authSrv := newFakeAuthServer(t, "USE_GUM_STATE")
		t.Cleanup(authSrv.Close)
		tokenSrv := partialConsentServer(t, scopeGmailReadonly)
		b := NewByoOAuth(ByoOAuthConfig{
			ClientID:       "user-client-id",
			Scopes:         []string{scopeGmailReadonly},
			RequiredScopes: []string{scopeGmailMetadata},
			TokenEndpoint:  tokenSrv.URL,
			AuthEndpoint:   authSrv.URL,
		}, NewOSKeyring())
		b.client = tokenSrv.Client()
		b.BrowserOpener = func(authURL string) error { go followAuthURL(authURL); return nil }

		if _, err := b.Login(context.Background()); err != nil {
			t.Fatalf("Login: %v; gmail.readonly subsumes gmail.metadata", err)
		}
	})
}

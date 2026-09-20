package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// scriptedKeyring is a KeyringBackend whose Get, Set and Delete each run a
// caller-supplied rule before touching the in-memory store. The rules let a
// test fail one keyring operation on one key shape, which is what separates
// the vault error arms from each other.
type scriptedKeyring struct {
	store  map[string]string
	getRl  func(key string) error
	setRl  func(key string) error
	delRl  func(key string) error
	setSeq int
}

func newScriptedKeyring() *scriptedKeyring {
	return &scriptedKeyring{store: map[string]string{}}
}

func (s *scriptedKeyring) Get(key string) (string, error) {
	if s.getRl != nil {
		if err := s.getRl(key); err != nil {
			return "", err
		}
	}
	return s.store[key], nil
}

func (s *scriptedKeyring) Set(key, value string) error {
	s.setSeq++
	if s.setRl != nil {
		if err := s.setRl(key); err != nil {
			return err
		}
	}
	s.store[key] = value
	return nil
}

func (s *scriptedKeyring) Delete(key string) error {
	if s.delRl != nil {
		if err := s.delRl(key); err != nil {
			return err
		}
	}
	delete(s.store, key)
	return nil
}

func TestTrackGumOAuthKeyPropagatesIndexReadFailure(t *testing.T) {
	sentinel := errors.New("keychain locked")
	kb := newScriptedKeyring()
	kb.getRl = func(string) error { return sentinel }

	err := NewCredentialVault(kb).TrackGumOAuthKey("gum.gum_oauth.subj.hash")
	if !errors.Is(err, sentinel) {
		t.Fatalf("TrackGumOAuthKey = %v; want the index read failure propagated", err)
	}
}

func TestRevokeAllGumOAuthPropagatesIndexReadFailure(t *testing.T) {
	sentinel := errors.New("keychain locked")
	kb := newScriptedKeyring()
	kb.getRl = func(string) error { return sentinel }

	err := NewCredentialVault(kb).RevokeAllGumOAuth()
	if !errors.Is(err, sentinel) {
		t.Fatalf("RevokeAllGumOAuth = %v; want the index read failure propagated", err)
	}
}

// TestRevokeAllGumOAuthReportsFirstDeleteFailure pins the "keep deleting"
// contract: one unusable entry must not strand the others, and the caller
// still sees the first failure.
func TestRevokeAllGumOAuthReportsFirstDeleteFailure(t *testing.T) {
	sentinel := errors.New("entry locked")
	kb := newScriptedKeyring()
	kb.store[gumOAuthVaultIndexKey] = "gum.gum_oauth.a.1\ngum.gum_oauth.b.2"
	kb.store["gum.gum_oauth.a.1"] = "rt-a"
	kb.store["gum.gum_oauth.b.2"] = "rt-b"
	kb.delRl = func(key string) error {
		if key == "gum.gum_oauth.a.1" {
			return sentinel
		}
		return nil
	}

	err := NewCredentialVault(kb).RevokeAllGumOAuth()
	if !errors.Is(err, sentinel) {
		t.Fatalf("RevokeAllGumOAuth = %v; want the first delete failure", err)
	}
	if _, stillThere := kb.store["gum.gum_oauth.b.2"]; stillThere {
		t.Error("the second entry survived; a failed delete must not stop the sweep")
	}
	if _, stillThere := kb.store[gumOAuthVaultIndexKey]; stillThere {
		t.Error("the index survived; it must be deleted after the entries")
	}
}

func TestRevokeAllGumOAuthReportsIndexDeleteFailure(t *testing.T) {
	sentinel := errors.New("index locked")
	kb := newScriptedKeyring()
	kb.store[gumOAuthVaultIndexKey] = "gum.gum_oauth.a.1"
	kb.delRl = func(key string) error {
		if key == gumOAuthVaultIndexKey {
			return sentinel
		}
		return nil
	}

	err := NewCredentialVault(kb).RevokeAllGumOAuth()
	if !errors.Is(err, sentinel) {
		t.Fatalf("RevokeAllGumOAuth = %v; want the index delete failure", err)
	}
}

// --- Resolve arms that sit between the subject pointer and the refresh token.

const resolveScope = "https://example.test/scope/a"

func resolveFixture(t *testing.T, kb KeyringBackend) *GumOAuth {
	t.Helper()
	return &GumOAuth{
		Vault:            NewCredentialVault(kb),
		ManifestBody:     promotedManifest(resolveScope),
		ClientIDOverride: "test-client-id",
	}
}

func TestGumOAuthResolveRefreshTokenReadFailurePropagates(t *testing.T) {
	sentinel := errors.New("keychain locked")
	kb := newScriptedKeyring()
	kb.store[gumOAuthSubjectKey([]string{resolveScope})] = "fp-1"
	kb.getRl = func(key string) error {
		if strings.HasPrefix(key, "gum.gum_oauth._subject.") {
			return nil
		}
		return sentinel
	}

	_, err := resolveFixture(t, kb).Resolve(context.Background(), []string{resolveScope})
	if !errors.Is(err, sentinel) {
		t.Fatalf("Resolve = %v; want the refresh-token read failure propagated", err)
	}
	// A keychain failure must not masquerade as "you never logged in".
	var ae *AuthError
	if errors.As(err, &ae) && ae.Code == "AUTH_LOGIN_REQUIRED" {
		t.Error("code = AUTH_LOGIN_REQUIRED; a locked keychain needs unlocking, not a fresh login")
	}
}

// TestGumOAuthResolveSubjectWithoutTokenAsksForLogin covers the half-written
// vault: the subject pointer survived but its refresh token did not.
func TestGumOAuthResolveSubjectWithoutTokenAsksForLogin(t *testing.T) {
	kb := newScriptedKeyring()
	kb.store[gumOAuthSubjectKey([]string{resolveScope})] = "fp-1"

	_, err := resolveFixture(t, kb).Resolve(context.Background(), []string{resolveScope})
	var ae *AuthError
	if !errors.As(err, &ae) {
		t.Fatalf("Resolve = %v; want *AuthError", err)
	}
	if ae.Code != "AUTH_LOGIN_REQUIRED" || !ae.Retryable {
		t.Fatalf("code = %q retryable = %v; want AUTH_LOGIN_REQUIRED and retryable", ae.Code, ae.Retryable)
	}
	if !strings.Contains(ae.HumanRemediation, "refresh token") {
		t.Errorf("remediation = %q; want the missing refresh token named", ae.HumanRemediation)
	}
}

// --- Login arms past the token exchange.

// loginFixture wires Login against a token endpoint the caller controls and
// the shared fake authorization server.
func loginFixture(t *testing.T, kb KeyringBackend, token http.HandlerFunc) *GumOAuth {
	t.Helper()
	tokenSrv := httptest.NewServer(token)
	t.Cleanup(tokenSrv.Close)
	authSrv := newFakeAuthServer(t, "USE_GUM_STATE")
	t.Cleanup(authSrv.Close)
	return &GumOAuth{
		Vault:            NewCredentialVault(kb),
		AuthURL:          authSrv.URL,
		TokenURL:         tokenSrv.URL,
		HTTPClient:       tokenSrv.Client(),
		ManifestBody:     promotedManifest(resolveScope),
		ClientIDOverride: "test-client-id",
		BrowserOpener: func(authURL string) error {
			go followAuthURL(authURL)
			return nil
		},
	}
}

func grantWithIDToken(idToken string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"access_token":"at-1","refresh_token":"rt-1","expires_in":3600,"id_token":%q}`, idToken)
	}
}

func TestGumOAuthLoginRejectsGrantWithoutIDToken(t *testing.T) {
	_, err := loginFixture(t, newScriptedKeyring(), grantWithIDToken("")).
		Login(context.Background(), []string{resolveScope})
	var ae *AuthError
	if !errors.As(err, &ae) || ae.Code != "GUM_OAUTH_ID_TOKEN_MISSING" {
		t.Fatalf("Login = %v; want GUM_OAUTH_ID_TOKEN_MISSING", err)
	}
}

func TestGumOAuthLoginSubjectPointerWriteFailurePropagates(t *testing.T) {
	sentinel := errors.New("subject write refused")
	kb := newScriptedKeyring()
	kb.setRl = func(key string) error {
		if strings.HasPrefix(key, "gum.gum_oauth._subject.") {
			return sentinel
		}
		return nil
	}

	_, err := loginFixture(t, kb, grantWithIDToken(fakeIDToken("subject-1"))).
		Login(context.Background(), []string{resolveScope})
	if !errors.Is(err, sentinel) {
		t.Fatalf("Login = %v; want the subject pointer write failure propagated", err)
	}
}

// indexWriteFailAfter fails the Nth (0-based) write to the gum_oauth index.
// Login tracks two keys, so N selects which TrackGumOAuthKey call breaks.
func indexWriteFailAfter(kb *scriptedKeyring, n int, sentinel error) {
	seen := 0
	kb.setRl = func(key string) error {
		if key != gumOAuthVaultIndexKey {
			return nil
		}
		defer func() { seen++ }()
		if seen == n {
			return sentinel
		}
		return nil
	}
}

func TestGumOAuthLoginTokenKeyTrackingFailurePropagates(t *testing.T) {
	sentinel := errors.New("index write refused")
	kb := newScriptedKeyring()
	indexWriteFailAfter(kb, 0, sentinel)

	_, err := loginFixture(t, kb, grantWithIDToken(fakeIDToken("subject-1"))).
		Login(context.Background(), []string{resolveScope})
	if !errors.Is(err, sentinel) {
		t.Fatalf("Login = %v; want the refresh-token tracking failure propagated", err)
	}
}

// TestGumOAuthLoginSubjectKeyTrackingFailurePropagates pins the second
// TrackGumOAuthKey call. An untracked subject pointer would survive `gum auth
// logout`, so this arm must not be swallowed either.
func TestGumOAuthLoginSubjectKeyTrackingFailurePropagates(t *testing.T) {
	sentinel := errors.New("index write refused")
	kb := newScriptedKeyring()
	indexWriteFailAfter(kb, 1, sentinel)

	_, err := loginFixture(t, kb, grantWithIDToken(fakeIDToken("subject-1"))).
		Login(context.Background(), []string{resolveScope})
	if !errors.Is(err, sentinel) {
		t.Fatalf("Login = %v; want the subject key tracking failure propagated", err)
	}
	if !strings.Contains(kb.store[gumOAuthVaultIndexKey], "gum.gum_oauth.") {
		t.Errorf("index = %q; want the refresh-token key tracked before the failure", kb.store[gumOAuthVaultIndexKey])
	}
}

// --- postToken body cap and id_token parsing.

func TestGumOAuthPostTokenRejectsOversizedBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(make([]byte, (1<<20)+1))
	}))
	t.Cleanup(srv.Close)

	g := &GumOAuth{TokenURL: srv.URL, HTTPClient: srv.Client()}
	_, err := g.postToken(context.Background(), nil)
	if err == nil || !strings.Contains(err.Error(), "read token response") {
		t.Fatalf("postToken = %v; want the 1 MiB cap enforced", err)
	}
}

func TestIDTokenSubjectRejectsMalformedTokens(t *testing.T) {
	cases := []struct {
		name    string
		idToken string
		code    string
		detail  string
	}{
		{"empty", "   ", "GUM_OAUTH_ID_TOKEN_MISSING", "did not return an id_token"},
		{"two parts", "header.payload", "GUM_OAUTH_ID_TOKEN_INVALID", "three dot-separated parts"},
		{"payload not base64", "header.!!!.sig", "GUM_OAUTH_ID_TOKEN_INVALID", "decode payload"},
		{"payload not json", "header.bm90IGpzb24.sig", "GUM_OAUTH_ID_TOKEN_INVALID", "decode claims"},
		{"no sub claim", "header.eyJzdWIiOiIgIn0.sig", "GUM_OAUTH_ID_TOKEN_INVALID", "missing sub claim"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := managedSubjectFingerprintFromIDToken(tc.idToken)
			var ae *AuthError
			if !errors.As(err, &ae) {
				t.Fatalf("managedSubjectFingerprintFromIDToken(%q) = %v; want *AuthError", tc.idToken, err)
			}
			if ae.Code != tc.code {
				t.Errorf("code = %q; want %q", ae.Code, tc.code)
			}
			if !strings.Contains(ae.HumanRemediation, tc.detail) {
				t.Errorf("remediation = %q; want it to name %q", ae.HumanRemediation, tc.detail)
			}
		})
	}
}

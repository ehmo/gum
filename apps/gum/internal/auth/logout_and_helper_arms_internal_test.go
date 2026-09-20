package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// logoutFixture registers a BYO client and a stored grant in kb so Logout
// reaches every step. The returned key is the grant entry's keyring key.
func logoutFixture(t *testing.T, kb KeyringBackend, profile string) string {
	t.Helper()
	client := ByoClient{ClientID: "client-1.apps.googleusercontent.com"}
	if err := StoreByoClient(kb, profile, client); err != nil {
		t.Fatalf("StoreByoClient: %v", err)
	}
	grantKey := NewByoOAuth(ByoOAuthConfig{ClientID: client.ClientID, Profile: profile}, kb).keyringKey()
	if err := kb.Set(grantKey, `{"refresh_token":"rt-1","scopes":["s"]}`); err != nil {
		t.Fatalf("seed grant: %v", err)
	}
	return grantKey
}

// isGrantKey matches the grant entry without matching the client entry:
// "gum.byo_oauth_client." shares the first 13 characters with "gum.byo_oauth.".
func isGrantKey(key string) bool { return strings.HasPrefix(key, "gum.byo_oauth.") }

func TestLogoutPropagatesClientReadFailure(t *testing.T) {
	sentinel := errors.New("keychain locked")
	kb := newScriptedKeyring()
	kb.getRl = func(string) error { return sentinel }

	_, err := Logout(context.Background(), kb, "", false)
	if !errors.Is(err, sentinel) {
		t.Fatalf("Logout = %v; want the client read failure propagated", err)
	}
}

func TestLogoutPropagatesGrantReadFailure(t *testing.T) {
	sentinel := errors.New("grant entry unreadable")
	kb := newScriptedKeyring()
	logoutFixture(t, kb, "")
	kb.getRl = func(key string) error {
		if isGrantKey(key) {
			return sentinel
		}
		return nil
	}

	_, err := Logout(context.Background(), kb, "", false)
	if !errors.Is(err, sentinel) {
		t.Fatalf("Logout = %v; want the grant read failure propagated", err)
	}
}

func TestLogoutPropagatesGrantDeleteFailure(t *testing.T) {
	sentinel := errors.New("grant delete refused")
	kb := newScriptedKeyring()
	logoutFixture(t, kb, "")
	kb.delRl = func(key string) error {
		if isGrantKey(key) {
			return sentinel
		}
		return nil
	}

	_, err := Logout(context.Background(), kb, "", false)
	if !errors.Is(err, sentinel) {
		t.Fatalf("Logout = %v; want the revoke failure propagated", err)
	}
}

func TestLogoutPropagatesVaultIndexReadFailure(t *testing.T) {
	sentinel := errors.New("index unreadable")
	kb := newScriptedKeyring()
	logoutFixture(t, kb, "")
	kb.getRl = func(key string) error {
		if key == gumOAuthVaultIndexKey {
			return sentinel
		}
		return nil
	}

	_, err := Logout(context.Background(), kb, "", false)
	if !errors.Is(err, sentinel) {
		t.Fatalf("Logout = %v; want the vault index read failure propagated", err)
	}
}

// TestLogoutPropagatesVaultPurgeFailure pins the arm that matters most: a
// gum_oauth refresh token that survives an explicit logout must be reported,
// not swallowed, or the operator believes a live credential is gone.
func TestLogoutPropagatesVaultPurgeFailure(t *testing.T) {
	sentinel := errors.New("vault entry locked")
	kb := newScriptedKeyring()
	logoutFixture(t, kb, "")
	kb.store[gumOAuthVaultIndexKey] = "gum.gum_oauth.subj.hash"
	kb.delRl = func(key string) error {
		if strings.HasPrefix(key, "gum.gum_oauth.") {
			return sentinel
		}
		return nil
	}

	_, err := Logout(context.Background(), kb, "", false)
	if !errors.Is(err, sentinel) {
		t.Fatalf("Logout = %v; want the vault purge failure propagated", err)
	}
}

func TestLogoutPropagatesClientDeleteFailure(t *testing.T) {
	sentinel := errors.New("client delete refused")
	kb := newScriptedKeyring()
	logoutFixture(t, kb, "")
	kb.delRl = func(key string) error {
		if strings.HasPrefix(key, "gum.byo_oauth_client.") {
			return sentinel
		}
		return nil
	}

	_, err := Logout(context.Background(), kb, "", true)
	if !errors.Is(err, sentinel) {
		t.Fatalf("Logout(forgetClient) = %v; want the client delete failure propagated", err)
	}
}

// --- BYO client storage helpers.

func TestByoClientKeyIsProfileScoped(t *testing.T) {
	if got, want := byoClientKeyringKey("  "), byoClientKeyringKey(DefaultAPIKeyProfile); got != want {
		t.Errorf("byoClientKeyringKey(blank) = %q; want the default profile key %q", got, want)
	}
	if byoClientKeyringKey("staging") == byoClientKeyringKey("prod") {
		t.Error("two profiles share one client key; a staging client would serve prod")
	}
}

func TestLoadByoClientWithoutBackendReportsNoClient(t *testing.T) {
	client, ok, err := LoadByoClient(nil, "default")
	if err != nil || ok || client.ClientID != "" {
		t.Fatalf("LoadByoClient(nil kb) = (%+v, %v, %v); want a clean not-configured answer", client, ok, err)
	}
}

// --- ID-token subject used for cache partitioning (never for authorization).

func TestOAuthSubjectFromIDTokenRejectsUnreadableTokens(t *testing.T) {
	cases := map[string]string{
		"two parts":        "header.payload",
		"payload not b64":  "header.!!!.sig",
		"payload not json": "header.bm90IGpzb24.sig",
	}
	for name, idToken := range cases {
		t.Run(name, func(t *testing.T) {
			if got := oauthSubjectFromIDToken(idToken); got != "" {
				t.Errorf("oauthSubjectFromIDToken(%q) = %q; want the empty partition name", idToken, got)
			}
		})
	}
}

// --- Managed-scopes manifest scope filter.

func TestManagedSupportedScopesPropagatesManifestFailure(t *testing.T) {
	_, err := managedSupportedScopes([]byte("{"))
	if err == nil || !strings.Contains(err.Error(), "manifest decode") {
		t.Fatalf("managedSupportedScopes(bad body) = %v; want the decode failure named", err)
	}
}

func TestManagedSupportedScopesDeduplicates(t *testing.T) {
	body := []byte(`{"schema_version":1,"scopes":[
		{"scope":"https://example.test/a","status":"active"},
		{"scope":"https://example.test/a","testing_allowed":true},
		{"scope":"https://example.test/b","status":"pending"}
	]}`)
	got, err := managedSupportedScopes(body)
	if err != nil {
		t.Fatalf("managedSupportedScopes: %v", err)
	}
	if len(got) != 1 || got[0] != "https://example.test/a" {
		t.Fatalf("scopes = %v; want the single eligible scope listed once", got)
	}
}

// --- byo_oauth refresh path.

// TestByoOAuthRefreshHealsMissingSubject covers the gum-mc67 heal: a grant
// stored before subjects were recorded gets one back from the refresh
// response's id_token, and the repaired grant is written to the keyring.
func TestByoOAuthRefreshHealsMissingSubject(t *testing.T) {
	idToken := fakeIDToken("subject-7")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"access_token":"at-1","expires_in":3600,"id_token":%q}`, idToken)
	}))
	t.Cleanup(srv.Close)

	kb := newScriptedKeyring()
	b := NewByoOAuth(ByoOAuthConfig{
		ClientID:      "client-1",
		Scopes:        []string{"https://example.test/a"},
		TokenEndpoint: srv.URL,
	}, kb)
	b.client = srv.Client()
	if err := kb.Set(b.keyringKey(), `{"refresh_token":"rt-1","scopes":["https://example.test/a"]}`); err != nil {
		t.Fatalf("seed grant: %v", err)
	}

	creds, err := b.Acquire(context.Background())
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if creds.SubjectFingerprint == "" {
		t.Error("SubjectFingerprint is empty; the refresh must adopt the id_token subject")
	}

	var healed struct {
		Subject string `json:"subject"`
	}
	if err := json.Unmarshal([]byte(kb.store[b.keyringKey()]), &healed); err != nil {
		t.Fatalf("decode healed grant: %v", err)
	}
	if healed.Subject == "" {
		t.Error("the stored grant still has no subject; the heal must be persisted")
	}
}

// TestRevokeRemoteIgnoresUnbuildableEndpoint pins the swallow-everything
// contract: the local delete is what guarantees the credential is gone, so a
// malformed revoke endpoint must not fail Revoke.
func TestRevokeRemoteIgnoresUnbuildableEndpoint(t *testing.T) {
	kb := newScriptedKeyring()
	b := NewByoOAuth(ByoOAuthConfig{ClientID: "client-1", RevokeEndpoint: "http://\x7f/revoke"}, kb)
	if err := kb.Set(b.keyringKey(), `{"refresh_token":"rt-1"}`); err != nil {
		t.Fatalf("seed grant: %v", err)
	}

	if err := b.Revoke(context.Background()); err != nil {
		t.Fatalf("Revoke = %v; want the local delete to succeed regardless", err)
	}
	if _, stillThere := kb.store[b.keyringKey()]; stillThere {
		t.Error("the grant survived Revoke")
	}
}

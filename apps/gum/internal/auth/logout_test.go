package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

const logoutTestScope = "https://www.googleapis.com/auth/webmasters.readonly"

// stubRevoke points DefaultRevokeEndpoint at a local server (so Logout's
// best-effort server-side revoke stays hermetic) and returns a counter of how
// many revocation POSTs it received.
func stubRevoke(t *testing.T) *int32 {
	t.Helper()
	var n int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&n, 1)
		w.WriteHeader(http.StatusOK)
	}))
	prev := DefaultRevokeEndpoint
	DefaultRevokeEndpoint = srv.URL
	t.Cleanup(func() {
		DefaultRevokeEndpoint = prev
		srv.Close()
		http.DefaultClient.CloseIdleConnections()
	})
	return &n
}

// seedGrant registers a BYO client and stores a refresh-token grant for it,
// returning the ByoOAuth handle so tests can inspect the grant afterwards.
func seedGrant(t *testing.T, kb KeyringBackend, profile, clientID string) *ByoOAuth {
	t.Helper()
	if err := StoreByoClient(kb, profile, ByoClient{ClientID: clientID}); err != nil {
		t.Fatalf("StoreByoClient: %v", err)
	}
	b := NewByoOAuth(ByoOAuthConfig{ClientID: clientID, Scopes: []string{logoutTestScope}}, kb)
	if err := b.StoreRefreshToken("rt-1"); err != nil {
		t.Fatalf("StoreRefreshToken: %v", err)
	}
	return b
}

// TestLogoutClearsGrant pins the default logout: it revokes the stored
// refresh-token grant for the active client but, without forgetClient, leaves
// the registered BYO client in place so the next `gum login` can reuse it.
func TestLogoutClearsGrant(t *testing.T) {
	stubRevoke(t)
	kb := &mockKeyring{data: map[string]string{}}
	b := seedGrant(t, kb, "default", "cid")

	res, err := Logout(context.Background(), kb, "default", false)
	if err != nil {
		t.Fatalf("Logout: %v", err)
	}
	if !res.GrantCleared {
		t.Error("GrantCleared = false, want true")
	}
	if res.ClientForgotten {
		t.Error("ClientForgotten = true; forgetClient was false")
	}
	if res.UsingManaged {
		t.Error("UsingManaged = true; a registered BYO client was present")
	}
	if _, ok, _ := b.loadGrant(); ok {
		t.Error("grant still present after logout")
	}
	if _, ok, _ := LoadByoClient(kb, "default"); !ok {
		t.Error("registered client removed despite forgetClient=false")
	}
}

// TestLogoutForgetClientRemovesClient pins that --forget-client (forgetClient=
// true) removes the registered BYO client entry in addition to the grant, so a
// later `gum login` falls back to the managed client instead of the old one.
func TestLogoutForgetClientRemovesClient(t *testing.T) {
	stubRevoke(t)
	kb := &mockKeyring{data: map[string]string{}}
	b := seedGrant(t, kb, "default", "cid")

	res, err := Logout(context.Background(), kb, "default", true)
	if err != nil {
		t.Fatalf("Logout: %v", err)
	}
	if !res.GrantCleared {
		t.Error("GrantCleared = false, want true")
	}
	if !res.ClientForgotten {
		t.Error("ClientForgotten = false, want true")
	}
	if _, ok, _ := b.loadGrant(); ok {
		t.Error("grant still present after logout")
	}
	if _, ok, _ := LoadByoClient(kb, "default"); ok {
		t.Error("registered client still present after --forget-client")
	}
}

// TestLogoutNoCredentialsIsNoop pins that logging out with nothing configured
// is a clean no-op rather than an error, so `gum logout` is safe to run when
// already logged out.
func TestLogoutNoCredentialsIsNoop(t *testing.T) {
	kb := &mockKeyring{data: map[string]string{}}

	res, err := Logout(context.Background(), kb, "default", false)
	if err != nil {
		t.Fatalf("Logout: %v", err)
	}
	if res.GrantCleared || res.ClientForgotten {
		t.Errorf("expected no-op, got %+v", res)
	}
}

// TestLogoutClearsCorruptGrant pins that a present-but-unparseable grant entry
// still counts as cleared: loadGrant treats corrupt JSON as absent, but the
// entry is real stored state that Revoke removes, so GrantCleared must be true
// (reporting it from the raw keyring value, not the parsed grant).
func TestLogoutClearsCorruptGrant(t *testing.T) {
	kb := &mockKeyring{data: map[string]string{}}
	if err := StoreByoClient(kb, "default", ByoClient{ClientID: "cid"}); err != nil {
		t.Fatalf("StoreByoClient: %v", err)
	}
	b := NewByoOAuth(ByoOAuthConfig{ClientID: "cid"}, kb)
	if err := kb.Set(b.keyringKey(), "{not valid json"); err != nil {
		t.Fatalf("seed corrupt grant: %v", err)
	}

	res, err := Logout(context.Background(), kb, "default", false)
	if err != nil {
		t.Fatalf("Logout: %v", err)
	}
	if !res.GrantCleared {
		t.Error("GrantCleared = false; a present (corrupt) grant entry was removed, want true")
	}
	if v, _ := kb.Get(b.keyringKey()); v != "" {
		t.Errorf("corrupt grant entry not removed: %q", v)
	}
}

// TestLogoutForgetClientSkippedWhenNoClient pins that requesting --forget-client
// with no registered BYO client records ForgetClientSkipped (so the CLI can
// explain the no-op) rather than silently dropping the flag.
func TestLogoutForgetClientSkippedWhenNoClient(t *testing.T) {
	kb := &mockKeyring{data: map[string]string{}}

	res, err := Logout(context.Background(), kb, "default", true)
	if err != nil {
		t.Fatalf("Logout: %v", err)
	}
	if !res.ForgetClientSkipped {
		t.Error("ForgetClientSkipped = false; --forget-client with no registered client, want true")
	}
	if res.ClientForgotten {
		t.Error("ClientForgotten = true; nothing was registered to forget")
	}
}

// TestLogoutRevokesAtGoogle pins the gum-1082 security behavior: Logout makes a
// best-effort POST to the revocation endpoint with the refresh token, in
// addition to clearing local state.
func TestLogoutRevokesAtGoogle(t *testing.T) {
	var gotToken string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		gotToken = r.FormValue("token")
		w.WriteHeader(http.StatusOK)
	}))
	prev := DefaultRevokeEndpoint
	DefaultRevokeEndpoint = srv.URL
	t.Cleanup(func() { DefaultRevokeEndpoint = prev; srv.Close(); http.DefaultClient.CloseIdleConnections() })

	kb := &mockKeyring{data: map[string]string{}}
	seedGrant(t, kb, "default", "cid") // stores refresh token "rt-1"
	if _, err := Logout(context.Background(), kb, "default", false); err != nil {
		t.Fatalf("Logout: %v", err)
	}
	if gotToken != "rt-1" {
		t.Errorf("revoke endpoint got token %q, want rt-1", gotToken)
	}
}

// TestLogoutClearsGumOAuthVault pins gum-h24d: Logout purges all gum_oauth
// CredentialVault entries tracked by the index key, even though gum_oauth is
// manifest-gated off in v0.1.0.
func TestLogoutClearsGumOAuthVault(t *testing.T) {
	stubRevoke(t)
	kb := &mockKeyring{data: map[string]string{}}
	seedGrant(t, kb, "default", "cid")

	scopes1 := []string{"https://www.googleapis.com/auth/gmail.readonly"}
	scopes2 := []string{"https://www.googleapis.com/auth/drive.readonly"}
	vault := NewCredentialVault(kb)
	fp1 := managedSubjectFingerprintFromSub("subject-1")
	fp2 := managedSubjectFingerprintFromSub("subject-2")
	key1 := vaultKey(gumOAuthStrategyName, fp1, scopes1)
	key2 := vaultKey(gumOAuthStrategyName, fp2, scopes2)
	subjectKey1 := gumOAuthSubjectKey(scopes1)
	subjectKey2 := gumOAuthSubjectKey(scopes2)
	if err := vault.StoreRefreshToken(gumOAuthStrategyName, fp1, scopes1, "rt-gum-1"); err != nil {
		t.Fatalf("seed vault entry 1: %v", err)
	}
	if err := vault.StoreGumOAuthSubject(scopes1, fp1); err != nil {
		t.Fatalf("seed subject 1: %v", err)
	}
	if err := vault.TrackGumOAuthKey(key1); err != nil {
		t.Fatalf("track key1: %v", err)
	}
	if err := vault.TrackGumOAuthKey(subjectKey1); err != nil {
		t.Fatalf("track subject key1: %v", err)
	}
	if err := vault.StoreRefreshToken(gumOAuthStrategyName, fp2, scopes2, "rt-gum-2"); err != nil {
		t.Fatalf("seed vault entry 2: %v", err)
	}
	if err := vault.StoreGumOAuthSubject(scopes2, fp2); err != nil {
		t.Fatalf("seed subject 2: %v", err)
	}
	if err := vault.TrackGumOAuthKey(key2); err != nil {
		t.Fatalf("track key2: %v", err)
	}
	if err := vault.TrackGumOAuthKey(subjectKey2); err != nil {
		t.Fatalf("track subject key2: %v", err)
	}

	res, err := Logout(context.Background(), kb, "default", false)
	if err != nil {
		t.Fatalf("Logout: %v", err)
	}
	if !res.GumOAuthVaultCleared {
		t.Error("GumOAuthVaultCleared = false; expected vault to be cleared")
	}
	if v, _ := kb.Get(key1); v != "" {
		t.Errorf("vault key1 still present after logout: %q", v)
	}
	if v, _ := kb.Get(key2); v != "" {
		t.Errorf("vault key2 still present after logout: %q", v)
	}
	if v, _ := kb.Get(subjectKey1); v != "" {
		t.Errorf("subject key1 still present after logout: %q", v)
	}
	if v, _ := kb.Get(subjectKey2); v != "" {
		t.Errorf("subject key2 still present after logout: %q", v)
	}
	if v, _ := kb.Get(gumOAuthVaultIndexKey); v != "" {
		t.Errorf("gum_oauth index still present after logout: %q", v)
	}
}

// TestGumOAuthRevokeIsNoopWhenVaultNil pins the nil-vault guard.
func TestGumOAuthRevokeIsNoopWhenVaultNil(t *testing.T) {
	g := &GumOAuth{Vault: nil}
	if err := g.Revoke(); err != nil {
		t.Errorf("Revoke with nil vault: want nil, got %v", err)
	}
}

// TestGumOAuthTrackAndRevokeRoundTrip pins the CredentialVault index round-trip:
// store + track, then RevokeAllGumOAuth removes both the entry and the index.
func TestGumOAuthTrackAndRevokeRoundTrip(t *testing.T) {
	kb := &mockKeyring{data: map[string]string{}}
	v := NewCredentialVault(kb)
	scopes := []string{"https://www.googleapis.com/auth/gmail.readonly"}
	fp := managedSubjectFingerprintFromSub("subject-x")
	key := vaultKey(gumOAuthStrategyName, fp, scopes)
	if err := v.StoreRefreshToken(gumOAuthStrategyName, fp, scopes, "rt-x"); err != nil {
		t.Fatalf("Store: %v", err)
	}
	if err := v.TrackGumOAuthKey(key); err != nil {
		t.Fatalf("Track: %v", err)
	}
	if idx, _ := kb.Get(gumOAuthVaultIndexKey); idx == "" {
		t.Fatal("index is empty after TrackGumOAuthKey")
	}
	if err := v.RevokeAllGumOAuth(); err != nil {
		t.Fatalf("RevokeAllGumOAuth: %v", err)
	}
	if v2, _ := kb.Get(key); v2 != "" {
		t.Errorf("vault key still present after RevokeAllGumOAuth: %q", v2)
	}
	if v2, _ := kb.Get(gumOAuthVaultIndexKey); v2 != "" {
		t.Errorf("index still present after RevokeAllGumOAuth: %q", v2)
	}
}

// TestTrackGumOAuthKeyDeduplicates pins that tracking the same key twice does
// not grow the index.
func TestTrackGumOAuthKeyDeduplicates(t *testing.T) {
	kb := &mockKeyring{data: map[string]string{}}
	v := NewCredentialVault(kb)
	if err := v.TrackGumOAuthKey("gum.gum_oauth.a.b"); err != nil {
		t.Fatalf("track 1: %v", err)
	}
	if err := v.TrackGumOAuthKey("gum.gum_oauth.a.b"); err != nil {
		t.Fatalf("track 2: %v", err)
	}
	idx, _ := kb.Get(gumOAuthVaultIndexKey)
	if idx != "gum.gum_oauth.a.b" {
		t.Errorf("index = %q; want single entry (no duplicate)", idx)
	}
}

// TestLogoutRevokeBestEffort pins that a failing/unreachable revoke endpoint
// does NOT block the local clear (logout still succeeds and the grant is gone).
func TestLogoutRevokeBestEffort(t *testing.T) {
	prev := DefaultRevokeEndpoint
	DefaultRevokeEndpoint = "http://127.0.0.1:1/revoke" // connection refused
	t.Cleanup(func() { DefaultRevokeEndpoint = prev; http.DefaultClient.CloseIdleConnections() })

	kb := &mockKeyring{data: map[string]string{}}
	b := seedGrant(t, kb, "default", "cid")
	res, err := Logout(context.Background(), kb, "default", false)
	if err != nil {
		t.Fatalf("Logout should succeed despite revoke failure: %v", err)
	}
	if !res.GrantCleared {
		t.Error("grant should be cleared locally despite revoke failure")
	}
	if _, ok, _ := b.loadGrant(); ok {
		t.Error("grant still present after logout")
	}
}

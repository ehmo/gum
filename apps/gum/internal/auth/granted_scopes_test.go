package auth

import "testing"

// TestGrantedScopesReturnsRecordedGrant pins the core of gum-n9yl: after a
// login records a grant, GrantedScopes must return exactly the scopes the
// operator authorized for the profile's registered client. This is the value
// the dispatcher feeds into ProfilePolicy.AllowedScopes; without it every
// scoped op is rejected with SCOPE_MISSING.
func TestGrantedScopesReturnsRecordedGrant(t *testing.T) {
	kb := &mockKeyring{data: map[string]string{}}
	const profile = "default"
	if err := StoreByoClient(kb, profile, ByoClient{ClientID: "client-xyz"}); err != nil {
		t.Fatalf("StoreByoClient: %v", err)
	}
	// Record a grant exactly as Login does: a refresh token plus the granted scope.
	b := NewByoOAuth(ByoOAuthConfig{
		ClientID: "client-xyz",
		Scopes:   []string{"https://www.googleapis.com/auth/webmasters.readonly"},
	}, kb)
	if err := b.StoreRefreshToken("rt-123"); err != nil {
		t.Fatalf("StoreRefreshToken: %v", err)
	}

	got := GrantedScopes(kb, profile)
	if len(got) != 1 || got[0] != "https://www.googleapis.com/auth/webmasters.readonly" {
		t.Fatalf("GrantedScopes=%v; want [webmasters.readonly]", got)
	}
}

// TestGrantedScopesNilWhenNoGrant: with no registered client and no managed
// fallback (empty embedded id/secret in test builds), GrantedScopes returns nil
// so the scope gate stays closed rather than silently allowing everything.
func TestGrantedScopesNilWhenNoGrant(t *testing.T) {
	kb := &mockKeyring{data: map[string]string{}}
	if got := GrantedScopes(kb, "default"); got != nil {
		t.Fatalf("GrantedScopes=%v; want nil", got)
	}
}

// TestGrantedScopesNilWhenClientButNoGrant: a registered client with no
// recorded grant yet (configured but never logged in) yields nil, not a panic.
func TestGrantedScopesNilWhenClientButNoGrant(t *testing.T) {
	kb := &mockKeyring{data: map[string]string{}}
	if err := StoreByoClient(kb, "default", ByoClient{ClientID: "client-xyz"}); err != nil {
		t.Fatalf("StoreByoClient: %v", err)
	}
	if got := GrantedScopes(kb, "default"); got != nil {
		t.Fatalf("GrantedScopes=%v; want nil", got)
	}
}

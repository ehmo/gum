package auth

// GrantedScopes returns the OAuth scopes the operator has authorized for the
// profile's active byo_oauth client. It resolves the client the same way
// `gum login` does: the operator must register a BYO client first. It then
// reads the grant recorded at login (byoGrant.Scopes).
//
// This is the source of truth for the dispatcher's per-profile scope allowlist
// (dispatch.ProfilePolicy.AllowedScopes). Without it the policy scope gate sees
// an empty allowlist and rejects every scoped op with SCOPE_MISSING even
// immediately after a successful login that recorded the grant (gum-n9yl).
//
// Returns nil when no client is configured or no grant has been recorded yet,
// so the gate stays closed rather than silently allowing every scope.
func GrantedScopes(kb KeyringBackend, profile string) []string {
	client, ok, _ := LoadByoClient(kb, profile)
	if !ok {
		return nil
	}
	grant, ok, _ := NewByoOAuth(ByoOAuthConfig{ClientID: client.ClientID, Profile: profile}, kb).loadGrant()
	if !ok {
		return nil
	}
	return grant.Scopes
}

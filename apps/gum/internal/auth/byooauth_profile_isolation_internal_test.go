package auth

import "testing"

// isoKeyring is a tiny in-memory KeyringBackend for the profile-isolation test.
type isoKeyring map[string]string

func (k isoKeyring) Get(key string) (string, error) { return k[key], nil }
func (k isoKeyring) Set(key, val string) error      { k[key] = val; return nil }
func (k isoKeyring) Delete(key string) error        { delete(k, key); return nil }

// TestByoOAuthKeyProfileIsolation pins gum-2fu0: the same OAuth client under
// different profiles must derive distinct keyring keys, while "default" and ""
// stay on the legacy unprefixed key (backward compat / no forced re-login).
func TestByoOAuthKeyProfileIsolation(t *testing.T) {
	const clientID = "client-123.apps.googleusercontent.com"
	keyFor := func(profile string) string {
		return NewByoOAuth(ByoOAuthConfig{ClientID: clientID, Profile: profile}, nil).keyringKey()
	}

	legacy := keyFor("default")
	if keyFor("") != legacy {
		t.Errorf("empty profile must map to the legacy default key (%s vs %s)", keyFor(""), legacy)
	}
	prod, staging := keyFor("prod"), keyFor("staging")
	if prod == legacy {
		t.Errorf("prod must not collide with default: %s", prod)
	}
	if prod == staging {
		t.Errorf("distinct profiles must not collide: %s", prod)
	}
	if keyFor("prod") != prod {
		t.Errorf("key must be deterministic for (profile, client)")
	}
	if other := (NewByoOAuth(ByoOAuthConfig{ClientID: "other.apps.googleusercontent.com", Profile: "prod"}, nil)).keyringKey(); other == prod {
		t.Errorf("different clients must not collide under the same profile")
	}
}

// TestByoOAuthGrantNotSharedAcrossProfiles is the end-to-end proof: a grant
// stored under "prod" is invisible to a "staging" resolver using the same
// client, so the second login can never serve the first profile's token.
func TestByoOAuthGrantNotSharedAcrossProfiles(t *testing.T) {
	const clientID = "shared-client.apps.googleusercontent.com"
	kb := isoKeyring{}

	prod := NewByoOAuth(ByoOAuthConfig{ClientID: clientID, Profile: "prod", Scopes: []string{"https://www.googleapis.com/auth/adwords"}}, kb)
	if err := prod.StoreRefreshToken("REFRESH-PROD"); err != nil {
		t.Fatalf("store prod grant: %v", err)
	}

	// A staging resolver with the SAME client must NOT see prod's grant.
	staging := NewByoOAuth(ByoOAuthConfig{ClientID: clientID, Profile: "staging"}, kb)
	if _, ok, _ := staging.loadGrant(); ok {
		t.Error("staging profile must not see prod's grant (cross-account substitution)")
	}

	// prod still reads its own grant.
	g, ok, _ := prod.loadGrant()
	if !ok || g.RefreshToken != "REFRESH-PROD" {
		t.Errorf("prod must read its own grant; got ok=%v token=%q", ok, g.RefreshToken)
	}
}

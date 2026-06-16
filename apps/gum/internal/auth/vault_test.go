package auth_test

import (
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/auth"
)

// memKB is a minimal in-memory KeyringBackend for vault tests.
type memKB struct{ m map[string]string }

func (k *memKB) Get(key string) (string, error)       { return k.m[key], nil }
func (k *memKB) Set(key, value string) error          { k.m[key] = value; return nil }
func (k *memKB) Delete(key string) error              { delete(k.m, key); return nil }

// TestCredentialVaultRoundTrip pins the basic store/lookup/delete contract.
func TestCredentialVaultRoundTrip(t *testing.T) {
	kb := &memKB{m: map[string]string{}}
	v := auth.NewCredentialVault(kb)
	scopes := []string{"https://www.googleapis.com/auth/gmail.readonly"}
	if err := v.StoreRefreshToken("gum_oauth", "fp123", scopes, "1//rt"); err != nil {
		t.Fatalf("Store: %v", err)
	}
	got, err := v.LookupRefreshToken("gum_oauth", "fp123", scopes)
	if err != nil || got != "1//rt" {
		t.Errorf("Lookup = (%q, %v); want (\"1//rt\", nil)", got, err)
	}
	if err := v.DeleteRefreshToken("gum_oauth", "fp123", scopes); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if got, _ := v.LookupRefreshToken("gum_oauth", "fp123", scopes); got != "" {
		t.Errorf("Lookup after Delete = %q; want empty", got)
	}
}

// TestCredentialVaultKeySeparation verifies spec §10.0.1: per-subject
// scoping. The same strategy+scopes under two different subject fingerprints
// MUST land in different keychain entries so credential switching never
// replays the prior subject's token.
func TestCredentialVaultKeySeparation(t *testing.T) {
	kb := &memKB{m: map[string]string{}}
	v := auth.NewCredentialVault(kb)
	scopes := []string{"https://www.googleapis.com/auth/gmail.readonly"}
	if err := v.StoreRefreshToken("gum_oauth", "fpAAA", scopes, "rt-A"); err != nil {
		t.Fatal(err)
	}
	if err := v.StoreRefreshToken("gum_oauth", "fpBBB", scopes, "rt-B"); err != nil {
		t.Fatal(err)
	}
	a, _ := v.LookupRefreshToken("gum_oauth", "fpAAA", scopes)
	b, _ := v.LookupRefreshToken("gum_oauth", "fpBBB", scopes)
	if a != "rt-A" || b != "rt-B" {
		t.Errorf("subject-scoped lookup leaked: A=%q B=%q", a, b)
	}
	if len(kb.m) != 2 {
		t.Errorf("expected 2 distinct keychain entries, got %d", len(kb.m))
	}
	// Verify the key shape — must include the strategy + a subject prefix
	// so a security audit can grep for `gum.gum_oauth.<fp>` entries.
	for k := range kb.m {
		if !strings.HasPrefix(k, "gum.gum_oauth.") {
			t.Errorf("vault key %q does not have the `gum.<strategy>.` shape", k)
		}
	}
}

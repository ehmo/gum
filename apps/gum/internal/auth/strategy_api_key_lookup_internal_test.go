package auth

import "testing"

// TestDefaultLookupBranches pins the keyring-vs-env precedence:
// keyring value wins when present and non-blank; whitespace-only
// keyring values fall through to env; nil keyring also falls through.
// Air-gapped CI relies on the env-var fallback being reachable even
// when no keychain backend is installed.
func TestDefaultLookupBranches(t *testing.T) {
	t.Run("keyring_value_wins", func(t *testing.T) {
		t.Setenv(EnvAPIKeyVar, "from-env")
		kb := &mockKeyring{data: map[string]string{apiKeyKeyringKey("default"): "from-keyring"}}
		r := &APIKeyResolver{Keyring: kb}
		if got := r.defaultLookup(); got != "from-keyring" {
			t.Errorf("got=%q; want from-keyring", got)
		}
	})

	t.Run("whitespace_keyring_falls_through_to_env", func(t *testing.T) {
		t.Setenv(EnvAPIKeyVar, "from-env")
		kb := &mockKeyring{data: map[string]string{apiKeyKeyringKey("default"): "   "}}
		r := &APIKeyResolver{Keyring: kb}
		if got := r.defaultLookup(); got != "from-env" {
			t.Errorf("got=%q; want from-env (whitespace keyring)", got)
		}
	})

	t.Run("nil_keyring_uses_env", func(t *testing.T) {
		t.Setenv(EnvAPIKeyVar, "env-only")
		r := &APIKeyResolver{}
		if got := r.defaultLookup(); got != "env-only" {
			t.Errorf("got=%q; want env-only", got)
		}
	})

	t.Run("trims_whitespace_around_env_value", func(t *testing.T) {
		t.Setenv(EnvAPIKeyVar, "  trimmed  ")
		r := &APIKeyResolver{}
		if got := r.defaultLookup(); got != "trimmed" {
			t.Errorf("got=%q; want trimmed", got)
		}
	})

	t.Run("everything_empty_returns_empty", func(t *testing.T) {
		t.Setenv(EnvAPIKeyVar, "")
		r := &APIKeyResolver{}
		if got := r.defaultLookup(); got != "" {
			t.Errorf("got=%q; want empty", got)
		}
	})
}

// mockKeyring is a minimal KeyringBackend stub for these table tests.
type mockKeyring struct {
	data map[string]string
}

func (m *mockKeyring) Get(key string) (string, error) {
	if v, ok := m.data[key]; ok {
		return v, nil
	}
	return "", nil
}
func (m *mockKeyring) Set(key, value string) error { m.data[key] = value; return nil }
func (m *mockKeyring) Delete(key string) error     { delete(m.data, key); return nil }

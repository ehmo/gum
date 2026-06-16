package auth_test

import (
	"errors"
	"testing"

	"github.com/ehmo/gum/internal/auth"
)

// keyringStub is a minimal in-memory KeyringBackend that lets us drive the
// StoreAPIKey/LookupAPIKey/DeleteAPIKey trio under unit control.
type keyringStub struct {
	store map[string]string
}

func newKeyringStub() *keyringStub { return &keyringStub{store: map[string]string{}} }

func (k *keyringStub) Get(key string) (string, error) { return k.store[key], nil }
func (k *keyringStub) Set(key, value string) error    { k.store[key] = value; return nil }
func (k *keyringStub) Delete(key string) error        { delete(k.store, key); return nil }

// TestStoreAPIKey covers the two paths: backend present → key is persisted;
// backend nil → AUTH_KEYCHAIN_UNAVAILABLE *AuthError.
func TestStoreAPIKey(t *testing.T) {
	t.Run("backend_present_persists", func(t *testing.T) {
		kb := newKeyringStub()
		if err := auth.StoreAPIKey(kb, "profile-a", "the-key"); err != nil {
			t.Fatalf("Store: %v", err)
		}
		got, _ := auth.LookupAPIKey(kb, "profile-a")
		if got != "the-key" {
			t.Errorf("persisted = %q, want the-key", got)
		}
	})

	t.Run("nil_backend_returns_AUTH_KEYCHAIN_UNAVAILABLE", func(t *testing.T) {
		err := auth.StoreAPIKey(nil, "profile-a", "the-key")
		var ae *auth.AuthError
		if !errors.As(err, &ae) {
			t.Fatalf("expected *AuthError, got %T: %v", err, err)
		}
		if ae.Code != "AUTH_KEYCHAIN_UNAVAILABLE" {
			t.Errorf("Code = %q, want AUTH_KEYCHAIN_UNAVAILABLE", ae.Code)
		}
	})
}

// TestLookupAPIKey covers the three branches: nil backend → "", nil err;
// present key returned; missing key → "" without error.
func TestLookupAPIKey(t *testing.T) {
	t.Run("nil_backend_returns_empty", func(t *testing.T) {
		got, err := auth.LookupAPIKey(nil, "p")
		if err != nil {
			t.Errorf("err = %v, want nil", err)
		}
		if got != "" {
			t.Errorf("got = %q, want empty", got)
		}
	})

	t.Run("missing_key_returns_empty", func(t *testing.T) {
		kb := newKeyringStub()
		got, err := auth.LookupAPIKey(kb, "p")
		if err != nil {
			t.Fatalf("err = %v", err)
		}
		if got != "" {
			t.Errorf("got = %q, want empty for missing key", got)
		}
	})

	t.Run("present_key_returned", func(t *testing.T) {
		kb := newKeyringStub()
		_ = auth.StoreAPIKey(kb, "p", "k")
		got, _ := auth.LookupAPIKey(kb, "p")
		if got != "k" {
			t.Errorf("got = %q, want k", got)
		}
	})
}

// TestDeleteAPIKey covers the trivial branches: nil backend → no error;
// backend.Delete is called for the resolved keyring key.
func TestDeleteAPIKey(t *testing.T) {
	t.Run("nil_backend_is_noop", func(t *testing.T) {
		if err := auth.DeleteAPIKey(nil, "p"); err != nil {
			t.Errorf("err = %v, want nil", err)
		}
	})

	t.Run("present_backend_removes", func(t *testing.T) {
		kb := newKeyringStub()
		_ = auth.StoreAPIKey(kb, "p", "k")
		if err := auth.DeleteAPIKey(kb, "p"); err != nil {
			t.Fatalf("err = %v", err)
		}
		got, _ := auth.LookupAPIKey(kb, "p")
		if got != "" {
			t.Errorf("post-delete lookup = %q, want empty", got)
		}
	})
}

// TestNewServiceAccountResolverFromEnv covers the env-driven SA constructor.
// Empty env → constructor returns a typed AuthError (caller treats as
// "operator hasn't set GUM_SERVICE_ACCOUNT_KEY").
func TestNewServiceAccountResolverFromEnv(t *testing.T) {
	t.Setenv("GUM_SERVICE_ACCOUNT_KEY", "")
	_, err := auth.NewServiceAccountResolverFromEnv()
	if err == nil {
		t.Errorf("expected error when GUM_SERVICE_ACCOUNT_KEY is empty, got nil")
	}
}

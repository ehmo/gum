package auth_test

import (
	"testing"

	"github.com/ehmo/gum/internal/auth"
)

// TestStoreAndLoadByoClientRoundTrips verifies a stored Desktop OAuth client
// (client_id + secret) reads back intact under the same profile.
func TestStoreAndLoadByoClientRoundTrips(t *testing.T) {
	kb := newKeyringStub()
	want := auth.ByoClient{ClientID: "123.apps.googleusercontent.com", ClientSecret: "GOCSPX-abc"}
	if err := auth.StoreByoClient(kb, "default", want); err != nil {
		t.Fatalf("StoreByoClient: %v", err)
	}
	got, ok, err := auth.LoadByoClient(kb, "default")
	if err != nil {
		t.Fatalf("LoadByoClient: %v", err)
	}
	if !ok {
		t.Fatal("LoadByoClient ok=false, want true")
	}
	if got != want {
		t.Fatalf("LoadByoClient = %+v, want %+v", got, want)
	}
}

// TestLoadByoClientAbsentReportsNotConfigured verifies a profile with no stored
// client returns ok=false (so callers can route to `gum auth use-oauth-client`)
// rather than an error.
func TestLoadByoClientAbsentReportsNotConfigured(t *testing.T) {
	got, ok, err := auth.LoadByoClient(newKeyringStub(), "default")
	if err != nil {
		t.Fatalf("LoadByoClient: %v", err)
	}
	if ok {
		t.Fatalf("LoadByoClient ok=true for empty keyring, got %+v", got)
	}
}

func TestLoadByoClientMalformedOrEmptyIDReportsNotConfigured(t *testing.T) {
	t.Run("malformed_json_errors", func(t *testing.T) {
		kb := newKeyringStub()
		if err := auth.StoreByoClient(kb, "default", auth.ByoClient{ClientID: "id"}); err != nil {
			t.Fatalf("StoreByoClient: %v", err)
		}
		for k := range kb.store {
			kb.store[k] = "{"
		}
		if _, _, err := auth.LoadByoClient(kb, "default"); err == nil {
			t.Fatal("LoadByoClient malformed JSON = nil, want error")
		}
	})

	t.Run("empty_client_id_is_absent", func(t *testing.T) {
		kb := newKeyringStub()
		if err := auth.StoreByoClient(kb, "default", auth.ByoClient{ClientID: "id"}); err != nil {
			t.Fatalf("StoreByoClient: %v", err)
		}
		for k := range kb.store {
			kb.store[k] = `{"client_id":""}`
		}
		got, ok, err := auth.LoadByoClient(kb, "default")
		if err != nil || ok {
			t.Fatalf("LoadByoClient empty id = %+v ok=%v err=%v, want absent nil", got, ok, err)
		}
	})
}

// TestStoreByoClientRejectsEmptyClientID verifies a client_id is mandatory: a
// secret without an id is not a usable OAuth client and must be refused at
// configure time, not at first dispatch.
func TestStoreByoClientRejectsEmptyClientID(t *testing.T) {
	if err := auth.StoreByoClient(newKeyringStub(), "default", auth.ByoClient{ClientSecret: "x"}); err == nil {
		t.Fatal("StoreByoClient(empty client_id) = nil, want error")
	}
}

// TestStoreByoClientAllowsEmptySecret verifies a public PKCE client (no secret)
// is valid — Desktop-app clients increasingly ship without a secret, and the
// loopback+PKCE flow does not require one.
func TestStoreByoClientAllowsEmptySecret(t *testing.T) {
	kb := newKeyringStub()
	if err := auth.StoreByoClient(kb, "default", auth.ByoClient{ClientID: "id-only"}); err != nil {
		t.Fatalf("StoreByoClient(no secret): %v", err)
	}
	got, ok, _ := auth.LoadByoClient(kb, "default")
	if !ok || got.ClientID != "id-only" || got.ClientSecret != "" {
		t.Fatalf("LoadByoClient = %+v ok=%v, want {id-only, \"\"}", got, ok)
	}
}

// TestStoreByoClientNilKeyringErrors verifies a missing keychain backend
// surfaces AUTH_KEYCHAIN_UNAVAILABLE rather than panicking.
func TestStoreByoClientNilKeyringErrors(t *testing.T) {
	if err := auth.StoreByoClient(nil, "default", auth.ByoClient{ClientID: "x"}); err == nil {
		t.Fatal("StoreByoClient(nil keyring) = nil, want AUTH_KEYCHAIN_UNAVAILABLE")
	}
}

// TestDeleteByoClientRemovesStoredClient verifies the delete helper clears the
// per-profile keyring entry and treats a nil keyring as a no-op.
func TestDeleteByoClientRemovesStoredClient(t *testing.T) {
	kb := newKeyringStub()
	if err := auth.StoreByoClient(kb, "default", auth.ByoClient{ClientID: "id"}); err != nil {
		t.Fatalf("StoreByoClient: %v", err)
	}
	if err := auth.DeleteByoClient(kb, "default"); err != nil {
		t.Fatalf("DeleteByoClient: %v", err)
	}
	if got, ok, err := auth.LoadByoClient(kb, "default"); err != nil || ok {
		t.Fatalf("LoadByoClient after delete = %+v ok=%v err=%v, want absent nil", got, ok, err)
	}
	if err := auth.DeleteByoClient(nil, "default"); err != nil {
		t.Fatalf("DeleteByoClient(nil) = %v, want nil", err)
	}
}

// TestNormaliseScopesExportedCoversShortEmptyAndURLForms pins the exported
// helper used by CLI auth paths.
func TestNormaliseScopesExportedCoversShortEmptyAndURLForms(t *testing.T) {
	got := auth.NormaliseScopes([]string{"", "gmail.readonly", "https://example.test/custom", "http://example.test/custom"})
	want := []string{
		"https://www.googleapis.com/auth/gmail.readonly",
		"https://example.test/custom",
		"http://example.test/custom",
	}
	if len(got) != len(want) {
		t.Fatalf("NormaliseScopes len=%d (%v), want %d (%v)", len(got), got, len(want), want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("NormaliseScopes[%d]=%q, want %q (all=%v)", i, got[i], want[i], got)
		}
	}
}

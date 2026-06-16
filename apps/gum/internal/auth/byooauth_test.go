package auth_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/goleak"

	"github.com/ehmo/gum/internal/auth"
)

// memKeyring is an in-memory KeyringBackend for unit tests.
type memKeyring struct {
	store map[string]string
}

func newMemKeyring() *memKeyring {
	return &memKeyring{store: make(map[string]string)}
}

func (m *memKeyring) Get(key string) (string, error) {
	v, ok := m.store[key]
	if !ok {
		return "", nil
	}
	return v, nil
}

func (m *memKeyring) Set(key, value string) error {
	m.store[key] = value
	return nil
}

func (m *memKeyring) Delete(key string) error {
	delete(m.store, key)
	return nil
}

// tokenResponse mirrors the Google OAuth token endpoint JSON response.
type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token,omitempty"`
}

// TestByoOauthTokenRefresh injects an in-memory KeyringBackend that holds a
// refresh token and wires the token endpoint to an httptest.Server.
// Asserts that Acquire completes the refresh path and returns valid Credentials.
func TestByoOauthTokenRefresh(t *testing.T) {
	// Register goleak BEFORE httptest server so cleanup order (LIFO) runs
	// srv.Close() before goleak.VerifyNone().
	t.Cleanup(func() { goleak.VerifyNone(t) })

	const fakeRefreshToken = "fake-refresh-token-12345"
	const fakeAccessToken = "ya29.fake-access-token"

	var requestCount atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
		if r.FormValue("grant_type") != "refresh_token" {
			http.Error(w, "expected refresh_token grant", http.StatusBadRequest)
			return
		}
		if r.FormValue("refresh_token") != fakeRefreshToken {
			http.Error(w, "wrong refresh token", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(tokenResponse{
			AccessToken: fakeAccessToken,
			TokenType:   "Bearer",
			ExpiresIn:   3600,
		})
	}))
	t.Cleanup(srv.Close)

	kb := newMemKeyring()
	cfg := auth.ByoOAuthConfig{
		ClientID:      "test-client-id.apps.googleusercontent.com",
		ClientSecret:  "test-client-secret",
		Scopes:        []string{"https://www.googleapis.com/auth/gmail.readonly"},
		TokenEndpoint: srv.URL,
	}

	var byo *auth.ByoOAuth
	msg, panicked := catchPanic(func() {
		byo = auth.NewByoOAuth(cfg, kb)
	})
	if panicked {
		t.Fatalf("NewByoOAuth panicked: %s — green team must implement NewByoOAuth", msg)
	}

	// Store a refresh token.
	msg, panicked = catchPanic(func() {
		if err := byo.StoreRefreshToken(fakeRefreshToken); err != nil {
			t.Errorf("StoreRefreshToken: %v", err)
		}
	})
	if panicked {
		t.Fatalf("StoreRefreshToken panicked: %s — green team must implement StoreRefreshToken", msg)
	}

	var creds *auth.Credentials
	var acquireErr error
	msg, panicked = catchPanic(func() {
		creds, acquireErr = byo.Acquire(t.Context())
	})
	if panicked {
		t.Fatalf("Acquire panicked: %s — green team must implement Acquire", msg)
	}

	if acquireErr != nil {
		t.Fatalf("Acquire: %v", acquireErr)
	}
	if creds.Token != fakeAccessToken {
		t.Errorf("Token = %q, want %q", creds.Token, fakeAccessToken)
	}
	if creds.ExpiresAt.IsZero() {
		t.Error("ExpiresAt is zero; expected a non-zero expiry from expires_in=3600")
	}
	if creds.ExpiresAt.Before(time.Now()) {
		t.Errorf("ExpiresAt %v is in the past", creds.ExpiresAt)
	}
	if requestCount.Load() == 0 {
		t.Error("token endpoint was never called; Acquire did not contact the server")
	}
}

// TestByoOauthNoRefreshToken asserts that Acquire returns a typed *AuthError
// when the keyring has no refresh token stored.
func TestByoOauthNoRefreshToken(t *testing.T) {
	defer goleak.VerifyNone(t)

	kb := newMemKeyring()
	cfg := auth.ByoOAuthConfig{
		ClientID:     "test-client-id.apps.googleusercontent.com",
		ClientSecret: "test-client-secret",
		Scopes:       []string{"https://www.googleapis.com/auth/gmail.readonly"},
	}

	var byo *auth.ByoOAuth
	msg, panicked := catchPanic(func() {
		byo = auth.NewByoOAuth(cfg, kb)
	})
	if panicked {
		t.Fatalf("NewByoOAuth panicked: %s — green team must implement NewByoOAuth", msg)
	}

	var acquireErr error
	msg, panicked = catchPanic(func() {
		_, acquireErr = byo.Acquire(t.Context())
	})
	if panicked {
		t.Fatalf("Acquire panicked: %s — green team must implement Acquire", msg)
	}

	if acquireErr == nil {
		t.Fatal("expected error when no refresh token is cached, got nil")
	}
	authErr, ok := acquireErr.(*auth.AuthError)
	if !ok {
		t.Fatalf("expected *auth.AuthError, got %T: %v", acquireErr, acquireErr)
	}
	if authErr.Code != "NO_REFRESH_TOKEN" {
		t.Errorf("AuthError.Code = %q, want %q", authErr.Code, "NO_REFRESH_TOKEN")
	}
}

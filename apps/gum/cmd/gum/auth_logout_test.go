package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	keyringlib "github.com/zalando/go-keyring"

	"github.com/ehmo/gum/internal/auth"
)

// stubRevokeEndpoint points the best-effort revocation endpoint at a local
// server so `gum logout` command tests stay hermetic (no call to Google).
func stubRevokeEndpoint(t *testing.T) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	prev := auth.DefaultRevokeEndpoint
	auth.DefaultRevokeEndpoint = srv.URL
	t.Cleanup(func() {
		auth.DefaultRevokeEndpoint = prev
		srv.Close()
		http.DefaultClient.CloseIdleConnections()
	})
}

// TestLogoutCommandClearsGrant pins the `gum logout` command end-to-end: it
// revokes the stored grant for the active profile (so GrantedScopes drops to
// nil) while keeping the registered client (no --forget-client), and reports
// grant_cleared in its JSON output.
func TestLogoutCommandClearsGrant(t *testing.T) {
	stubRevokeEndpoint(t)
	keyringlib.MockInit()
	kb := auth.NewOSKeyring()
	scope := "https://www.googleapis.com/auth/webmasters.readonly"
	if err := auth.StoreByoClient(kb, "default", auth.ByoClient{ClientID: "cid"}); err != nil {
		t.Fatalf("StoreByoClient: %v", err)
	}
	b := auth.NewByoOAuth(auth.ByoOAuthConfig{ClientID: "cid", Scopes: []string{scope}}, kb)
	if err := b.StoreRefreshToken("rt-1"); err != nil {
		t.Fatalf("StoreRefreshToken: %v", err)
	}
	if got := auth.GrantedScopes(kb, "default"); len(got) != 1 {
		t.Fatalf("precondition: GrantedScopes = %v, want exactly one scope", got)
	}

	root := newRootCmd()
	var out, errBuf bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errBuf)
	root.SetArgs([]string{"logout"})
	if err := root.Execute(); err != nil {
		t.Fatalf("gum logout: %v\nstderr: %s", err, errBuf.String())
	}

	if got := auth.GrantedScopes(kb, "default"); got != nil {
		t.Errorf("after logout GrantedScopes = %v, want nil", got)
	}
	if _, ok, _ := auth.LoadByoClient(kb, "default"); !ok {
		t.Error("registered client removed despite no --forget-client")
	}

	var payload map[string]any
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("output is not JSON: %q (%v)", out.String(), err)
	}
	if payload["grant_cleared"] != true {
		t.Errorf("grant_cleared = %v, want true", payload["grant_cleared"])
	}
	if payload["client_forgotten"] != false {
		t.Errorf("client_forgotten = %v, want false", payload["client_forgotten"])
	}
}

// TestLogoutCommandForgetClient pins that `gum logout --forget-client` also
// removes the registered BYO client entry.
func TestLogoutCommandForgetClient(t *testing.T) {
	stubRevokeEndpoint(t)
	keyringlib.MockInit()
	kb := auth.NewOSKeyring()
	if err := auth.StoreByoClient(kb, "default", auth.ByoClient{ClientID: "cid"}); err != nil {
		t.Fatalf("StoreByoClient: %v", err)
	}
	b := auth.NewByoOAuth(auth.ByoOAuthConfig{ClientID: "cid", Scopes: []string{"s"}}, kb)
	if err := b.StoreRefreshToken("rt-1"); err != nil {
		t.Fatalf("StoreRefreshToken: %v", err)
	}

	root := newRootCmd()
	var out, errBuf bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errBuf)
	root.SetArgs([]string{"logout", "--forget-client"})
	if err := root.Execute(); err != nil {
		t.Fatalf("gum logout --forget-client: %v\nstderr: %s", err, errBuf.String())
	}

	if _, ok, _ := auth.LoadByoClient(kb, "default"); ok {
		t.Error("registered client still present after --forget-client")
	}

	var payload map[string]any
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("output is not JSON: %q (%v)", out.String(), err)
	}
	if payload["client_forgotten"] != true {
		t.Errorf("client_forgotten = %v, want true", payload["client_forgotten"])
	}
	if payload["grant_cleared"] != true {
		t.Errorf("grant_cleared = %v, want true", payload["grant_cleared"])
	}
}

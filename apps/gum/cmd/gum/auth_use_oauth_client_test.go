package main

import (
	"bytes"
	"strings"
	"testing"

	keyringlib "github.com/zalando/go-keyring"

	"github.com/ehmo/gum/internal/auth"
)

// TestAuthUseOAuthClientRequiresClientID pins that --client-id is mandatory:
// without it there is no OAuth client to register.
func TestAuthUseOAuthClientRequiresClientID(t *testing.T) {
	cmd := newAuthUseOAuthClientCmd()
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs(nil)
	if err := cmd.Execute(); err == nil {
		t.Fatalf("expected CLI_ARG_INVALID; got success")
	}
}

// TestAuthUseOAuthClientStoresPublicClient pins the public-PKCE happy path:
// only --client-id, no secret, persisted to the keychain and loadable back.
func TestAuthUseOAuthClientStoresPublicClient(t *testing.T) {
	keyringlib.MockInit()
	t.Cleanup(keyringlib.MockInit)

	cmd := newAuthUseOAuthClientCmd()
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetArgs([]string{"--client-id", "999.apps.googleusercontent.com"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v; stdout=%q", err, stdout.String())
	}
	if !strings.Contains(stdout.String(), "public PKCE client") {
		t.Errorf("stdout did not note public client: %q", stdout.String())
	}
	got, ok, err := auth.LoadByoClient(auth.NewOSKeyring(), auth.DefaultAPIKeyProfile)
	if err != nil || !ok {
		t.Fatalf("LoadByoClient ok=%v err=%v", ok, err)
	}
	if got.ClientID != "999.apps.googleusercontent.com" || got.ClientSecret != "" {
		t.Errorf("stored client = %+v, want id only", got)
	}
}

// TestAuthUseOAuthClientSecretFromStdinNotEchoed pins that a piped secret is
// stored but never echoed to stdout (shell-history / scrollback hygiene).
func TestAuthUseOAuthClientSecretFromStdinNotEchoed(t *testing.T) {
	keyringlib.MockInit()
	t.Cleanup(keyringlib.MockInit)

	const secret = "GOCSPX-do-not-echo"
	cmd := newAuthUseOAuthClientCmd()
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetIn(strings.NewReader(secret + "\n"))
	cmd.SetArgs([]string{"--client-id", "id-x", "--secret-stdin"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if strings.Contains(stdout.String(), secret) {
		t.Errorf("stdout leaked the secret: %q", stdout.String())
	}
	got, ok, _ := auth.LoadByoClient(auth.NewOSKeyring(), auth.DefaultAPIKeyProfile)
	if !ok || got.ClientSecret != secret {
		t.Errorf("stored secret = %q ok=%v, want %q", got.ClientSecret, ok, secret)
	}
}

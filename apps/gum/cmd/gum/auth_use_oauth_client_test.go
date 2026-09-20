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

// runUseOAuthClient executes `gum auth use-oauth-client` with args and returns
// its stdout.
func runUseOAuthClient(t *testing.T, stdin string, args ...string) string {
	t.Helper()
	root := newRootCmd()
	var out, errBuf bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errBuf)
	root.SetIn(strings.NewReader(stdin))
	root.SetArgs(append([]string{"auth", "use-oauth-client"}, args...))
	if err := root.Execute(); err != nil {
		t.Fatalf("gum auth use-oauth-client %v: %v\nstderr: %s", args, err, errBuf.String())
	}
	return out.String()
}

// TestUseOAuthClientKeepsSecretWhenReRegistered pins gum-ak65: re-running
// use-oauth-client for the same client id without a secret flag must not erase
// the stored secret. Google's token endpoint requires the secret for a
// Desktop-app client, so wiping it broke the next refresh with invalid_client.
func TestUseOAuthClientKeepsSecretWhenReRegistered(t *testing.T) {
	keyringlib.MockInit()
	t.Cleanup(keyringlib.MockInit)
	kb := auth.NewOSKeyring()

	runUseOAuthClient(t, "s3cr3t\n", "--client-id", "cid", "--secret-stdin")
	stored, ok, err := auth.LoadByoClient(kb, "default")
	if err != nil || !ok {
		t.Fatalf("precondition LoadByoClient: ok=%v err=%v", ok, err)
	}
	if stored.ClientSecret != "s3cr3t" {
		t.Fatalf("precondition ClientSecret = %q, want %q", stored.ClientSecret, "s3cr3t")
	}

	out := runUseOAuthClient(t, "", "--client-id", "cid")
	stored, ok, err = auth.LoadByoClient(kb, "default")
	if err != nil || !ok {
		t.Fatalf("LoadByoClient after re-register: ok=%v err=%v", ok, err)
	}
	if stored.ClientSecret != "s3cr3t" {
		t.Errorf("ClientSecret = %q after re-register without a secret flag; want the stored secret kept", stored.ClientSecret)
	}
	if strings.Contains(out, "public PKCE client") {
		t.Errorf("output claims a public PKCE client while a secret is stored:\n%s", out)
	}
}

// TestUseOAuthClientClearsSecretForNewClientID pins the other half: a
// different client id starts fresh, so the previous client's secret is not
// carried over onto it.
func TestUseOAuthClientClearsSecretForNewClientID(t *testing.T) {
	keyringlib.MockInit()
	t.Cleanup(keyringlib.MockInit)
	kb := auth.NewOSKeyring()

	runUseOAuthClient(t, "s3cr3t\n", "--client-id", "cid", "--secret-stdin")
	out := runUseOAuthClient(t, "", "--client-id", "other-cid")

	stored, ok, err := auth.LoadByoClient(kb, "default")
	if err != nil || !ok {
		t.Fatalf("LoadByoClient: ok=%v err=%v", ok, err)
	}
	if stored.ClientID != "other-cid" {
		t.Fatalf("ClientID = %q, want other-cid", stored.ClientID)
	}
	if stored.ClientSecret != "" {
		t.Errorf("ClientSecret = %q; a different client id must not inherit the old secret", stored.ClientSecret)
	}
	if !strings.Contains(out, "public PKCE client") {
		t.Errorf("output does not report the public-client case:\n%s", out)
	}
}

// TestUseOAuthClientEmptySecretStdinClearsSecret pins the explicit clear:
// --secret-stdin with empty input is the operator saying "no secret", which
// must overwrite a stored one.
func TestUseOAuthClientEmptySecretStdinClearsSecret(t *testing.T) {
	keyringlib.MockInit()
	t.Cleanup(keyringlib.MockInit)
	kb := auth.NewOSKeyring()

	runUseOAuthClient(t, "s3cr3t\n", "--client-id", "cid", "--secret-stdin")
	runUseOAuthClient(t, "", "--client-id", "cid", "--secret-stdin")

	stored, ok, err := auth.LoadByoClient(kb, "default")
	if err != nil || !ok {
		t.Fatalf("LoadByoClient: ok=%v err=%v", ok, err)
	}
	if stored.ClientSecret != "" {
		t.Errorf("ClientSecret = %q; an explicit empty --secret-stdin must clear it", stored.ClientSecret)
	}
}

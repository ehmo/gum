// Spec gum-6hcr: `gum auth use-api-key` must NOT accept the key as a
// positional argv (shell history + process listing leak). The fix:
//   - Reject positional arg with CLI_ARG_INVALID + remediation hint.
//   - Read the key from stdin (--stdin or piped) or from a file (--from-file).
//   - When the OS keyring is available, persist there and NEVER echo the
//     key on stdout. Round-trip through APIKeyResolver works.
//   - When keyring is unavailable, print env-var instructions WITHOUT the
//     key bytes (the operator copies the key from their own clipboard /
//     password manager).

package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	keyringlib "github.com/zalando/go-keyring"

	"github.com/ehmo/gum/internal/auth"
)

// TestAuthUseAPIKeyRejectsPositionalArg pins the security invariant: the
// CLI MUST refuse positional input.
func TestAuthUseAPIKeyRejectsPositionalArg(t *testing.T) {
	cmd := newAuthUseAPIKeyCmd()
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"AIza-leaks-via-argv"})

	err := cmd.Execute()
	if err == nil {
		t.Fatalf("expected CLI_ARG_INVALID; got success. stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	if !strings.Contains(err.Error(), "CLI_ARG_INVALID") && !strings.Contains(err.Error(), "--stdin") {
		t.Errorf("err = %v; want CLI_ARG_INVALID with --stdin hint", err)
	}
	if strings.Contains(stdout.String(), "AIza-leaks-via-argv") || strings.Contains(stderr.String(), "AIza-leaks-via-argv") {
		t.Errorf("key bytes echoed to output: stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

// TestAuthUseAPIKeyStdinStoresInKeyring pins the happy path: stdin →
// keyring; no key bytes on stdout; subsequent APIKeyResolver lookup
// returns the same value.
func TestAuthUseAPIKeyStdinStoresInKeyring(t *testing.T) {
	keyringlib.MockInit()
	t.Cleanup(keyringlib.MockInit)

	const key = "AIza-via-stdin-secret"
	cmd := newAuthUseAPIKeyCmd()
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetIn(strings.NewReader(key + "\n"))
	cmd.SetArgs([]string{"--stdin"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v; stdout=%q", err, stdout.String())
	}

	if strings.Contains(stdout.String(), key) {
		t.Errorf("stdout leaked the key: %q", stdout.String())
	}

	resolver := &auth.APIKeyResolver{
		Keyring: auth.NewOSKeyring(),
		Profile: auth.DefaultAPIKeyProfile,
	}
	resolver.Lookup = func() string {
		v, _ := auth.LookupAPIKey(resolver.Keyring, resolver.Profile)
		return v
	}
	creds, err := resolver.Resolve(context.Background(), nil)
	if err != nil {
		t.Fatalf("resolver: %v", err)
	}
	if creds.APIKey != key {
		t.Errorf("round-trip APIKey = %q; want %q", creds.APIKey, key)
	}
}

// TestAuthUseAPIKeyEmptyKeyRejected pins the spec §7 guard: a piped
// empty / whitespace-only payload must fail CLI_ARG_INVALID rather
// than store an empty string in the keyring.
func TestAuthUseAPIKeyEmptyKeyRejected(t *testing.T) {
	keyringlib.MockInit()
	t.Cleanup(keyringlib.MockInit)
	cmd := newAuthUseAPIKeyCmd()
	cmd.SetIn(strings.NewReader("   \n"))
	cmd.SetArgs([]string{"--stdin"})
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "CLI_ARG_INVALID") {
		t.Errorf("want CLI_ARG_INVALID empty-key error; got %v; out=%q", err, buf.String())
	}
}

// TestAuthUseAPIKeyFromFile pins the --from-file branch: bytes are
// read from disk and stored under the (non-default) profile.
func TestAuthUseAPIKeyFromFile(t *testing.T) {
	keyringlib.MockInit()
	t.Cleanup(keyringlib.MockInit)

	dir := t.TempDir()
	keyfile := filepath.Join(dir, "k")
	const key = "AIza-from-file"
	if err := os.WriteFile(keyfile, []byte(key+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	// --profile is the root persistent flag (no longer a per-command shadow),
	// so exercise via root: `gum auth use-api-key ... --profile work`.
	root := newRootCmd()
	root.SetArgs([]string{"auth", "use-api-key", "--from-file", keyfile, "--profile", "work"})
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v\nout=%s", err, buf.String())
	}
	if !strings.Contains(buf.String(), `profile "work"`) {
		t.Errorf("expected profile %q acknowledgement in output; got %q", "work", buf.String())
	}
	got, err := auth.LookupAPIKey(auth.NewOSKeyring(), "work")
	if err != nil || got != key {
		t.Errorf("LookupAPIKey(work) = %q, %v; want %q", got, err, key)
	}
}

// TestAuthUseAPIKeyFromFileMissingFails pins the read-error path so a
// typo'd path produces an actionable error, not a silent store.
func TestAuthUseAPIKeyFromFileMissingFails(t *testing.T) {
	cmd := newAuthUseAPIKeyCmd()
	cmd.SetArgs([]string{"--from-file", filepath.Join(t.TempDir(), "absent")})
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "read --from-file") {
		t.Errorf("want 'read --from-file' error; got %v", err)
	}
}

// TestAuthUseAPIKeyKeyringUnavailableFallback pins the no-backend
// branch: when StoreAPIKey fails, the CLI prints env-var instructions
// and MUST NOT echo the key bytes.
func TestAuthUseAPIKeyKeyringUnavailableFallback(t *testing.T) {
	keyringlib.MockInitWithError(errors.New("backend unavailable"))
	t.Cleanup(keyringlib.MockInit)

	const key = "AIza-keyring-down-secret"
	cmd := newAuthUseAPIKeyCmd()
	cmd.SetIn(strings.NewReader(key))
	cmd.SetArgs([]string{"--stdin"})
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v\nout=%s", err, buf.String())
	}
	out := buf.String()
	if !strings.Contains(out, "OS keychain backend unavailable") {
		t.Errorf("want 'OS keychain backend unavailable' message; got %q", out)
	}
	if !strings.Contains(out, auth.EnvAPIKeyVar) {
		t.Errorf("want env-var %q in fallback instructions; got %q", auth.EnvAPIKeyVar, out)
	}
	if strings.Contains(out, key) {
		t.Errorf("key leaked into fallback output: %q", out)
	}
}

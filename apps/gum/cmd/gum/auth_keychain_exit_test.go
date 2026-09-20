// A failed secret store must not exit 0. `gum auth use-api-key` and
// `gum auth use-ads-developer-token` both treated EVERY keychain error as
// "backend unavailable on this platform", printed env-var instructions, and
// returned nil. A locked keychain or a denied write therefore reported success
// to a script that piped in a secret and checked the exit code.
//
// The platform-without-a-backend case is still a soft fallback, because there
// is nothing to fix and the env var is the documented alternative.

package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	keyringlib "github.com/zalando/go-keyring"

	"github.com/ehmo/gum/internal/auth"
)

// runSecretStoreCmd pipes secret into cmd and returns stdout+stderr and the
// Execute error.
func runSecretStoreCmd(t *testing.T, newCmd func() *cobra.Command, secret string) (string, error) {
	t.Helper()
	cmd := newCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetIn(strings.NewReader(secret + "\n"))
	cmd.SetArgs([]string{"--stdin"})
	err := cmd.Execute()
	return buf.String(), err
}

// TestSecretStoreFailureExitsNonZero pins the exit code for a real keychain
// fault: the OS backend exists but the operation failed.
func TestSecretStoreFailureExitsNonZero(t *testing.T) {
	cases := []struct {
		name   string
		newCmd func() *cobra.Command
	}{
		{"use-api-key", newAuthUseAPIKeyCmd},
		{"use-ads-developer-token", newAuthUseAdsDeveloperTokenCmd},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			keyringlib.MockInitWithError(errors.New("keychain is locked"))
			t.Cleanup(keyringlib.MockInit)

			out, err := runSecretStoreCmd(t, tc.newCmd, "secret-value-abc")
			if err == nil {
				t.Fatalf("store failure returned nil error (exit 0); out=%q", out)
			}
			if strings.Contains(out, "secret-value-abc") {
				t.Errorf("output leaked the secret: %q", out)
			}
			if !strings.Contains(err.Error(), "AUTH_KEYCHAIN_UNAVAILABLE") && !strings.Contains(err.Error(), "keychain") {
				t.Errorf("err = %v; want a keychain store failure", err)
			}
		})
	}
}

// TestSecretStoreUnsupportedPlatformFallsBack pins the one soft case: no
// keychain backend at all still prints env-var instructions and exits 0.
func TestSecretStoreUnsupportedPlatformFallsBack(t *testing.T) {
	cases := []struct {
		name   string
		newCmd func() *cobra.Command
		envVar string
	}{
		{"use-api-key", newAuthUseAPIKeyCmd, auth.EnvAPIKeyVar},
		{"use-ads-developer-token", newAuthUseAdsDeveloperTokenCmd, auth.EnvGoogleAdsDeveloperToken},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			keyringlib.MockInitWithError(keyringlib.ErrUnsupportedPlatform)
			t.Cleanup(keyringlib.MockInit)

			out, err := runSecretStoreCmd(t, tc.newCmd, "secret-value-abc")
			if err != nil {
				t.Fatalf("unsupported platform must fall back, got err=%v; out=%q", err, out)
			}
			if !strings.Contains(out, tc.envVar) {
				t.Errorf("fallback output does not name %s: %q", tc.envVar, out)
			}
			if strings.Contains(out, "secret-value-abc") {
				t.Errorf("output leaked the secret: %q", out)
			}
		})
	}
}

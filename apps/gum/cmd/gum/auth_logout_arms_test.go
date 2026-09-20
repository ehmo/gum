package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	keyringlib "github.com/zalando/go-keyring"

	"github.com/ehmo/gum/internal/auth"
)

// runLogoutArms runs `gum logout` through the root command and returns its
// stdout plus the RunE error.
func runLogoutArms(t *testing.T, args ...string) (string, error) {
	t.Helper()
	root := newRootCmd()
	var out, errBuf bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errBuf)
	root.SetIn(strings.NewReader(""))
	root.SetArgs(append([]string{"logout"}, args...))
	err := root.Execute()
	return out.String(), err
}

// TestLogoutSurfacesAKeyringFailure pins the auth.Logout error arm. An
// unreachable keychain must not read as "already logged out": the caller
// would believe the credential was gone while it is still stored.
func TestLogoutSurfacesAKeyringFailure(t *testing.T) {
	stubRevokeEndpoint(t)
	keyringlib.MockInitWithError(errors.New("keychain locked"))
	t.Cleanup(keyringlib.MockInit)

	out, err := runLogoutArms(t)
	if err == nil {
		t.Fatalf("logout against an unreachable keychain succeeded; stdout %q", out)
	}
	if !strings.Contains(err.Error(), "gum logout:") {
		t.Errorf("err=%q; want the 'gum logout:' wrap", err)
	}
}

// TestLogoutForgetClientWithoutOneSaysSo pins the ForgetClientSkipped case.
// --forget-client with nothing registered used to report a plain "nothing to
// clear", which does not tell the operator the flag was a no-op.
func TestLogoutForgetClientWithoutOneSaysSo(t *testing.T) {
	stubRevokeEndpoint(t)
	keyringlib.MockInit()

	out, err := runLogoutArms(t, "--forget-client")
	if err != nil {
		t.Fatalf("gum logout --forget-client: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("decode logout output %q: %v", out, err)
	}
	if payload["forget_client_skipped"] != true {
		t.Errorf("forget_client_skipped = %v; want true", payload["forget_client_skipped"])
	}
	next, _ := payload["next"].(string)
	if !strings.Contains(next, "no BYO OAuth client was registered to forget") {
		t.Errorf("next = %q; want the skipped-forget explanation", next)
	}
}

// TestLogoutForgetClientAlsoReportsAVaultPurge pins the one case where a
// vault purge and a skipped --forget-client coincide: legacy gum_oauth
// entries with no BYO client registered. Both facts have to reach the
// operator, not just the purge.
func TestLogoutForgetClientAlsoReportsAVaultPurge(t *testing.T) {
	stubRevokeEndpoint(t)
	keyringlib.MockInit()
	kb := auth.NewOSKeyring()

	const legacyKey = "gum.gum_oauth.abc123.deadbeef"
	if err := kb.Set(legacyKey, "rt-legacy"); err != nil {
		t.Fatalf("seed legacy vault entry: %v", err)
	}
	if err := auth.NewCredentialVault(kb).TrackGumOAuthKey(legacyKey); err != nil {
		t.Fatalf("TrackGumOAuthKey: %v", err)
	}

	out, err := runLogoutArms(t, "--forget-client")
	if err != nil {
		t.Fatalf("gum logout --forget-client: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("decode logout output %q: %v", out, err)
	}
	if payload["gum_oauth_vault_cleared"] != true {
		t.Errorf("gum_oauth_vault_cleared = %v; want true", payload["gum_oauth_vault_cleared"])
	}
	next, _ := payload["next"].(string)
	if !strings.Contains(next, "cleared stored gum_oauth refresh tokens") {
		t.Errorf("next = %q; want the vault-purge line", next)
	}
	if !strings.Contains(next, "no BYO OAuth client was registered to forget") {
		t.Errorf("next = %q; want the skipped-forget clause too", next)
	}
}

package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	keyringlib "github.com/zalando/go-keyring"

	"github.com/ehmo/gum/internal/auth"
	"github.com/ehmo/gum/internal/dispatch"
	"github.com/ehmo/gum/internal/plugins"
)

// TestMergePluginRowsSkipsANilManifest pins the nil guard. Host.List builds
// the manifest slice from disk, so one unreadable install must not drop the
// rest of the listing.
func TestMergePluginRowsSkipsANilManifest(t *testing.T) {
	rows := mergePluginRows(
		[]*plugins.Manifest{nil, {PluginID: "acme-plug", Version: "1.0.0"}},
		nil,
	)
	if len(rows) != 1 {
		t.Fatalf("rows = %d; want only the real manifest", len(rows))
	}
	if rows[0].id != "acme-plug" {
		t.Errorf("rows[0].id = %q; want acme-plug", rows[0].id)
	}
}

// TestJITLoginDeclinesAnUnknownOp pins the jitByoScopes guard. A JIT login
// only helps a byo_oauth variant, so an op the catalog does not carry must
// leave the original structured error untouched.
func TestJITLoginDeclinesAnUnknownOp(t *testing.T) {
	derr := dispatch.NewStructuredError(dispatch.ErrCodeAuthRequired, "no grant")
	cmd := &cobra.Command{Use: "call"}
	cmd.SetContext(context.Background())
	cmd.SetIn(strings.NewReader(""))
	if maybeJITLogin(cmd, "no.such.op", "", derr) {
		t.Error("maybeJITLogin fired for an op the catalog does not carry")
	}
}

// TestValidAdsConstantRejectsABareResourcePrefix pins the empty-id guard.
// "geoTargetConstants/" carries the prefix but no id, so accepting it would
// send a resource name with no target to the Ads API.
func TestValidAdsConstantRejectsABareResourcePrefix(t *testing.T) {
	if validAdsConstant("geoTargetConstants/", "geoTargetConstants") {
		t.Error("validAdsConstant accepted a prefix with no id")
	}
	if validAdsConstant("   ", "geoTargetConstants") {
		t.Error("validAdsConstant accepted whitespace as an id")
	}
}

// TestDoctorAuthReportsARegisteredClient pins the keyring-decided arm of
// doctorAuth. With a BYO client stored, the keychain probe answers on its
// own and doctor must not fall through to the config-file heuristic.
func TestDoctorAuthReportsARegisteredClient(t *testing.T) {
	keyringlib.MockInit()
	isolatedHome(t)
	kb := auth.NewOSKeyring()
	if err := auth.StoreByoClient(kb, "default", auth.ByoClient{ClientID: "cid"}); err != nil {
		t.Fatalf("StoreByoClient: %v", err)
	}

	cmd := newRootCmd()
	cmd.SetContext(context.Background())
	got := doctorAuth(cmd)
	if got.Name != "auth" {
		t.Fatalf("check name = %q; want auth", got.Name)
	}
	if !strings.Contains(got.Summary, "BYO OAuth") {
		t.Errorf("summary = %q; want the keychain-sourced BYO verdict", got.Summary)
	}
}

// TestConfigSetSurfacesALoadFailure pins the config.Load arm in `config
// set`. Writing into a config gum cannot parse would drop every existing
// key, so the command has to stop instead.
func TestConfigSetSurfacesALoadFailure(t *testing.T) {
	configRoot := withTempConfigRootCLI(t)
	writeBrokenConfig(t, configRoot, "default")

	out, err := runCLI(t, "config", "set", "output.default_format=json")
	if err == nil {
		t.Fatalf("config set over a broken config succeeded; out %q", out)
	}
	if !strings.Contains(err.Error(), "expected key = value") {
		t.Errorf("err=%q; want the config parse failure", err)
	}
}

// TestCacheProfileDirRejectsABadProfile pins the resolveProfileName arm in
// cacheProfileDir. The real root rejects the name in PersistentPreRunE, so
// the guard is reached only by a root without that hook.
func TestCacheProfileDirRejectsABadProfile(t *testing.T) {
	root := bareRootWithProfile("bad/name")
	root.PersistentFlags().Lookup("profile").Changed = true
	sub := newCacheCmd()
	root.AddCommand(sub)

	if _, err := cacheProfileDir(sub); err == nil {
		t.Fatal("cacheProfileDir accepted a bad profile")
	}
}

// TestMCPStdioRejectsABadProfileDirectly pins the SetProfile arm. The CLI
// cannot reach it (the root validates --profile first), so the server-side
// guard is what keeps a bad name out of a programmatic caller's session.
func TestMCPStdioRejectsABadProfileDirectly(t *testing.T) {
	keyringlib.MockInit()
	isolatedHome(t)

	err := runMCPStdio(context.Background(), "bad/name")
	if err == nil {
		t.Fatal("runMCPStdio accepted a bad profile")
	}
	if !strings.Contains(err.Error(), "bad/name") {
		t.Errorf("err=%q does not name the rejected profile", err)
	}
}

// TestMCPCmdRunsTheStdioServer pins the RunE body. Every other mcp test
// calls runMCPStdio directly, so the flag-to-server wiring itself had no
// coverage. A pre-cancelled context makes the server return at once.
func TestMCPCmdRunsTheStdioServer(t *testing.T) {
	keyringlib.MockInit()
	isolatedHome(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	root := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetIn(strings.NewReader(""))
	root.SetArgs([]string{"mcp", "--stdio"})

	done := make(chan error, 1)
	go func() { done <- root.ExecuteContext(ctx) }()
	select {
	case err := <-done:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatalf("gum mcp --stdio: %v (out %q)", err, out.String())
		}
	case <-t.Context().Done():
		t.Fatal("gum mcp --stdio did not return after its context was cancelled")
	}
}

// TestSetupSurfacesAnInstallFailure pins the wet-run arm. The dry run only
// plans paths, so a home directory gum can read but not write reaches
// agents.Install and fails there, after the plan has already printed.
func TestSetupSurfacesAnInstallFailure(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory write permissions")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := os.Chmod(home, 0o500); err != nil {
		t.Fatalf("chmod %s: %v", home, err)
	}
	t.Cleanup(func() { _ = os.Chmod(home, 0o700) })

	out, err := runCLI(t, "setup", "--target=claude", "--scope=user",
		"--features=skills", "--yes", "--format=json")
	if err == nil {
		t.Fatalf("setup into a read-only home succeeded; out %q", out)
	}
}

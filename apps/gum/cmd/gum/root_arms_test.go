package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	keyringlib "github.com/zalando/go-keyring"

	"github.com/ehmo/gum/internal/adapters/googleads"
	"github.com/ehmo/gum/internal/auth"
	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/dispatch"
	"github.com/ehmo/gum/internal/output/gain"
)

// isolatedHome points every per-profile lookup at fresh temp dirs so a
// developer's real ~/.config/gum and ~/.local/share/gum cannot decide a test.
func isolatedHome(t *testing.T) (dataHome string) {
	t.Helper()
	dataHome = t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", dataHome)
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	return dataHome
}

// bareRootWithProfile is a root command carrying only the --profile persistent
// flag. It has a nil Context, which is what makes it the seam for the
// context.Background() fallback in promotePendingPlugins.
func bareRootWithProfile(value string) *cobra.Command {
	root := &cobra.Command{Use: "bare"}
	root.PersistentFlags().String("profile", value, "")
	return root
}

// TestApplyLoggingFlagsRejectsAnUnknownLevel drives the error arm through
// PersistentPreRunE rather than the helper, so the hook's own propagation is
// what the test pins.
func TestApplyLoggingFlagsRejectsAnUnknownLevel(t *testing.T) {
	isolatedHome(t)
	root := newRootCmd()
	// ParseFlags is what merges the persistent set into Flags(); without it the
	// hook looks the flag up on an empty local set and never sees the value.
	if err := root.ParseFlags([]string{"--log-level", "bogus"}); err != nil {
		t.Fatalf("parse flags: %v", err)
	}

	err := root.PersistentPreRunE(root, nil)
	if err == nil {
		t.Fatal("PersistentPreRunE err=nil; want the invalid --log-level rejection")
	}
	if !strings.Contains(err.Error(), "bogus") {
		t.Errorf("err=%v; want it to name the rejected value", err)
	}
}

// TestApplyProfileSelectionGuards covers the two arms that let the hook run
// against a command tree that has no --profile flag at all.
func TestApplyProfileSelectionGuards(t *testing.T) {
	if err := applyProfileSelection(nil); err != nil {
		t.Errorf("applyProfileSelection(nil)=%v; want nil", err)
	}
	if err := applyProfileSelection(&cobra.Command{Use: "noflag"}); err != nil {
		t.Errorf("applyProfileSelection(no --profile)=%v; want nil", err)
	}
}

// TestPromotePendingPluginsGuards covers every early return that keeps the
// startup activation write best-effort.
func TestPromotePendingPluginsGuards(t *testing.T) {
	isolatedHome(t)

	promotePendingPlugins(nil)
	promotePendingPlugins(&cobra.Command{Use: "noflag"})
	promotePendingPlugins(bareRootWithProfile("bad/name"))

	// HOME and both XDG vars cleared makes DataDir fail for a name that parses.
	t.Setenv("HOME", "")
	t.Setenv("XDG_DATA_HOME", "")
	promotePendingPlugins(bareRootWithProfile("default"))
}

// TestPromotePendingPluginsWithoutAContext pins the context.Background()
// fallback: a command that never ran carries a nil Context, and the promotion
// still has to reach the registry.
func TestPromotePendingPluginsWithoutAContext(t *testing.T) {
	isolatedHome(t)
	root := bareRootWithProfile("default")
	if root.Context() != nil {
		t.Fatal("a command that never executed must carry a nil Context")
	}

	promotePendingPlugins(root)
}

// TestPluginStarterSurfacesProfileDirFailures pins the two error arms of the
// production plugin starter closure. Both run before any subprocess spawn, so
// the adapter reports the failure instead of a bogus SERVICE_DOWN on a plugin
// it never looked for.
func TestPluginStarterSurfacesProfileDirFailures(t *testing.T) {
	rv := &dispatch.ResolvedVariant{
		OpID: "flights.search",
		Variant: &catalog.Variant{
			VariantID: "flights.search.v1",
			Binding: &catalog.Binding{
				AdapterKey: "plugin.mcp",
				PluginName: "fli",
				ToolName:   "search",
			},
		},
		AdapterKey: "plugin.mcp",
	}
	inv := &dispatch.Invocation{OpID: "flights.search"}

	t.Run("unparseable profile", func(t *testing.T) {
		isolatedHome(t)
		adapterMap, _ := defaultAdapters("bad/name")
		_, err := adapterMap["plugin.mcp"].Execute(context.Background(), inv, rv, nil)
		if err == nil {
			t.Fatal("Execute err=nil; want the profile-name rejection")
		}
	})

	t.Run("unwritable profile dir", func(t *testing.T) {
		parent := t.TempDir()
		t.Setenv("HOME", t.TempDir())
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())
		t.Setenv("XDG_DATA_HOME", filepath.Join(parent, "data"))
		t.Cleanup(func() { _ = os.Chmod(parent, 0o700) })
		if err := os.Chmod(parent, 0o500); err != nil {
			t.Fatalf("chmod %s: %v", parent, err)
		}

		adapterMap, _ := defaultAdapters("default")
		_, err := adapterMap["plugin.mcp"].Execute(context.Background(), inv, rv, nil)
		if err == nil {
			t.Fatal("Execute err=nil; want the registry mkdir failure")
		}
	})
}

// TestGoogleAdsDevTokenReadsTheProfileKeyring pins the developer-token closure
// the Google Ads adapter is built with. keyringlib.MockInit swaps the OS
// backend for an in-process map, so this never touches a real keychain.
func TestGoogleAdsDevTokenReadsTheProfileKeyring(t *testing.T) {
	isolatedHome(t)
	keyringlib.MockInit()
	t.Setenv(auth.EnvGoogleAdsDeveloperToken, "")

	if err := auth.StoreDeveloperToken(auth.NewOSKeyring(), "work", "tok-work"); err != nil {
		t.Fatalf("StoreDeveloperToken: %v", err)
	}

	adapterMap, _ := defaultAdapters("work")
	gads, ok := adapterMap["googleads.search"].(*googleads.Adapter)
	if !ok {
		t.Fatalf("googleads.search adapter type=%T; want *googleads.Adapter", adapterMap["googleads.search"])
	}
	if got := gads.DevToken(); got != "tok-work" {
		t.Errorf("DevToken()=%q; want the token stored under the work profile", got)
	}
}

// TestProfileHierarchyLookupArms covers the resolver wrapper's three outcomes:
// a project-local hit, a miss that stays quiet, and a malformed file that is
// dropped with a warning rather than failing the call.
func TestProfileHierarchyLookupArms(t *testing.T) {
	t.Run("project-local hit", func(t *testing.T) {
		isolatedHome(t)
		root := t.TempDir()
		writeArmsProfile(t, root, "tight.toml", "format = \"json\"\nlimit = 7\n")
		t.Chdir(root)

		p, ok := profileHierarchyLookup("tight")
		if !ok {
			t.Fatal("profileHierarchyLookup ok=false; want the project-local profile")
		}
		if p.Limit != 7 {
			t.Errorf("limit=%d; want 7", p.Limit)
		}
	})

	t.Run("missing name is quiet", func(t *testing.T) {
		isolatedHome(t)
		t.Chdir(t.TempDir())

		if _, ok := profileHierarchyLookup("nosuchprofile"); ok {
			t.Error("profileHierarchyLookup ok=true; want a miss")
		}
	})

	t.Run("malformed file is dropped", func(t *testing.T) {
		isolatedHome(t)
		root := t.TempDir()
		writeArmsProfile(t, root, "broken.toml", "format = [\n")
		t.Chdir(root)

		if _, ok := profileHierarchyLookup("broken"); ok {
			t.Error("profileHierarchyLookup ok=true; want the malformed file dropped")
		}
	})
}

// TestProfileOverrideBindingsArms covers the merged [override_bindings] table
// and the arm that drops the whole table when a file will not parse.
func TestProfileOverrideBindingsArms(t *testing.T) {
	t.Run("bindings load", func(t *testing.T) {
		isolatedHome(t)
		root := t.TempDir()
		writeArmsProfile(t, root, "bind.toml", "[output_profiles.\"tight\"]\nformat = \"json\"\n\n[override_bindings]\n\"gmail.messages.list\" = \"tight\"\n")
		t.Chdir(root)

		got := profileOverrideBindings()
		if got["gmail.messages.list"] != "tight" {
			t.Errorf("bindings=%v; want gmail.messages.list bound to tight", got)
		}
	})

	t.Run("malformed file yields no bindings", func(t *testing.T) {
		isolatedHome(t)
		root := t.TempDir()
		writeArmsProfile(t, root, "broken.toml", "[override_bindings\n")
		t.Chdir(root)

		if got := profileOverrideBindings(); got != nil {
			t.Errorf("bindings=%v; want nil after the load failure", got)
		}
	})
}

// TestDispatcherWarnsWhenTheGainLedgerCannotOpen pins the best-effort arm:
// accounting that cannot start must not stop the CLI from dispatching.
func TestDispatcherWarnsWhenTheGainLedgerCannotOpen(t *testing.T) {
	dataHome := isolatedHome(t)
	// A directory where the ledger file belongs makes every open fail.
	ledger := filepath.Join(dataHome, "gum", "default", gain.LedgerFileName)
	if err := os.MkdirAll(ledger, 0o700); err != nil {
		t.Fatalf("mkdir %s: %v", ledger, err)
	}

	disp := newDefaultCodeDispatcherForProfile("default")
	if disp == nil {
		t.Fatal("dispatcher=nil; a ledger failure must not stop construction")
	}
}

// TestResolveAuditRuntimeConfigFallsBackOnABadProfile pins the arm that keeps
// the audit defaults when the per-profile config cannot be loaded.
func TestResolveAuditRuntimeConfigFallsBackOnABadProfile(t *testing.T) {
	isolatedHome(t)
	got := resolveAuditRuntimeConfig("bad/name")
	if got != defaultAuditRuntimeConfig() {
		t.Errorf("config=%+v; want the defaults %+v", got, defaultAuditRuntimeConfig())
	}
}

// TestProfileAuditDirRejectsABadName pins the parse guard in front of the
// audit directory lookup.
func TestProfileAuditDirRejectsABadName(t *testing.T) {
	isolatedHome(t)
	if _, err := profileAuditDir("bad/name"); err == nil {
		t.Fatal("profileAuditDir err=nil; want the profile-name rejection")
	}
}

// writeArmsProfile writes one file into <parent>/.gum/profiles.
func writeArmsProfile(t *testing.T, parent, name, body string) {
	t.Helper()
	dir := filepath.Join(parent, ".gum", "profiles")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

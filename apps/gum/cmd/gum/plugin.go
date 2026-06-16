package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/ehmo/gum/internal/auditlog"
	"github.com/ehmo/gum/internal/auth"
	"github.com/ehmo/gum/internal/config"
	"github.com/ehmo/gum/internal/plugins"
	"github.com/ehmo/gum/internal/plugins/registry"
	profilepkg "github.com/ehmo/gum/internal/profile"
	"github.com/spf13/cobra"
)

// profileIsDev returns true when the active profile's config.toml has
// profile.is_dev=true. Spec §5.1: only dev profiles honor the
// --dev-allow-namespace-conflict flag. Load errors and unset values both
// surface as false so production profiles default to the strict gate.
func profileIsDev(profile string) bool {
	c, _, err := config.Load(profile)
	if err != nil {
		return false
	}
	v, ok := c.Get("profile.is_dev")
	if !ok {
		return false
	}
	return strings.EqualFold(v, "true") || v == "1"
}

// PluginsHostInterface is a minimal interface over *plugins.Host so tests can
// inject a mock without importing a subprocess-capable host.
// Exported so external test packages (package main_test) can implement it.
type PluginsHostInterface interface {
	Install(ctx context.Context, source string) (string, error)
	InstallWithRegistry(ctx context.Context, source string, opts plugins.InstallOptions) (string, error)
	Remove(ctx context.Context, pluginID string) error
	List() ([]*plugins.Manifest, error)
	Start(ctx context.Context, pluginID string) (*plugins.Plugin, error)
}

// pluginsHostInterface is an unexported alias kept for internal use.
type pluginsHostInterface = PluginsHostInterface

// PluginRegistryFactory builds a registry for the given profile dir; tests
// override to inject a tempdir. Nil falls back to the real
// registry.New constructor.
type PluginRegistryFactory func(profileDir string) *registry.Registry

// DispatchPluginCommand parses args (the slice after "plugin") and dispatches
// to the appropriate host method. Supported subcommands:
//
//	install <local-dir>         — install a plugin from a local dir
//	list                        — list installed plugins
//	remove <id>                 — remove a plugin by ID
//	run <id> <tool> <args-json> — call a tool on a running plugin
//	reload <id>                 — clear quarantine and verify the plugin can spawn
//	unquarantine <id>           — clear quarantine without restart
//
// Returns a human-readable result string on success, or an error.
// Exported so cmd/gum tests can call it without subprocess wiring.
func DispatchPluginCommand(args []string, host pluginsHostInterface) (string, error) {
	return DispatchPluginCommandWithRegistry(args, host, "", nil)
}

// PluginInstallOptions carries the CLI-side flags consumed by the
// install subcommand. ProfileIsDev is read from the profile config (key
// `profile.is_dev`) and AllowNamespaceConflict from the
// `--dev-allow-namespace-conflict` flag. Both must be true to bypass
// PLUGIN_NAMESPACE_CONFLICT per spec §5.1.
type PluginInstallOptions struct {
	ProfileIsDev           bool
	AllowNamespaceConflict bool
}

// DispatchPluginCommandWithRegistry is the registry-aware variant used by the
// install/reload/unquarantine subcommands. profileDir resolves
// `<data home>/gum/<profile>` and is required: install writes the three
// registry files via the spec §8.7 atomic protocol, and reload/unquarantine
// read plugin-state.json from the same profile.
func DispatchPluginCommandWithRegistry(args []string, host pluginsHostInterface, profileDir string, regFactory PluginRegistryFactory) (string, error) {
	return DispatchPluginCommandWithOptions(args, host, profileDir, regFactory, PluginInstallOptions{})
}

// PluginSetupOptions carries the injectable dependencies for the
// `gum plugin setup <name>` subcommand. Tests supply stub implementations
// for Keyring, In, Out, and RunCanary so no real subprocess or OS keychain
// is touched.
type PluginSetupOptions struct {
	// InstallRoot overrides the default plugin install directory.
	// Tests set this to the tempdir where the fake plugin manifest was written.
	InstallRoot string
	// Keyring overrides the OS keychain backend (nil → auth.NewOSKeyring()).
	Keyring auth.KeyringBackend
	// In is the reader for credential input (nil → os.Stdin).
	In interface{ Read([]byte) (int, error) }
	// Out is the writer for user-facing prompts (nil → os.Stdout).
	Out interface{ Write([]byte) (int, error) }
	// RunCanary overrides the live canary function (nil → real canary via Start).
	RunCanary func(ctx context.Context, pluginID string) error
}

// DispatchPluginCommandWithOptions extends DispatchPluginCommandWithRegistry
// with the install-side flags (dev-profile + namespace-conflict override).
// Existing callers that don't set the flags keep the strict (non-dev) gate.
func DispatchPluginCommandWithOptions(args []string, host pluginsHostInterface, profileDir string, regFactory PluginRegistryFactory, installOpts PluginInstallOptions) (string, error) {
	return DispatchPluginCommandFull(args, host, profileDir, regFactory, installOpts, PluginSetupOptions{})
}

// DispatchPluginCommandFull is the fully injectable variant that additionally
// accepts PluginSetupOptions for the setup subcommand.
func DispatchPluginCommandFull(args []string, host pluginsHostInterface, profileDir string, regFactory PluginRegistryFactory, installOpts PluginInstallOptions, setupOpts PluginSetupOptions) (string, error) {
	if len(args) == 0 {
		return "", fmt.Errorf("gum plugin: missing subcommand; expected install|list|remove|run|setup")
	}

	// Make plugin operations (install handshake, subprocess canary, etc.)
	// interruptible: a Ctrl-C / SIGTERM cancels the in-flight op instead of
	// running it to completion on a non-cancellable Background context
	// (review gum-yvam).
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	switch args[0] {
	case "install":
		if len(args) < 2 {
			return "", fmt.Errorf("gum plugin install: missing <local-dir> argument")
		}
		if profileDir != "" {
			reg, err := openRegistry(profileDir, regFactory)
			if err != nil {
				return "", err
			}
			pluginID, err := host.InstallWithRegistry(ctx, args[1], plugins.InstallOptions{
				Registry: reg,
				Namespace: plugins.NamespaceOptions{
					ProfileIsDev:          installOpts.ProfileIsDev,
					AllowConflictOverride: installOpts.AllowNamespaceConflict,
				},
			})
			if err != nil {
				return "", err
			}
			return "installed " + pluginID + "\n" + pluginSandboxAdvisory, nil
		}
		pluginID, err := host.Install(ctx, args[1])
		if err != nil {
			return "", err
		}
		return "installed " + pluginID + "\n" + pluginSandboxAdvisory, nil

	case "list":
		manifests, err := host.List()
		if err != nil {
			return "", err
		}
		if len(manifests) == 0 {
			return "", nil
		}
		var sb strings.Builder
		for _, m := range manifests {
			fmt.Fprintf(&sb, "%s\t%s\t%s\n", m.PluginID, m.Version, m.Name)
		}
		return sb.String(), nil

	case "remove":
		if len(args) < 2 {
			return "", fmt.Errorf("gum plugin remove: missing <id> argument")
		}
		if err := host.Remove(ctx, args[1]); err != nil {
			return "", err
		}
		return fmt.Sprintf("removed %s\n", args[1]), nil

	case "run":
		if len(args) < 3 {
			return "", fmt.Errorf("gum plugin run: usage: run <id> <tool> [args-json]")
		}
		pluginID := args[1]
		toolName := args[2]

		var rawArgs any = map[string]any{}
		if len(args) >= 4 {
			if err := json.Unmarshal([]byte(args[3]), &rawArgs); err != nil {
				return "", fmt.Errorf("gum plugin run: invalid args JSON: %w", err)
			}
		}

		// Route through the Supervisor when a profile registry exists so a
		// permanently-quarantined plugin is refused (gum-g7xr) — host.Start
		// alone has no quarantine awareness. With no profile there is no
		// registry and therefore no quarantine state to enforce.
		var plug *plugins.Plugin
		var err error
		if profileDir != "" {
			reg, rerr := openRegistry(profileDir, regFactory)
			if rerr != nil {
				return "", rerr
			}
			plug, err = plugins.NewSupervisor(reg, host.Start, nil).Start(ctx, pluginID)
		} else {
			plug, err = host.Start(ctx, pluginID)
		}
		if err != nil {
			return "", err
		}
		defer plug.Stop(ctx) //nolint:errcheck

		result, err := plug.CallTool(ctx, toolName, rawArgs)
		if err != nil {
			return "", err
		}
		return string(result), nil

	case "unquarantine":
		if len(args) < 2 {
			return "", fmt.Errorf("gum plugin unquarantine: missing <id> argument")
		}
		reg, err := openRegistry(profileDir, regFactory)
		if err != nil {
			return "", err
		}
		if err := plugins.ClearQuarantine(ctx, reg, args[1]); err != nil {
			return "", fmt.Errorf("gum plugin unquarantine: %w", err)
		}
		return fmt.Sprintf("unquarantined %s\n", args[1]), nil

	case "reload":
		if len(args) < 2 {
			return "", fmt.Errorf("gum plugin reload: missing <id> argument")
		}
		reg, err := openRegistry(profileDir, regFactory)
		if err != nil {
			return "", err
		}
		pluginID := args[1]
		if err := plugins.ClearQuarantine(ctx, reg, pluginID); err != nil {
			return "", fmt.Errorf("gum plugin reload: clear quarantine: %w", err)
		}
		// Passive canary: spawn one ephemeral subprocess to verify the plugin
		// initialises, then stop it. A spawn failure re-quarantines via the
		// Supervisor so the operator sees the same outcome whether the failure
		// surfaces during reload or during the next live invocation.
		sup := plugins.NewSupervisor(reg, host.Start, nil)
		plug, startErr := sup.Start(ctx, pluginID)
		if startErr != nil {
			return "", fmt.Errorf("gum plugin reload: passive canary: %w", startErr)
		}
		_ = plug.Stop(ctx)
		return fmt.Sprintf("reloaded %s\n", pluginID), nil

	case "setup":
		if len(args) < 2 {
			return "", fmt.Errorf("gum plugin setup: missing <name> argument")
		}
		pluginName := args[1]
		reg, err := openRegistry(profileDir, regFactory)
		if err != nil {
			return "", err
		}
		profile := "default"
		if profileDir != "" {
			// Derive profile name from the last path component of profileDir.
			profile = filepath.Base(profileDir)
		}

		var inReader io.Reader = os.Stdin
		if setupOpts.In != nil {
			inReader = setupOpts.In
		}
		var outWriter io.Writer = os.Stdout
		if setupOpts.Out != nil {
			outWriter = setupOpts.Out
		}

		sopts := plugins.SetupOptions{
			Registry:    reg,
			Profile:     profile,
			InstallRoot: setupOpts.InstallRoot,
			Keyring:     setupOpts.Keyring,
			In:          inReader,
			Out:         outWriter,
			RunCanary:   setupOpts.RunCanary,
		}
		if sopts.RunCanary == nil {
			// Default: use the real host canary (Start + Stop).
			sopts.RunCanary = func(cctx context.Context, pid string) error {
				plug, startErr := host.Start(cctx, pid)
				if startErr != nil {
					return startErr
				}
				return plug.Stop(cctx)
			}
		}

		if err := plugins.SetupCredentials(ctx, pluginName, sopts); err != nil {
			return "", err
		}
		return fmt.Sprintf("plugin %q configured and activated\n", pluginName), nil

	case "transfer-namespace":
		return dispatchTransferNamespace(ctx, args[1:], profileDir, regFactory)

	default:
		return "", fmt.Errorf("gum plugin: unknown subcommand %q", args[0])
	}
}

// dispatchTransferNamespace handles `gum plugin transfer-namespace <prefix>
// {--new-owner <name>|--release} --yes` per spec §5.1.3 line 526. Flags are
// position-independent (any order after the prefix); --yes is mandatory in
// both modes to make this a non-interactive override.
func dispatchTransferNamespace(ctx context.Context, args []string, profileDir string, regFactory PluginRegistryFactory) (string, error) {
	if len(args) == 0 {
		return "", errors.New("gum plugin transfer-namespace: missing <prefix> argument; usage: transfer-namespace <prefix> {--new-owner <name>|--release} --yes")
	}
	prefix := args[0]
	var (
		newOwner string
		release  bool
		yes      bool
	)
	i := 1
	for i < len(args) {
		switch args[i] {
		case "--new-owner":
			if i+1 >= len(args) {
				return "", errors.New("gum plugin transfer-namespace: --new-owner requires a value")
			}
			newOwner = args[i+1]
			i += 2
		case "--release":
			release = true
			i++
		case "--yes":
			yes = true
			i++
		default:
			return "", fmt.Errorf("gum plugin transfer-namespace: unknown flag %q", args[i])
		}
	}
	if release && newOwner != "" {
		return "", errors.New("gum plugin transfer-namespace: --release and --new-owner are mutually exclusive")
	}
	if !release && newOwner == "" {
		return "", errors.New("gum plugin transfer-namespace: must specify --new-owner <name> or --release")
	}
	if !yes {
		return "", errors.New("gum plugin transfer-namespace: --yes is required (non-interactive consent acknowledgment, spec §5.1.3 line 526)")
	}

	reg, err := openRegistry(profileDir, regFactory)
	if err != nil {
		return "", err
	}

	opts := plugins.TransferOptions{}
	var oldOwner string
	mutate := func(files *registry.Files) error {
		var mutErr error
		if release {
			oldOwner, mutErr = plugins.ReleaseNamespace(files.Lock, prefix, opts)
		} else {
			oldOwner, mutErr = plugins.TransferNamespace(files.Lock, prefix, newOwner, opts)
		}
		return mutErr
	}
	if err := reg.WriteTransaction(ctx, mutate); err != nil {
		return "", fmt.Errorf("gum plugin transfer-namespace: %w", err)
	}

	emitTransferAudit(reg.ProfileDir(), prefix, oldOwner, newOwner, release)

	if release {
		return fmt.Sprintf("released namespace %q (was owned by %q)\n", prefix, oldOwner), nil
	}
	return fmt.Sprintf("transferred namespace %q from %q to %q\n", prefix, oldOwner, newOwner), nil
}

// emitTransferAudit writes one audit.jsonl row recording the §5.1.3 transfer.
// Best-effort: a write failure is logged via auditlog's own audit.broken
// sentinel, not surfaced to the caller, because the on-disk plugins.lock
// transfer is already committed and the operator's blast radius for an audit
// gap is smaller than rolling back a successful transfer.
func emitTransferAudit(profileDir, prefix, oldOwner, newOwner string, release bool) {
	if profileDir == "" {
		return
	}
	w, err := auditlog.New(profileDir)
	if err != nil {
		return
	}
	defer w.Close() //nolint:errcheck
	action := "plugin_namespace_transfer"
	if release {
		action = "plugin_namespace_release"
	}
	w.Append(map[string]any{
		"event_type": action,
		"prefix":     prefix,
		"old_owner":  oldOwner,
		"new_owner":  newOwner,
		"released":   release,
		"profile":    filepath.Base(profileDir),
		"emitted_at": time.Now().UTC().Format(time.RFC3339),
	})
}

// openRegistry resolves the registry for the active profile. The factory hook
// lets tests substitute a tempdir-backed registry.
func openRegistry(profileDir string, factory PluginRegistryFactory) (*registry.Registry, error) {
	if profileDir == "" {
		return nil, errors.New("gum plugin: profile dir unresolved (internal error: pass --profile or set XDG_DATA_HOME)")
	}
	if factory != nil {
		return factory(profileDir), nil
	}
	if err := os.MkdirAll(profileDir, 0o700); err != nil {
		return nil, fmt.Errorf("gum plugin: mkdir profile dir: %w", err)
	}
	return registry.New(profileDir), nil
}

// resolveProfileDir returns `<data home>/gum/<profile>` honouring XDG_DATA_HOME.
func resolveProfileDir(profile string) (string, error) {
	name, err := profilepkg.Parse(profile)
	if err != nil {
		return "", err
	}
	dir, err := name.DataDir()
	if err != nil {
		return "", fmt.Errorf("resolve profile dir: %w", err)
	}
	return dir, nil
}

// defaultPluginsHostInterface constructs the real *plugins.Host using the
// default install root. Used by main when not under test.
func defaultPluginsHost() pluginsHostInterface {
	return plugins.NewHost(plugins.HostConfig{})
}

// pluginCommandHelp is a concise overview for `gum plugin --help`. The
// per-subcommand list is rendered by cobra's auto-generated "Available
// Commands" section, so this text only covers the trust model rather than
// re-listing the subcommands (which previously produced two conflicting usage
// blocks — review gum-s985).
const pluginCommandHelp = `Manage gum plugins: install, list, run, and curate third-party subprocess
plugins. Installing a plugin launches an untrusted subprocess, so 'install'
requires --yes to acknowledge that trust boundary. See the subcommands below.`

// pluginSandboxAdvisory is appended to install output so operators understand
// the process trust boundary and platform-specific sandbox behavior.
const pluginSandboxAdvisory = "note: this plugin runs as a subprocess. " +
	"GUM OS-enforces network=false and fs_write_dir on macOS and Linux; " +
	"unsupported platforms fail closed until their sandbox backends exist. " +
	"Only install plugins you trust.\n"

// init-time compile check: *plugins.Host satisfies pluginsHostInterface.
var _ pluginsHostInterface = (*plugins.Host)(nil)

// newPluginCmd wraps DispatchPluginCommand in a cobra subtree so help, --help,
// and unknown subcommands behave consistently with the rest of the CLI. The
// underlying DispatchPluginCommand API remains the authoritative implementation
// and is exercised directly by plugin_subcommands_test.go.
func newPluginCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "plugin",
		Short: "Manage gum plugins",
		Long:  pluginCommandHelp,
	}
	parentHelpOnly(cmd)
	cmd.AddCommand(
		newPluginInstallCmd(),
		newPluginListCmd(),
		newPluginRemoveCmd(),
		newPluginRunCmd(),
		newPluginSetupCmd(),
		newPluginReloadCmd(),
		newPluginUnquarantineCmd(),
		newPluginTransferNamespaceCmd(),
	)
	return cmd
}

// newPluginSetupCmd implements `gum plugin setup <name>` (spec §7/§8.2).
// It prompts for each credential declared in the plugin manifest, stores
// secrets in the OS keychain, and runs the live canary to verify the
// plugin is functional. All user-facing output uses alias/display_name/
// setup_hint — raw env var names are never shown (spec §1414, §1606).
func newPluginSetupCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "setup <name>",
		Short: "Configure plugin credentials and run live canary (§7/§8.2)",
		Long: `Reads the plugin's credential_descriptors from its manifest, prompts for
each missing credential by display_name and setup_hint (never by raw env var
name per spec §1414/§1606), stores secrets in the OS keychain, then runs
'gum canary --plugin=<name> --live' to verify the plugin is functional.

On canary success the plugin state is set to 'active'.
On canary failure the plugin is quarantined with CANARY_FAILED (spec §8.6);
run 'gum plugin reload <name>' after correcting credentials.`,
		SilenceUsage: true,
		Args:         cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			profile := resolveProfileFlag(cmd)
			profileDir, err := resolveProfileDir(profile)
			if err != nil {
				return err
			}
			out, err := DispatchPluginCommandFull(
				append([]string{"setup"}, args...),
				defaultPluginsHost(),
				profileDir,
				nil,
				PluginInstallOptions{},
				PluginSetupOptions{},
			)
			if err != nil {
				return err
			}
			_, _ = fmt.Fprint(cmd.OutOrStdout(), out)
			return nil
		},
	}
}

// newPluginTransferNamespaceCmd surfaces the spec §5.1.3 mutator as a cobra
// subcommand. The prefix is a positional arg; --new-owner and --release
// are mutually exclusive modes and --yes is the mandatory non-interactive
// consent acknowledgment. The heavy lifting (registry write transaction +
// audit emit) lives in dispatchTransferNamespace.
func newPluginTransferNamespaceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "transfer-namespace <prefix>",
		Short: "Transfer or release a third-party namespace owner (§5.1.3)",
		Long: `Updates the namespace_owner binding for <prefix> in the active profile's
plugins.lock, recording the prior owner in transfer_history and emitting an
audit.jsonl row.

Pass either --new-owner <name> to hand the prefix to a different owner string
(matching subsequent installs succeed without --dev-allow-namespace-conflict)
or --release to clear the binding entirely (any owner may then re-bind on the
next install). --yes is mandatory in both modes — this command is destructive
to the namespace lease and has no interactive prompt.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			profile := resolveProfileFlag(cmd)
			profileDir, err := resolveProfileDir(profile)
			if err != nil {
				return err
			}
			newOwner, _ := cmd.Flags().GetString("new-owner")
			release, _ := cmd.Flags().GetBool("release")
			yes, _ := cmd.Flags().GetBool("yes")
			dispatchArgs := []string{"transfer-namespace", args[0]}
			if newOwner != "" {
				dispatchArgs = append(dispatchArgs, "--new-owner", newOwner)
			}
			if release {
				dispatchArgs = append(dispatchArgs, "--release")
			}
			if yes {
				dispatchArgs = append(dispatchArgs, "--yes")
			}
			out, err := DispatchPluginCommandWithRegistry(dispatchArgs, defaultPluginsHost(), profileDir, nil)
			if err != nil {
				return err
			}
			_, _ = fmt.Fprint(cmd.OutOrStdout(), out)
			return nil
		},
	}
	cmd.Flags().String("new-owner", "", "Hand the prefix to this namespace_owner string. Mutually exclusive with --release.")
	cmd.Flags().Bool("release", false, "Clear the existing namespace_owner binding without assigning a new one.")
	cmd.Flags().Bool("yes", false, "Acknowledge the non-interactive consent gate. Required (spec §5.1.3).")
	return cmd
}

func newPluginReloadCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "reload <id>",
		Short: "Clear quarantine, restart the plugin subprocess, and run a passive canary",
		Long:  "Clears any quarantine state for the named plugin, then spawns the subprocess once via the supervisor to act as a passive canary. A spawn failure re-quarantines the plugin per spec §8.6.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			profile := resolveProfileFlag(cmd)
			profileDir, err := resolveProfileDir(profile)
			if err != nil {
				return err
			}
			out, err := DispatchPluginCommandWithRegistry(append([]string{"reload"}, args...), defaultPluginsHost(), profileDir, nil)
			if err != nil {
				return err
			}
			_, _ = fmt.Fprint(cmd.OutOrStdout(), out)
			return nil
		},
	}
}

func newPluginUnquarantineCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "unquarantine <id>",
		Short: "Clear quarantine state without restarting the plugin",
		Long:  "Resets quarantined, retry_count, backoff_step, and next_retry_at in plugin-state.json so the plugin can be invoked on the next call. Use when the operator has independently verified the plugin is healthy and wants to bypass the exponential-backoff window (spec §8.6).",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			profile := resolveProfileFlag(cmd)
			profileDir, err := resolveProfileDir(profile)
			if err != nil {
				return err
			}
			out, err := DispatchPluginCommandWithRegistry(append([]string{"unquarantine"}, args...), defaultPluginsHost(), profileDir, nil)
			if err != nil {
				return err
			}
			_, _ = fmt.Fprint(cmd.OutOrStdout(), out)
			return nil
		},
	}
}

func newPluginInstallCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "install <local-dir>",
		Short: "Install a plugin from a local directory",
		Long: `Installs a plugin via the spec §8.7 atomic protocol: validates the manifest,
runs the spec §5.1 namespace-ownership check against the active profile's
plugins.lock, and writes plugin-catalog.json + plugins.lock + plugin-state.json
through one fsync'd transaction.

Plugins are third-party subprocesses. Installing one trusts its executable and
manifest-declared capabilities; plugin-managed auth means the plugin, not GUM's
OAuth resolver, is responsible for any credentials it requests or forwards.
Review the source and manifest, then pass --yes to acknowledge that trust.

Use --dev-allow-namespace-conflict on a dev profile (set profile.is_dev=true via
gum config set) to install a plugin whose prefix collides with an existing
namespace_owner. Outside a dev profile the flag is ignored and the install
fails with PLUGIN_NAMESPACE_CONFLICT.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			yes, _ := cmd.Flags().GetBool("yes")
			if !yes {
				return errors.New("gum plugin install: --yes is required to acknowledge that plugins run third-party subprocess code and may manage their own credentials")
			}
			profile := resolveProfileFlag(cmd)
			profileDir, err := resolveProfileDir(profile)
			if err != nil {
				return err
			}
			allowConflict, _ := cmd.Flags().GetBool("dev-allow-namespace-conflict")
			out, err := DispatchPluginCommandWithOptions(
				append([]string{"install"}, args...),
				defaultPluginsHost(),
				profileDir,
				nil,
				PluginInstallOptions{
					ProfileIsDev:           profileIsDev(profile),
					AllowNamespaceConflict: allowConflict,
				},
			)
			if err != nil {
				return err
			}
			_, _ = fmt.Fprint(cmd.OutOrStdout(), out)
			return nil
		},
	}
	cmd.Flags().Bool("dev-allow-namespace-conflict", false,
		"On a dev profile, allow installing a plugin whose prefix is already locked to a different namespace_owner (spec §5.1). Ignored outside dev profiles.")
	cmd.Flags().Bool("yes", false, "Acknowledge that the plugin subprocess and manifest-declared capabilities are trusted enough to install.")
	return cmd
}

func newPluginListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List installed plugins",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out, err := DispatchPluginCommand([]string{"list"}, defaultPluginsHost())
			if err != nil {
				return err
			}
			if out == "" {
				// Keep stdout empty for pipes; tell an interactive human
				// (stderr) so an empty result isn't mistaken for a silent
				// failure, while piped/scripted output stays clean (gum-s985).
				if isTerminal(cmd.ErrOrStderr()) {
					_, _ = fmt.Fprintln(cmd.ErrOrStderr(), "No plugins installed.")
				}
				return nil
			}
			_, _ = fmt.Fprint(cmd.OutOrStdout(), out)
			return nil
		},
	}
}

func newPluginRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "remove <id>",
		Short: "Remove a plugin by ID",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out, err := DispatchPluginCommand(append([]string{"remove"}, args...), defaultPluginsHost())
			if err != nil {
				return err
			}
			_, _ = fmt.Fprint(cmd.OutOrStdout(), out)
			return nil
		},
	}
}

func newPluginRunCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "run <id> <tool> [args-json]",
		Short: "Call a tool on a running plugin",
		Args:  cobra.RangeArgs(2, 3),
		RunE: func(cmd *cobra.Command, args []string) error {
			out, err := DispatchPluginCommand(append([]string{"run"}, args...), defaultPluginsHost())
			if err != nil {
				return err
			}
			_, _ = fmt.Fprint(cmd.OutOrStdout(), out)
			return nil
		},
	}
}

// suppress "imported and not used" for fmt if only used in panic messages.
var _ = fmt.Sprintf

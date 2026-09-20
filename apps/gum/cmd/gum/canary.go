package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/ehmo/gum/internal/auth"
	"github.com/ehmo/gum/internal/plugins"
	"github.com/ehmo/gum/internal/plugins/registry"
	"github.com/spf13/cobra"
)

// newCanaryCmd implements `gum canary --plugin=<id> [--live]` (gum-xepy).
//
// Behaviour:
//   - --plugin is required; --live is optional and currently triggers a
//     no-op ping after a successful Start. v0.1.0 keeps the live ping as a
//     plain Start+Stop because the MCP go-sdk handshake already exercises
//     tools/list during Connect.
//   - On success, the command prints a single-line JSON envelope on stdout
//     and exits 0.
//   - On failure, the command prints a SERVICE_DOWN-shaped JSON envelope on
//     stdout (so it is machine-parseable in CI) and returns a non-nil error
//     so cobra sets a non-zero exit code. The envelope preserves the
//     source_error_code so the operator can distinguish "manifest missing"
//     from "handshake timeout" from "executable not executable".
//
// Spec §8.2 sets the SERVICE_DOWN mapping for the "plugin not installed"
// failure shape, and internal/plugins/error_mapping.go MapPluginErrorCode
// already encodes the projection table. This command applies the same
// projection at the CLI surface so external tooling does not have to know
// the Go-internal error sentinels.
func newCanaryCmd() *cobra.Command {
	var (
		pluginID string
		live     bool
	)
	cmd := &cobra.Command{
		Use:   "canary",
		Short: "Spawn a plugin subprocess once to verify it can boot",
		Long: "gum canary --plugin=<id> [--live] resolves the named plugin under " +
			"the active install root, spawns it once via the plugin host, and " +
			"reports the outcome as a stable JSON envelope on stdout. A failed " +
			"canary surfaces SERVICE_DOWN.",
		// Match the root command: a failed canary is a structured outcome,
		// not a usage error, so cobra should not pollute stdout with the
		// usage block. SilenceErrors keeps the JSON envelope as the only
		// thing the operator sees on stdout/stderr beyond the exit code.
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runCanary(cmd, pluginID, live)
		},
	}
	cmd.Flags().StringVar(&pluginID, "plugin", "", "Plugin id to canary (required)")
	cmd.Flags().BoolVar(&live, "live", false, "Issue a live subprocess ping after Start")
	_ = cmd.MarkFlagRequired("plugin")
	return cmd
}

// runCanary is split out so the table-driven test can reach the projection
// path without re-parsing flags through cobra.
func runCanary(cmd *cobra.Command, pluginID string, live bool) error {
	if pluginID == "" {
		return errors.New("gum canary: --plugin is required")
	}

	profile := resolveProfileFlag(cmd)
	// The canary is the gate that clears needs_configuration (§8.7), so it has
	// to spawn with the same credentials a real call gets: the profile keychain
	// entries `gum plugin setup` wrote.
	cfg := plugins.HostConfig{Profile: profile, Keyring: auth.NewOSKeyring()}
	// Verify against the plugins.lock row when one exists, so a rewritten
	// sidecar cannot make a swapped binary pass the canary.
	var reg *registry.Registry
	if dir, err := resolveProfileDir(profile); err == nil {
		reg = registry.New(dir)
		cfg.TrustedDigest = plugins.RecordedDigestResolver(reg)
	}
	host := plugins.NewHost(cfg)
	ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
	defer cancel()

	plug, err := startCanaryPlugin(ctx, host, reg, pluginID)
	if err != nil {
		emitCanaryEnvelope(cmd, map[string]any{
			"ok":                false,
			"plugin_id":         pluginID,
			"error_code":        canaryErrorCode(err),
			"source_error_code": canarySourceErrorCode(err),
			"message":           err.Error(),
		})
		return fmt.Errorf("gum canary: %s: %w", pluginID, err)
	}
	// Best-effort stop; a Stop failure is reported but does not flip the
	// canary outcome because Start already proved the subprocess can boot.
	stopErr := plug.Stop(ctx)
	env := map[string]any{
		"ok":        true,
		"plugin_id": pluginID,
		"live":      live,
	}
	if stopErr != nil {
		env["stop_error"] = stopErr.Error()
	}
	emitCanaryEnvelope(cmd, env)
	return nil
}

// startCanaryPlugin applies the spec §8.6 quarantine gate before spawning
// (gum-vgip). host.Start has no quarantine awareness, so without this gate a
// canary would execute a plugin the supervisor had already refused to run —
// the same hole gum-g7xr closed for `gum plugin run` and the dispatch adapter.
//
// The canary needs no exception for re-testing a failed plugin: `gum plugin
// reload <id>` already owns that path, clearing the quarantine first and then
// supervising the spawn. Unlike Supervisor.Start this does not record a crash
// on failure, because a diagnostic spawn must not advance the §8.6 backoff
// ladder and mask the fault on the next run.
//
// With no resolvable profile there is no registry, so there is no quarantine
// state to read and the spawn proceeds.
func startCanaryPlugin(ctx context.Context, host *plugins.Host, reg *registry.Registry, pluginID string) (*plugins.Plugin, error) {
	if reg != nil {
		state, err := plugins.ReadSupervisorState(reg, pluginID)
		if err != nil {
			return nil, fmt.Errorf("read plugin state: %w", err)
		}
		if err := plugins.CheckQuarantine(state, pluginID, time.Now()); err != nil {
			return nil, err
		}
	}
	return host.Start(ctx, pluginID)
}

// emitCanaryEnvelope writes a single-line JSON envelope to the command's
// stdout. Encoding errors are swallowed: the only realistic failure path
// here is a closed pipe, and printing a garbled envelope would only
// confuse callers further.
func emitCanaryEnvelope(cmd *cobra.Command, env map[string]any) {
	out := cmd.OutOrStdout()
	enc := json.NewEncoder(out)
	enc.SetIndent("", "")
	_ = enc.Encode(env)
}

// canaryErrorCode projects a Go-side Start error onto the stable spec §8
// error_code surface. The mapping is intentionally narrow: any failure that
// is not a known plugin-local code maps to SERVICE_DOWN.
func canaryErrorCode(err error) string {
	// Spec §13 bullet 5: a quarantined plugin surfaces VARIANT_QUARANTINED on
	// every invocation path, not SERVICE_DOWN. Nothing was spawned, so the
	// operator needs the quarantine named rather than a generic outage.
	if errors.Is(err, plugins.ErrPluginQuarantined) {
		return "VARIANT_QUARANTINED"
	}
	return "SERVICE_DOWN"
}

// canarySourceErrorCode preserves the Go-internal sentinel name so audit
// readers can correlate the spec §8 stable code with the actual failure.
func canarySourceErrorCode(err error) string {
	switch {
	case errors.Is(err, plugins.ErrManifestNotFound):
		return "ErrManifestNotFound"
	case errors.Is(err, plugins.ErrManifestInvalid):
		return "ErrManifestInvalid"
	case errors.Is(err, plugins.ErrExecutableUntrusted):
		return "ErrExecutableUntrusted"
	case errors.Is(err, plugins.ErrPluginEnvProhibited):
		return "ErrPluginEnvProhibited"
	case errors.Is(err, plugins.ErrPluginQuarantined):
		return "ErrPluginQuarantined"
	default:
		return "Unknown"
	}
}

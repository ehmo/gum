package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"github.com/ehmo/gum/internal/plugins"
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
		Short: "Spawn a plugin subprocess once to verify it can boot (§8)",
		Long: "gum canary --plugin=<id> [--live] resolves the named plugin under " +
			"the active install root, spawns it once via the plugin host, and " +
			"reports the outcome as a stable JSON envelope on stdout. A failed " +
			"canary surfaces SERVICE_DOWN per spec §8 error_code mapping.",
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
	cmd.Flags().BoolVar(&live, "live", false, "Issue a live subprocess ping after Start (§8.2)")
	_ = cmd.MarkFlagRequired("plugin")
	return cmd
}

// runCanary is split out so the table-driven test can reach the projection
// path without re-parsing flags through cobra.
func runCanary(cmd *cobra.Command, pluginID string, live bool) error {
	if pluginID == "" {
		return errors.New("gum canary: --plugin is required")
	}

	host := plugins.NewHost(plugins.HostConfig{})
	ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
	defer cancel()

	plug, err := host.Start(ctx, pluginID)
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
	if errors.Is(err, plugins.ErrManifestNotFound) ||
		errors.Is(err, plugins.ErrManifestInvalid) ||
		errors.Is(err, plugins.ErrExecutableUntrusted) {
		return "SERVICE_DOWN"
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
	default:
		return "Unknown"
	}
}

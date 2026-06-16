// Spec §7/§8.2: gum plugin setup <name> credential-prompt flow.
// Reads manifest credential_descriptors, prompts the user for each secret,
// stores in the OS keychain, then runs the live canary.
//
// User-facing output uses alias/display_name/setup_hint only — raw env var
// names MUST NOT appear in any user-visible message (spec §1414, §1606).

package plugins

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/ehmo/gum/internal/auth"
	"github.com/ehmo/gum/internal/plugins/registry"
)

// SetupOptions carries the injectable dependencies for SetupCredentials so
// tests can stub the keyring, canary, and I/O without spawning real processes.
type SetupOptions struct {
	// Registry is the active profile registry (required).
	Registry *registry.Registry
	// Profile is the active profile name (required, used in keychain keys).
	Profile string
	// InstallRoot is the directory where plugins are installed. Defaults to
	// ~/.local/share/gum/plugins. Tests override to a tempdir.
	InstallRoot string
	// Keyring is the backend used to persist secrets. Defaults to auth.NewOSKeyring().
	Keyring auth.KeyringBackend
	// In is the reader for user input (secret values). Defaults to os.Stdin.
	In io.Reader
	// Out is the writer for user-facing prompts. Defaults to os.Stdout.
	Out io.Writer
	// RunCanary executes the live canary for pluginID. Returns nil on success.
	// If nil, a default no-op canary is used (always succeeds), which is only
	// appropriate for tests that explicitly do not want canary logic.
	RunCanary func(ctx context.Context, pluginID string) error
}

// SetupCredentials implements the spec §7/§8.2 credential-prompt flow:
//
//  1. Load and validate the plugin manifest from the install root.
//  2. Validate credential_descriptors against needs_user_creds (§1606).
//  3. For each descriptor, prompt the user (using display_name + setup_hint)
//     and read the secret value from opts.In.
//  4. Store each secret in the OS keychain via opts.Keyring.
//  5. Run the live canary via opts.RunCanary.
//  6. On canary success: set plugin status to "active" in plugin-state.json.
//     On canary failure: set plugin status to "quarantined" with CANARY_FAILED
//     annotation per spec §8.6.
//
// All user-facing output uses alias/display_name/setup_hint only (spec §1414).
// Raw env var names MUST NOT appear in any returned error or written output.
func SetupCredentials(ctx context.Context, pluginID string, opts SetupOptions) error {
	// Validate pluginID before it is joined into a filesystem path
	// (loadManifestByPluginID does installRoot/<pluginID>/manifest.json). Without
	// this, `gum plugin setup ../../../etc` would escape the install root —
	// Host.Remove and Host.Start already guard against this; SetupCredentials did not.
	if !pluginIDRe.MatchString(pluginID) {
		return ErrManifestInvalid
	}
	if opts.Registry == nil {
		return fmt.Errorf("plugin setup: registry is required")
	}
	if opts.Profile == "" {
		return fmt.Errorf("plugin setup: profile is required")
	}
	if opts.Keyring == nil {
		opts.Keyring = auth.NewOSKeyring()
	}
	installRoot := opts.InstallRoot
	if installRoot == "" {
		home, _ := os.UserHomeDir()
		if home == "" {
			home = os.Getenv("HOME")
		}
		installRoot = home + "/.local/share/gum/plugins"
	}

	// 1. Load manifest.
	m, err := loadManifestByPluginID(installRoot, pluginID)
	if err != nil {
		return setupUserError("plugin not configured", "run 'gum plugin install' first")
	}

	// 2. Validate credential_descriptors.
	descs := m.Requirements.CredentialDescriptors
	needs := m.Requirements.NeedsUserCreds
	if err := ValidateCredentialDescriptors(needs, descs); err != nil {
		// Validation failures are manifest-author errors; wrap without the
		// original detail to avoid leaking env names in the user-visible path.
		return setupUserError("plugin manifest has invalid credential configuration",
			"check the plugin manifest with the plugin author")
	}

	if len(descs) == 0 {
		// No credentials required — nothing to prompt.
		return nil
	}

	// 3+4. Prompt for each credential and store in keyring.
	for _, d := range descs {
		if err := promptAndStore(opts, pluginID, d); err != nil {
			// Error messages use alias/display_name only.
			return fmt.Errorf("plugin setup: credential %q: %w", d.Alias, err)
		}
	}

	// 5. Run live canary.
	canaryFn := opts.RunCanary
	if canaryFn == nil {
		canaryFn = func(_ context.Context, _ string) error { return nil }
	}

	canaryErr := canaryFn(ctx, pluginID)
	if canaryErr != nil {
		// 6a. Canary failure: quarantine the plugin with CANARY_FAILED annotation.
		now := time.Now()
		if regErr := setPluginQuarantinedCANARYFailed(ctx, opts.Registry, pluginID, now); regErr != nil {
			// Surface registry error; canary error is the root cause but do not
			// leak canary details directly.
			return fmt.Errorf("plugin setup: canary failed and registry update failed: %w", regErr)
		}
		// Return a user-facing error without raw error internals.
		return fmt.Errorf("plugin setup: live canary failed for %q; plugin is quarantined. Run 'gum plugin reload %s' after fixing credentials", pluginID, pluginID)
	}

	// 6b. Canary success: mark plugin active.
	return setPluginActive(ctx, opts.Registry, pluginID, time.Now())
}

// promptAndStore prompts the user for descriptor d's secret, reads it from
// opts.In, and stores it in opts.Keyring. The prompt uses display_name and
// setup_hint — never the raw env var name.
func promptAndStore(opts SetupOptions, pluginID string, d CredentialDescriptor) error {
	w := opts.Out
	if w == nil {
		return fmt.Errorf("no output writer")
	}
	r := opts.In
	if r == nil {
		return fmt.Errorf("no input reader")
	}

	// Print prompt — use display_name and setup_hint only.
	if d.SetupHint != "" {
		_, _ = fmt.Fprintf(w, "  Hint: %s\n", d.SetupHint)
	}
	_, _ = fmt.Fprintf(w, "Enter value for %q (%s): ", d.DisplayName, d.Kind)

	// Read one line from stdin; the value is never echoed to logs.
	scanner := bufio.NewScanner(r)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return fmt.Errorf("reading input: %w", err)
		}
		return fmt.Errorf("no input provided for %q", d.DisplayName)
	}
	secret := strings.TrimRight(scanner.Text(), "\r\n")
	if secret == "" {
		return fmt.Errorf("empty value provided for %q", d.DisplayName)
	}

	key := PluginCredentialKey(opts.Profile, pluginID, d.Alias)
	if err := opts.Keyring.Set(key, secret); err != nil {
		return fmt.Errorf("keychain write failed for %q: %w", d.DisplayName, err)
	}

	return nil
}

// setPluginActive writes status=active + activated_at to plugin-state.json.
func setPluginActive(ctx context.Context, reg *registry.Registry, pluginName string, now time.Time) error {
	return reg.WriteTransaction(ctx, func(f *registry.Files) error {
		row, idx := findOrAppendRow(f, pluginName)
		row["status"] = "active"
		row["activated_at"] = now.UTC().Format(time.RFC3339)
		delete(row, "reason")
		f.State.Plugins[idx] = row
		return nil
	})
}

// setPluginQuarantinedCANARYFailed writes quarantine state with the
// CANARY_FAILED annotation per spec §8.6.
func setPluginQuarantinedCANARYFailed(ctx context.Context, reg *registry.Registry, pluginName string, now time.Time) error {
	return reg.WriteTransaction(ctx, func(f *registry.Files) error {
		row, idx := findOrAppendRow(f, pluginName)
		row["quarantined"] = true
		row["quarantined_at"] = now.UTC().Format(time.RFC3339)
		row["last_error_code"] = "CANARY_FAILED"
		row["reason"] = "CANARY_FAILED"
		f.State.Plugins[idx] = row
		return nil
	})
}

// loadManifestByPluginID loads the manifest from <installRoot>/<pluginID>/.
func loadManifestByPluginID(installRoot, pluginID string) (*Manifest, error) {
	return LoadManifest(installRoot + "/" + pluginID)
}

// setupUserError returns an error whose message contains only the safe
// user-facing strings (no raw env var names, no internal error details).
func setupUserError(summary, suggestion string) error {
	if suggestion != "" {
		return fmt.Errorf("plugin setup: %s (%s)", summary, suggestion)
	}
	return fmt.Errorf("plugin setup: %s", summary)
}

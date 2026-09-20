// Spec §5.1 + §8.7: registry-aware install path. The host's legacy Install()
// (host.go) is file-copy only; this file ships the spec-mandated install
// path that ALSO writes the three registry files (plugin-catalog.json,
// plugins.lock, plugin-state.json) and runs the install-time validators
// gum-f5j shipped:
//
//   - ValidateBinding   — every advertised tool produces a Binding whose
//     selector fields match the mcp-plugin contract before plugin-catalog.json
//     is committed.
//   - ValidateNamespaceOwnership — the plugin's prefix is not locked to a
//     different namespace_owner before plugins.lock is committed (override
//     via NamespaceOptions on dev profiles).
//
// On success the registry transaction writes the catalog variant rows, the
// lock entry (prefix→namespace_owner via RecordNamespaceOwner), and a
// state row for crash-recovery bookkeeping.

package plugins

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/plugins/registry"
)

// executableDigestSidecar is the in-install-dir file that records the sha256
// captured at install time. Start re-hashes the installed executable and
// compares against this value before exec'ing (spec §8.7 line 1690).
const executableDigestSidecar = ".executable.sha256"

// InstallOptions carries the registry binding + namespace-conflict opt-ins
// used by InstallWithRegistry. The Registry field is required: without it
// the install falls back to file-copy-only and the validators stay dormant.
type InstallOptions struct {
	Registry  *registry.Registry
	Namespace NamespaceOptions
}

// InstallWithRegistry is the §8.7 install protocol entry point: validate,
// then write registry files, then copy the plugin tree. It runs the three
// gum-f5j helpers at the gates the spec mandates (§5.1, §8) and surfaces
// the wrapped sentinel errors verbatim (PLUGIN_NAMESPACE_CONFLICT,
// PLUGIN_BINDING_INVALID) so the CLI can render the stable error envelope.
//
// Sequence:
//
//  1. LoadManifest (delegates to host.go).
//  2. Load existing registry (so the validators see the current lock).
//  3. ValidateNamespaceOwnership for the plugin's prefix (= plugin_id).
//  4. Build a Binding per advertised_tool; ValidateBinding each.
//  5. Copy files into install_root/<plugin_id>/ and hash the installed
//     executable.
//  6. WriteTransaction, merging on plugin_id per §8.7 step 2 so a reinstall
//     replaces the plugin's rows instead of duplicating them:
//     - One variant row per advertised_tool in plugin-catalog.json.
//     - One plugin row in plugins.lock + RecordNamespaceOwner.
//     - One fresh state row in plugin-state.json.
//
// Steps 1-3 fail without writing anything. Step 4 fails before the
// filesystem copy or transaction starts. Step 5 can leave an orphan install
// directory if interrupted before Step 6; the registry remains unmutated, so
// operators can safely re-run install or `gum plugin remove`. Step 6 is atomic.
func (h *Host) InstallWithRegistry(ctx context.Context, source string, opts InstallOptions) (string, error) {
	if opts.Registry == nil {
		return "", fmt.Errorf("plugin install: registry is required for InstallWithRegistry")
	}

	info, err := os.Stat(source)
	if err != nil {
		return "", fmt.Errorf("plugin install: stat source: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("plugin install: source %q is not a directory", source)
	}

	m, err := LoadManifest(source)
	if err != nil {
		return "", err
	}

	files, err := opts.Registry.Load()
	if err != nil {
		return "", fmt.Errorf("plugin install: load registry: %w", err)
	}

	prefix := m.PluginID
	if err := ValidateNamespaceOwnership(prefix, m.NamespaceOwner, files.Lock, opts.Namespace); err != nil {
		return "", err
	}

	bindings, err := buildPluginBindings(m)
	if err != nil {
		return "", err
	}

	// File copy MUST precede registry write so the executable_sha256 captured
	// in the lock row matches the file Start will re-hash at spawn time
	// (spec §8.7 / gum-25xk). Ordering: validate → copy → hash → write
	// transaction. A crash between copy and write leaves an orphan install
	// dir that the next install or `gum plugin remove` cleans up.
	if _, err := h.Install(ctx, source); err != nil {
		return "", err
	}

	installDir := filepath.Join(h.cfg.InstallRoot, m.PluginID)
	execPath := filepath.Join(installDir, m.Executable)
	if err := assertInsideInstallRoot(installDir, execPath); err != nil {
		return "", err
	}
	execSHA256, err := hashFileSHA256(execPath)
	if err != nil {
		return "", fmt.Errorf("plugin install: hash executable: %w", err)
	}
	// Sidecar file: Start reads this to construct the ExecutableBinding it
	// passes to VerifyExecutableBinding. Plain-hex digest at 0o600, matching
	// the registry files that hold the authoritative copy — the install dir
	// itself is 0o755, so a 0o644 sidecar was readable by every local account.
	sidecar := filepath.Join(installDir, executableDigestSidecar)
	if err := os.WriteFile(sidecar, []byte(execSHA256+"\n"), 0o600); err != nil {
		return "", fmt.Errorf("plugin install: write digest sidecar: %w", err)
	}

	err = opts.Registry.WriteTransaction(ctx, func(f *registry.Files) error {
		// Spec §8.7 step 2 says merge, not append. A reinstall or upgrade that
		// appended left two rows per plugin, and RecordedDigestResolver returns
		// the first match, so the superseded executable_sha256 won and every
		// spawn failed ErrExecutableUntrusted. Dropping the plugin's own rows
		// first makes the write idempotent in the plugin's key.
		f.Catalog.Variants = dropRows(f.Catalog.Variants, func(row map[string]any) bool {
			owner, _ := row["owner_plugin"].(string)
			return owner == m.PluginID
		})
		f.Lock.Plugins = dropRows(f.Lock.Plugins, func(row map[string]any) bool {
			name, _ := row["name"].(string)
			return name == m.PluginID
		})
		// The state row is rebuilt, not carried over: §8.7 step 2 calls it the
		// initial state. A reinstall is how an operator recovers a plugin that
		// quarantined itself, so inheriting the old quarantine flag would make
		// that recovery impossible.
		f.State.Plugins = dropRows(f.State.Plugins, func(row map[string]any) bool {
			name, _ := row["name"].(string)
			return name == m.PluginID
		})

		for i, tool := range m.AdvertisedTools {
			f.Catalog.Variants = append(f.Catalog.Variants, map[string]any{
				"variant_id":   pluginVariantID(m.PluginID, tool.Name),
				"op_id":        pluginOpID(m.PluginID, tool.Name),
				"owner_plugin": m.PluginID,
				"risk_class":   tool.RiskClass,
				"binding":      bindingToMap(bindings[i]),
			})
		}
		f.Lock.Plugins = append(f.Lock.Plugins, map[string]any{
			"name":              m.PluginID,
			"version":           m.Version,
			"namespace_owner":   m.NamespaceOwner,
			"prefix":            prefix,
			"executable_sha256": execSHA256,
		})
		RecordNamespaceOwner(f.Lock, prefix, m.NamespaceOwner)
		f.State.Plugins = append(f.State.Plugins, initialPluginState(m, execSHA256, time.Now()))
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("plugin install: write registry: %w", err)
	}

	return m.PluginID, nil
}

// initialPluginState builds the spec §8.7 step 2 initial state row: the
// install stamp, a null activation, and the two gate flags.
//
// A fresh install is installed_pending_restart, not active. A running MCP
// server keeps serving the roster it booted with, so the row stays out of
// every invokable surface until PromotePendingRestart runs at the next
// startup (spec §13 line 3148). A manifest that declares needs_user_creds
// parks in needs_configuration instead: `gum plugin setup` and its live
// canary are what activate that one.
func initialPluginState(m *Manifest, execSHA256 string, now time.Time) map[string]any {
	needsConfig := len(m.Requirements.NeedsUserCreds) > 0 ||
		len(m.Requirements.CredentialDescriptors) > 0
	status := StatusInstalledPendingRestart
	if needsConfig {
		status = StatusNeedsConfiguration
	}
	return map[string]any{
		"name":                m.PluginID,
		"status":              status,
		"installed_at":        now.UTC().Format(time.RFC3339),
		"activated_at":        nil,
		"quarantined":         false,
		"needs_configuration": needsConfig,
		"executable_sha256":   execSHA256,
	}
}

// buildPluginBindings turns each advertised_tool into a Binding and runs
// ValidateBinding before any registry mutation. Returns the slice in
// manifest order so callers can pair index-wise with m.AdvertisedTools.
func buildPluginBindings(m *Manifest) ([]*catalog.Binding, error) {
	out := make([]*catalog.Binding, 0, len(m.AdvertisedTools))
	for _, tool := range m.AdvertisedTools {
		b := &catalog.Binding{
			BindingSchemaVersion: 1,
			AdapterKey:           "plugin.mcp",
			OperationKey:         pluginOpID(m.PluginID, tool.Name),
			PluginName:           m.PluginID,
			ToolName:             tool.Name,
		}
		if err := ValidateBinding(b, catalog.BackendKindMCPPlugin); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, nil
}

func pluginOpID(pluginID, toolName string) string {
	return "plug." + pluginID + "." + toolName
}

func pluginVariantID(pluginID, toolName string) string {
	return pluginOpID(pluginID, toolName) + ".v1"
}

func bindingToMap(b *catalog.Binding) map[string]any {
	return map[string]any{
		"binding_schema_version": b.BindingSchemaVersion,
		"adapter_key":            b.AdapterKey,
		"operation_key":          b.OperationKey,
		"plugin_name":            b.PluginName,
		"tool_name":              b.ToolName,
	}
}

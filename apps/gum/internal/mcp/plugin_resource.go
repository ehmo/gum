// Spec §13 line 3158-3166: full gum://plugin/{name} record assembly from the
// three §8.7 registry files. The MCP resource handler (resource_templates.go)
// delegates to loadPluginResourceRecord here; keeping the assembly in its own
// file keeps the precedence rule auditable in one place when v0.2+ extends
// the field set (e.g., last_error_code wiring once §8.6 quarantine state is
// emitted by the runtime).
//
// Source precedence (spec §13 line 3158):
//   - runtime status (status, reason, quarantined_at, last_error_code,
//     installed_at, activated_at) → plugin-state.json wins
//   - variant_ids and variant_count                → plugin-catalog.json wins
//   - package + executable (source, ref, checksum, argv, sha256, install_root,
//     description, namespace_owner, version, shape, tos, risk)
//                                                  → plugins.lock wins
//   - install_generation                            → plugin-state.json wins
//     (lockfile carries the same value post-WriteTransaction; we surface the
//     state copy because it is the runtime authority per spec §8.7)
//
// metadata_warning="lock_catalog_mismatch" is emitted when the lock-asserted
// variant_count diverges from the actual count of catalog variants whose
// owner_plugin matches; that is the cheapest source-disagreement signal the
// reader can compute today without re-running ValidateBinding.

package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
)

// pluginResourceRecord is the §13 line 3161 wire shape. Status-specific
// fields are populated by the assembler based on resolvePluginStatus. JSON
// tags use omitempty so optional fields (executable, activated_at, reason,
// credential_descriptors, quarantined_at, last_error_code, metadata_warning)
// stay absent when not applicable.
type pluginResourceRecord struct {
	Name                  string         `json:"name"`
	Version               string         `json:"version"`
	Description           string         `json:"description"`
	NamespaceOwner        string         `json:"namespace_owner"`
	Shape                 string         `json:"shape"`
	Status                string         `json:"status"`
	ToS                   string         `json:"tos"`
	Risk                  string         `json:"risk"`
	VariantCount          int            `json:"variant_count"`
	VariantIDs            []string       `json:"variant_ids"`
	Package               map[string]any `json:"package"`
	InstallGeneration     int            `json:"install_generation"`
	Executable            map[string]any `json:"executable,omitempty"`
	ActivatedAt           string         `json:"activated_at,omitempty"`
	InstalledAt           string         `json:"installed_at,omitempty"`
	Reason                string         `json:"reason,omitempty"`
	QuarantinedAt         string         `json:"quarantined_at,omitempty"`
	LastErrorCode         string         `json:"last_error_code,omitempty"`
	CredentialDescriptors []any          `json:"credential_descriptors,omitempty"`
	MetadataWarning       string         `json:"metadata_warning,omitempty"`
}

// loadPluginResourceRecord assembles the §13 line 3161 record for a single
// plugin or returns (nil, false) when no row exists in either plugins.lock or
// plugin-state.json for that name. Catalog-only entries are intentionally not
// surfaced as plugins because v0.1.0 installs always write a lock or state
// row alongside the catalog variant.
func (s *Server) loadPluginResourceRecord(name string) (*pluginResourceRecord, bool) {
	profileDir := s.profilePluginDir()
	if profileDir == "" || name == "" {
		return nil, false
	}
	lockTop := loadPluginFileEnvelope(filepath.Join(profileDir, "plugins.lock"))
	stateTop := loadPluginFileEnvelope(filepath.Join(profileDir, "plugin-state.json"))
	catalogTop := loadPluginFileEnvelope(filepath.Join(profileDir, "plugin-catalog.json"))

	lockRow := findPluginRow(lockTop, name)
	stateRow := findPluginRow(stateTop, name)
	if lockRow == nil && stateRow == nil {
		return nil, false
	}

	variantIDs := collectPluginVariantIDs(catalogTop, name)
	rec := &pluginResourceRecord{
		Name:              name,
		Version:           stringFromRow(lockRow, "version"),
		Description:       stringFromRow(lockRow, "description"),
		NamespaceOwner:    stringFromRow(lockRow, "namespace_owner"),
		Shape:             stringFromRow(lockRow, "shape"),
		ToS:               stringFromRow(lockRow, "tos"),
		Risk:              stringFromRow(lockRow, "risk"),
		VariantCount:      len(variantIDs),
		VariantIDs:        variantIDs,
		Package:           mapFromRow(lockRow, "package"),
		InstallGeneration: intFromTop(stateTop, "install_generation"),
		Status:            resolvePluginStatus(stateRow),
		InstalledAt:       stringFromRow(stateRow, "installed_at"),
	}
	if rec.Package == nil {
		// Empty object preserves the §13 line 3161 "package contains source,
		// ref, checksum" shape even when the lockfile is missing the fields;
		// downstream clients should branch on the inner keys, not the
		// envelope's presence.
		rec.Package = map[string]any{}
	}
	if rec.VariantIDs == nil {
		rec.VariantIDs = []string{}
	}
	if rec.InstallGeneration == 0 {
		rec.InstallGeneration = intFromTop(lockTop, "install_generation")
	}
	if exe := mapFromRow(lockRow, "executable"); exe != nil {
		rec.Executable = exe
	}

	// Status-specific fields.
	switch rec.Status {
	case "active":
		rec.ActivatedAt = stringFromRow(stateRow, "activated_at")
	case "installed_pending_restart":
		if rec.Reason = stringFromRow(stateRow, "reason"); rec.Reason == "" {
			rec.Reason = "restart_required"
		}
		rec.ActivatedAt = stringFromRow(stateRow, "activated_at")
	case "needs_configuration":
		if rec.Reason = stringFromRow(stateRow, "reason"); rec.Reason == "" {
			rec.Reason = "missing_credentials"
		}
		if descs := credentialDescriptors(stateRow); descs != nil {
			rec.CredentialDescriptors = descs
		}
	case "quarantined":
		rec.Reason = stringFromRow(stateRow, "reason")
		rec.QuarantinedAt = stringFromRow(stateRow, "quarantined_at")
		rec.LastErrorCode = stringFromRow(stateRow, "last_error_code")
	}

	// metadata_warning when the lockfile asserts a variant_count that does
	// not match the catalog's actual variant rows for this owner.
	if lockRow != nil {
		if asserted := intFromRow(lockRow, "variant_count"); asserted != 0 && asserted != rec.VariantCount {
			rec.MetadataWarning = "lock_catalog_mismatch"
		}
	}

	return rec, true
}

// loadPluginFileEnvelope parses the top-level JSON object of one §8.7 file.
// Missing or malformed files return nil so the assembler can treat them as
// "no rows" without branching on file-system errors. This matches the
// behaviour of loadPluginInventoryRows (no hard failure on first-install).
func loadPluginFileEnvelope(path string) map[string]any {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var top map[string]any
	if err := json.Unmarshal(data, &top); err != nil {
		return nil
	}
	return top
}

// findPluginRow scans the "plugins" array inside the envelope for the row
// whose "name" field matches. Returns nil when the envelope is nil, the
// "plugins" key is missing, or no row matches.
func findPluginRow(top map[string]any, name string) map[string]any {
	if top == nil {
		return nil
	}
	rows, _ := top["plugins"].([]any)
	for _, raw := range rows {
		row, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if n, _ := row["name"].(string); n == name {
			return row
		}
	}
	return nil
}

// collectPluginVariantIDs walks plugin-catalog.json variants[] and returns
// the variant_ids whose owner_plugin matches name, sorted lexicographically.
// Per spec §13 line 3161 the list is profile-inventory (not active-only).
func collectPluginVariantIDs(top map[string]any, name string) []string {
	if top == nil {
		return nil
	}
	variants, _ := top["variants"].([]any)
	out := make([]string, 0, len(variants))
	for _, raw := range variants {
		row, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if owner, _ := row["owner_plugin"].(string); owner != name {
			continue
		}
		if id, _ := row["variant_id"].(string); id != "" {
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out
}

// mapFromRow returns row[key] when it is a JSON object; nil otherwise. The
// returned map is the original reference — callers must not mutate it.
func mapFromRow(row map[string]any, key string) map[string]any {
	if row == nil {
		return nil
	}
	m, _ := row[key].(map[string]any)
	return m
}

// credentialDescriptors returns the sanitised descriptor list for a
// needs_configuration plugin. Spec §13 line 3165 forbids raw env names in
// this resource, so this helper passes through only the four whitelisted
// fields per descriptor (alias, kind, display_name, setup_hint).
func credentialDescriptors(stateRow map[string]any) []any {
	if stateRow == nil {
		return nil
	}
	raw, _ := stateRow["credential_descriptors"].([]any)
	if len(raw) == 0 {
		return nil
	}
	out := make([]any, 0, len(raw))
	for _, item := range raw {
		desc, ok := item.(map[string]any)
		if !ok {
			continue
		}
		safe := map[string]any{}
		for _, key := range []string{"alias", "kind", "display_name", "setup_hint"} {
			if v, ok := desc[key]; ok {
				safe[key] = v
			}
		}
		out = append(out, safe)
	}
	return out
}

// intFromTop reads a top-level integer field (install_generation,
// install_txid-counters) from one of the §8.7 file envelopes.
func intFromTop(top map[string]any, key string) int {
	if top == nil {
		return 0
	}
	return intFromRow(top, key)
}

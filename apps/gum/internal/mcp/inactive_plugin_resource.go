// Spec §13: inactive-plugin and quarantined branches
// for gum://op/{id} and gum://variant/{id}. The handlers in
// resource_templates.go delegate to inactivePluginOpResponse /
// inactivePluginVariantResponse when the active snapshot misses; if the
// requested op/variant is owned by an inventory-visible plugin, the response
// is determined by the plugin's runtime status.
//
// Status mapping (spec §13):
//   - installed_pending_restart → JSON body {execution_support: "schema_only",
//     status, reason: "Plugin installed but MCP server not yet restarted;
//     restart to invoke."}
//   - needs_configuration       → JSON body adds credential_aliases (manifest
//     descriptor aliases only, no raw env names per spec §13)
//   - quarantined               → JSON-RPC application error with envelope
//     error_code: VARIANT_QUARANTINED + reason from plugin-state.json
//   - active                    → return (nil, false) so the snapshot-miss
//     remains a RESOURCE_NOT_FOUND. The boot-time merge
//     (plugins.SessionCatalog) puts every active plugin variant in the
//     snapshot, so reaching this arm means the row was refused as malformed
//     or its install generation was torn. Both are real faults, and
//     RESOURCE_NOT_FOUND is the honest answer; synthesising a record here
//     would advertise an op no dispatch can route.
//
// VARIANT_QUARANTINED uses JSON-RPC code -32000 (spec §13 "other
// stable runtime resource errors"); RESOURCE_NOT_FOUND uses the SDK's
// CodeResourceNotFound due to the collision documented in help_resource.go.

package mcp

import (
	"encoding/json"
	"path/filepath"

	"github.com/ehmo/gum/internal/output/jcs"
	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	// jsonRPCRuntimeAppError matches spec §13's "-32000 for other
	// stable runtime resource errors". VARIANT_QUARANTINED rides this code.
	jsonRPCRuntimeAppError = -32000

	reasonInstalledPendingRestart = "Plugin installed but MCP server not yet restarted; restart to invoke."
	reasonNeedsConfiguration      = "Plugin requires credential setup and a successful live canary before activation."
)

// inactivePluginOpResponse returns the inactive-plugin branch for gum://op/{id}.
//   - handled=false → inventory doesn't own this op_id; caller falls through
//     to RESOURCE_NOT_FOUND.
//   - handled=true + err != nil → quarantined; caller returns the error.
//   - handled=true + result != nil → pending_restart / needs_configuration.
//   - handled=true + result == nil + err == nil → active; caller falls through.
//     An active plugin's ops are merged into the snapshot at boot, so this
//     arm is reached only when the merge refused the row or the install
//     generation was torn.
func (s *Server) inactivePluginOpResponse(uri, opID string) (*sdkmcp.ReadResourceResult, *jsonrpc.Error, bool) {
	owner, ok := s.lookupOpOwnerPlugin(opID)
	if !ok {
		return nil, nil, false
	}
	return s.inactivePluginResponseFor(uri, owner, opID, "")
}

// inactivePluginVariantResponse is the symmetric path for gum://variant/{id}.
func (s *Server) inactivePluginVariantResponse(uri, variantID string) (*sdkmcp.ReadResourceResult, *jsonrpc.Error, bool) {
	owner, ok := s.lookupVariantOwnerPlugin(variantID)
	if !ok {
		return nil, nil, false
	}
	return s.inactivePluginResponseFor(uri, owner, "", variantID)
}

// inactivePluginResponseFor branches on the owning plugin's status. See
// inactivePluginOpResponse for the return-tuple contract.
func (s *Server) inactivePluginResponseFor(uri, ownerPlugin, opID, variantID string) (*sdkmcp.ReadResourceResult, *jsonrpc.Error, bool) {
	stateRow := s.lookupStateRow(ownerPlugin)
	if stateRow == nil {
		return nil, nil, false
	}
	if quar, _ := stateRow["quarantined"].(bool); quar {
		reason, _ := stateRow["reason"].(string)
		return nil, variantQuarantinedError(uri, opID, variantID, reason), true
	}
	status := resolvePluginStatus(stateRow)
	switch status {
	case "installed_pending_restart":
		payload := map[string]any{
			"execution_support": "schema_only",
			"status":            status,
			"reason":            reasonInstalledPendingRestart,
		}
		return jsonResourceResultFromPayload(uri, payload), nil, true
	case "needs_configuration":
		payload := map[string]any{
			"execution_support":  "schema_only",
			"status":             status,
			"reason":             reasonNeedsConfiguration,
			"credential_aliases": credentialAliasNames(stateRow),
		}
		return jsonResourceResultFromPayload(uri, payload), nil, true
	case "quarantined":
		// Belt-and-braces fallback: resolvePluginStatus returns "quarantined"
		// only when the explicit quarantined=true flag isn't set; if a future
		// state writer sets status="quarantined" directly we still need to
		// emit the VARIANT_QUARANTINED envelope. Reason may be empty here.
		reason, _ := stateRow["reason"].(string)
		return nil, variantQuarantinedError(uri, opID, variantID, reason), true
	}
	return nil, nil, false
}

// lookupOpOwnerPlugin scans plugin-catalog.json variants[] for a row whose
// op_id matches and returns the owner_plugin name.
func (s *Server) lookupOpOwnerPlugin(opID string) (string, bool) {
	top := loadPluginFileEnvelope(filepath.Join(s.profilePluginDir(), "plugin-catalog.json"))
	if top == nil {
		return "", false
	}
	variants, _ := top["variants"].([]any)
	for _, raw := range variants {
		row, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if got, _ := row["op_id"].(string); got != opID {
			continue
		}
		if owner, _ := row["owner_plugin"].(string); owner != "" {
			return owner, true
		}
	}
	return "", false
}

// lookupVariantOwnerPlugin is the variant_id counterpart of lookupOpOwnerPlugin.
func (s *Server) lookupVariantOwnerPlugin(variantID string) (string, bool) {
	top := loadPluginFileEnvelope(filepath.Join(s.profilePluginDir(), "plugin-catalog.json"))
	if top == nil {
		return "", false
	}
	variants, _ := top["variants"].([]any)
	for _, raw := range variants {
		row, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if got, _ := row["variant_id"].(string); got != variantID {
			continue
		}
		if owner, _ := row["owner_plugin"].(string); owner != "" {
			return owner, true
		}
	}
	return "", false
}

// lookupStateRow returns the plugin-state.json row for the named plugin.
// Used to read status, reason, quarantined_at, credential_descriptors.
func (s *Server) lookupStateRow(name string) map[string]any {
	top := loadPluginFileEnvelope(filepath.Join(s.profilePluginDir(), "plugin-state.json"))
	return findPluginRow(top, name)
}

// credentialAliasNames extracts safe alias strings from the state row's
// credential_descriptors. Other descriptor fields (kind, env, secret name)
// are deliberately dropped per spec §13.
func credentialAliasNames(stateRow map[string]any) []string {
	if stateRow == nil {
		return nil
	}
	raw, _ := stateRow["credential_descriptors"].([]any)
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		desc, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if alias, _ := desc["alias"].(string); alias != "" {
			out = append(out, alias)
		}
	}
	return out
}

// jsonResourceResultFromPayload canonicalizes payload per RFC 8785 and wraps
// it in the one-content-item shape. gum://op/{id} and gum://variant/{id} are
// named by spec §13, so the body MUST be JCS-canonical; stdlib
// json.Marshal is not a JCS encoder and would HTML-escape a credential alias
// containing '&', '<' or '>'. The error is discarded because the payload
// holds only strings and a []string, which neither encoder can reject
// (bead gum-yi62).
func jsonResourceResultFromPayload(uri string, payload map[string]any) *sdkmcp.ReadResourceResult {
	body, _ := jcs.Marshal(payload)
	return jsonResourceResult(uri, body)
}

// variantQuarantinedError builds the canonical VARIANT_QUARANTINED envelope.
// Spec §13 names the error code; §7 pins JSON-RPC -32000 for
// non-RESOURCE_NOT_FOUND / non-RESULT_ARTIFACT_EXPIRED runtime errors.
func variantQuarantinedError(uri, opID, variantID, reason string) *jsonrpc.Error {
	envelope := map[string]any{
		"error_code":   "VARIANT_QUARANTINED",
		"uri":          uri,
		"user_message": "Variant is quarantined: " + uri + ".",
	}
	if opID != "" {
		envelope["op_id"] = opID
	}
	if variantID != "" {
		envelope["variant_id"] = variantID
	}
	if reason != "" {
		envelope["reason"] = reason
	}
	data, _ := json.Marshal(envelope)
	return &jsonrpc.Error{
		Code:    jsonRPCRuntimeAppError,
		Message: "Variant quarantined",
		Data:    data,
	}
}

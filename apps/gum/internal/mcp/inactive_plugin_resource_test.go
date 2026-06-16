// gum-j8g2 acceptance: spec §13 lines 3155 + 3179 + 3180 inactive-plugin and
// quarantined branches for gum://op/{id} and gum://variant/{id}.
//
// When the active catalog snapshot doesn't carry the op/variant but the
// plugin-catalog.json inventory does, the resource handler MUST consult
// plugin-state.json for the owning plugin's status and:
//   - installed_pending_restart / needs_configuration → return a status-only
//     schema response with execution_support="schema_only".
//   - quarantined → return a JSON-RPC application error with envelope
//     error_code="VARIANT_QUARANTINED" (NOT RESOURCE_NOT_FOUND).
//   - otherwise: RESOURCE_NOT_FOUND (preserves the existing miss behaviour).

package mcp_test

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"go.uber.org/goleak"
)

// TestOpResourceInactivePluginPath covers the spec §13 line 3179 op-resource
// branches: pending_restart and needs_configuration each surface a
// status-only schema instead of the full record (or a RESOURCE_NOT_FOUND).
func TestOpResourceInactivePluginPath(t *testing.T) {
	defer goleak.VerifyNone(t)

	t.Run("pending_restart_returns_schema_only", func(t *testing.T) {
		ctx, cs, profileDir, cleanup := connectResourceClient(t)
		defer cleanup()
		seedInactivePlugin(t, profileDir, "p-restart", "installed_pending_restart", nil)
		const uri = "gum://op/plug.p-restart.do_thing"
		res, err := cs.ReadResource(ctx, &sdkmcp.ReadResourceParams{URI: uri})
		if err != nil {
			t.Fatalf("ReadResource(%s): %v", uri, err)
		}
		payload := singleJSONContent(t, res, uri)
		if got, _ := payload["execution_support"].(string); got != "schema_only" {
			t.Errorf("execution_support=%q; want schema_only", got)
		}
		if got, _ := payload["status"].(string); got != "installed_pending_restart" {
			t.Errorf("status=%q; want installed_pending_restart", got)
		}
		if got, _ := payload["reason"].(string); got != "Plugin installed but MCP server not yet restarted; restart to invoke." {
			t.Errorf("reason=%q; mismatch with spec §13 line 3179", got)
		}
		// pending_restart MUST NOT carry credential_aliases (that's the
		// needs_configuration branch).
		if _, present := payload["credential_aliases"]; present {
			t.Errorf("credential_aliases present for pending_restart; spec only allows it on needs_configuration")
		}
	})

	t.Run("needs_configuration_returns_credential_aliases", func(t *testing.T) {
		ctx, cs, profileDir, cleanup := connectResourceClient(t)
		defer cleanup()
		seedInactivePlugin(t, profileDir, "p-config", "needs_configuration", []string{"flights_oauth", "openai_api_key"})
		const uri = "gum://op/plug.p-config.do_thing"
		res, err := cs.ReadResource(ctx, &sdkmcp.ReadResourceParams{URI: uri})
		if err != nil {
			t.Fatalf("ReadResource(%s): %v", uri, err)
		}
		payload := singleJSONContent(t, res, uri)
		if got, _ := payload["execution_support"].(string); got != "schema_only" {
			t.Errorf("execution_support=%q; want schema_only", got)
		}
		if got, _ := payload["status"].(string); got != "needs_configuration" {
			t.Errorf("status=%q; want needs_configuration", got)
		}
		if got, _ := payload["reason"].(string); got != "Plugin requires credential setup and a successful live canary before activation." {
			t.Errorf("reason=%q; mismatch with spec §13 line 3179", got)
		}
		aliases, _ := payload["credential_aliases"].([]any)
		if len(aliases) != 2 || aliases[0] != "flights_oauth" || aliases[1] != "openai_api_key" {
			t.Errorf("credential_aliases=%v; want [flights_oauth openai_api_key]", aliases)
		}
	})
}

// TestVariantResourceQuarantinedPath covers spec §13 lines 3155 + 3180: a
// variant owned by a quarantined plugin returns the VARIANT_QUARANTINED
// application error (not RESOURCE_NOT_FOUND and not a status-only response).
func TestVariantResourceQuarantinedPath(t *testing.T) {
	defer goleak.VerifyNone(t)

	ctx, cs, profileDir, cleanup := connectResourceClient(t)
	defer cleanup()
	seedQuarantinedPlugin(t, profileDir, "p-bad", "binary checksum mismatch")
	const uri = "gum://variant/plug.p-bad.do_thing.v1"
	_, err := cs.ReadResource(ctx, &sdkmcp.ReadResourceParams{URI: uri})
	if err == nil {
		t.Fatal("ReadResource returned nil error; want VARIANT_QUARANTINED envelope")
	}
	var rpcErr *jsonrpc.Error
	if !errors.As(err, &rpcErr) {
		t.Fatalf("error type=%T; want *jsonrpc.Error", err)
	}
	// Per spec §13 line 1427 the JSON-RPC code is -32000 for non-RESOURCE_NOT_FOUND
	// / non-RESULT_ARTIFACT_EXPIRED runtime resource errors.
	if rpcErr.Code != -32000 {
		t.Errorf("JSON-RPC error.code=%d; want -32000", rpcErr.Code)
	}
	envelope := decodeEnvelope(t, rpcErr.Data)
	if got, _ := envelope["error_code"].(string); got != "VARIANT_QUARANTINED" {
		t.Errorf("envelope.error_code=%q; want VARIANT_QUARANTINED", got)
	}
	if got, _ := envelope["variant_id"].(string); got != "plug.p-bad.do_thing.v1" {
		t.Errorf("envelope.variant_id=%q; want plug.p-bad.do_thing.v1", got)
	}
	if reason, _ := envelope["reason"].(string); reason != "binary checksum mismatch" {
		t.Errorf("envelope.reason=%q; want quarantine cause from plugin-state.json", reason)
	}
}

// TestOpResourceQuarantinedPath pins the §13 line 3180 op-resource quarantine
// path symmetric to the variant test above.
func TestOpResourceQuarantinedPath(t *testing.T) {
	defer goleak.VerifyNone(t)

	ctx, cs, profileDir, cleanup := connectResourceClient(t)
	defer cleanup()
	seedQuarantinedPlugin(t, profileDir, "p-bad", "binary checksum mismatch")
	const uri = "gum://op/plug.p-bad.do_thing"
	_, err := cs.ReadResource(ctx, &sdkmcp.ReadResourceParams{URI: uri})
	if err == nil {
		t.Fatal("ReadResource returned nil error; want VARIANT_QUARANTINED envelope")
	}
	var rpcErr *jsonrpc.Error
	if !errors.As(err, &rpcErr) {
		t.Fatalf("error type=%T; want *jsonrpc.Error", err)
	}
	if rpcErr.Code != -32000 {
		t.Errorf("JSON-RPC error.code=%d; want -32000", rpcErr.Code)
	}
	envelope := decodeEnvelope(t, rpcErr.Data)
	if got, _ := envelope["error_code"].(string); got != "VARIANT_QUARANTINED" {
		t.Errorf("envelope.error_code=%q; want VARIANT_QUARANTINED", got)
	}
}

// TestVariantResourceInactivePluginPath pins the §13 line 3155 symmetric
// inactive-plugin response for the variant resource.
func TestVariantResourceInactivePluginPath(t *testing.T) {
	defer goleak.VerifyNone(t)

	ctx, cs, profileDir, cleanup := connectResourceClient(t)
	defer cleanup()
	seedInactivePlugin(t, profileDir, "p-restart", "installed_pending_restart", nil)
	const uri = "gum://variant/plug.p-restart.do_thing.v1"
	res, err := cs.ReadResource(ctx, &sdkmcp.ReadResourceParams{URI: uri})
	if err != nil {
		t.Fatalf("ReadResource(%s): %v", uri, err)
	}
	payload := singleJSONContent(t, res, uri)
	if got, _ := payload["execution_support"].(string); got != "schema_only" {
		t.Errorf("execution_support=%q; want schema_only", got)
	}
	if got, _ := payload["status"].(string); got != "installed_pending_restart" {
		t.Errorf("status=%q; want installed_pending_restart", got)
	}
}

// seedInactivePlugin writes a plugin-catalog.json with one variant owned by
// `name`, a plugin-state.json marking the plugin with the given status, and
// a minimal lockfile row. credAliases is consulted only when the status is
// needs_configuration to populate credential_descriptors.
func seedInactivePlugin(t *testing.T, profileDir, name, status string, credAliases []string) {
	t.Helper()
	opID := "plug." + name + ".do_thing"
	variantID := opID + ".v1"
	catalog := map[string]any{
		"plugin_catalog_schema_version": 1,
		"variants": []map[string]any{
			{
				"variant_id":   variantID,
				"op_id":        opID,
				"owner_plugin": name,
				"risk_class":   "read",
				"binding": map[string]any{
					"binding_schema_version": 1,
					"adapter_key":            "plugin.mcp",
					"operation_key":          opID,
					"plugin_name":            name,
					"tool_name":              "do_thing",
				},
			},
		},
	}
	lock := map[string]any{
		"plugins_lock_schema_version": 1,
		"plugins": []map[string]any{
			{"name": name, "version": "1.0.0", "shape": "mcp-plugin"},
		},
	}
	stateRow := map[string]any{
		"name":   name,
		"status": status,
	}
	if status == "needs_configuration" && len(credAliases) > 0 {
		descs := make([]map[string]any, 0, len(credAliases))
		for _, alias := range credAliases {
			descs = append(descs, map[string]any{"alias": alias, "kind": "oauth"})
		}
		stateRow["credential_descriptors"] = descs
	}
	state := map[string]any{
		"plugin_state_schema_version": 1,
		"plugins":                     []map[string]any{stateRow},
	}
	writePluginRegistryFiles(t, profileDir, catalog, lock, state)
}

// seedQuarantinedPlugin writes a single-plugin inventory whose state row
// flips quarantined=true with the supplied human-readable reason.
func seedQuarantinedPlugin(t *testing.T, profileDir, name, reason string) {
	t.Helper()
	opID := "plug." + name + ".do_thing"
	variantID := opID + ".v1"
	catalog := map[string]any{
		"plugin_catalog_schema_version": 1,
		"variants": []map[string]any{
			{
				"variant_id":   variantID,
				"op_id":        opID,
				"owner_plugin": name,
				"risk_class":   "read",
			},
		},
	}
	lock := map[string]any{
		"plugins_lock_schema_version": 1,
		"plugins": []map[string]any{
			{"name": name, "version": "1.0.0", "shape": "mcp-plugin"},
		},
	}
	state := map[string]any{
		"plugin_state_schema_version": 1,
		"plugins": []map[string]any{
			{"name": name, "status": "active", "quarantined": true, "reason": reason},
		},
	}
	writePluginRegistryFiles(t, profileDir, catalog, lock, state)
}

// singleJSONContent extracts the JSON body from a successful resource read.
func singleJSONContent(t *testing.T, res *sdkmcp.ReadResourceResult, wantURI string) map[string]any {
	t.Helper()
	if len(res.Contents) != 1 {
		t.Fatalf("Contents=%d; want 1", len(res.Contents))
	}
	c := res.Contents[0]
	if c.URI != wantURI {
		t.Errorf("Contents[0].URI=%q; want %q", c.URI, wantURI)
	}
	if c.MIMEType != "application/json" {
		t.Errorf("MIMEType=%q; want application/json", c.MIMEType)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(c.Text), &payload); err != nil {
		t.Fatalf("payload not JSON: %v; raw=%s", err, c.Text)
	}
	return payload
}

// decodeEnvelope unmarshals a JSON-RPC error.data envelope.
func decodeEnvelope(t *testing.T, data []byte) map[string]any {
	t.Helper()
	var envelope map[string]any
	if err := json.Unmarshal(data, &envelope); err != nil {
		t.Fatalf("envelope unmarshal: %v; raw=%s", err, string(data))
	}
	return envelope
}

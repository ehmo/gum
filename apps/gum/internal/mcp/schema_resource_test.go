// gum-kqvf acceptance: spec §13 line 3156 gum://schema/{ref} body
// materialiser. The handler delegates to schema_resource.go which walks four
// resolution stages — grammar check, active first-party snapshot, profile-
// local plugin inventory, RESOURCE_NOT_FOUND fallback — and these tests pin
// every stage.
//
// First-party happy path is exercised by injecting a custom catalog that
// references the embedded `test-fixture.v1` schema (the v0.1.0 generator
// does not yet populate any first-party refs; see bd show gum-zev5).
// Plugin happy path seeds plugin-catalog.json + plugin-state.json +
// `<profile>/plugin-schemas/<ref>.<sha256>.json` to mirror the on-disk
// shape written by install_registry.go.

package mcp_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"go.uber.org/goleak"

	"github.com/ehmo/gum/internal/catalog"
	gummcp "github.com/ehmo/gum/internal/mcp"
)

// TestSchemaResourceFirstPartyHit covers the §13 line 3156 first-party path:
// a ref that is referenced by an op in the active snapshot AND has a body
// in the embedded gen/schemas/ store resolves to application/schema+json
// with JCS-canonical bytes.
func TestSchemaResourceFirstPartyHit(t *testing.T) {
	defer goleak.VerifyNone(t)

	snapshot := &catalog.Catalog{
		Ops: []catalog.Op{
			{
				OpID:             "test.placeholder.show",
				OpSchemaVersion:  1,
				Title:            "Placeholder op",
				Summary:          "Test-only op that pins a first-party schema_ref.",
				DefaultVariantID: "test.placeholder.show.v1",
				ResponseRef:      "test-fixture.v1",
				Variants: []catalog.Variant{
					{
						VariantID:            "test.placeholder.show.v1",
						VariantSchemaVersion: 1,
						RiskClass:            "read",
					},
				},
			},
		},
	}
	ctx, cs, _, cleanup := connectResourceClientWithCatalog(t, snapshot)
	defer cleanup()

	const uri = "gum://schema/test-fixture.v1"
	res, err := cs.ReadResource(ctx, &sdkmcp.ReadResourceParams{URI: uri})
	if err != nil {
		t.Fatalf("ReadResource(%s): %v", uri, err)
	}
	if len(res.Contents) != 1 {
		t.Fatalf("Contents=%d; want 1", len(res.Contents))
	}
	c := res.Contents[0]
	if c.URI != uri {
		t.Errorf("Contents[0].URI=%q; want %q", c.URI, uri)
	}
	if c.MIMEType != "application/schema+json" {
		t.Errorf("Contents[0].MIMEType=%q; want application/schema+json", c.MIMEType)
	}
	var schema map[string]any
	if err := json.Unmarshal([]byte(c.Text), &schema); err != nil {
		t.Fatalf("schema body not JSON: %v; raw=%q", err, c.Text)
	}
	if got, _ := schema["$schema"].(string); got != "https://json-schema.org/draft/2020-12/schema" {
		t.Errorf("$schema=%q; want JSON Schema 2020-12", got)
	}
	if got, _ := schema["$id"].(string); got != "test-fixture.v1" {
		t.Errorf("$id=%q; want test-fixture.v1", got)
	}
}

// TestSchemaResourcePluginHit covers the §13 line 3156 plugin path: a ref
// listed in plugin-catalog.json variants[].schema_hashes whose owner is
// active resolves from `<profileDir>/plugin-schemas/<ref>.<sha256>.json`.
func TestSchemaResourcePluginHit(t *testing.T) {
	defer goleak.VerifyNone(t)

	ctx, cs, profileDir, cleanup := connectResourceClient(t)
	defer cleanup()

	const (
		plugin = "test-plugin"
		ref    = "plug.test-plugin.do_thing.response"
	)
	body := []byte(`{"$schema":"https://json-schema.org/draft/2020-12/schema","$id":"plug.test-plugin.do_thing.response","type":"object","properties":{"items":{"type":"array"}}}`)
	hash := writePluginSchemaBody(t, profileDir, ref, body)
	seedPluginSchemaInventory(t, profileDir, plugin, ref, hash)

	uri := "gum://schema/" + ref
	res, err := cs.ReadResource(ctx, &sdkmcp.ReadResourceParams{URI: uri})
	if err != nil {
		t.Fatalf("ReadResource(%s): %v", uri, err)
	}
	if len(res.Contents) != 1 {
		t.Fatalf("Contents=%d; want 1", len(res.Contents))
	}
	c := res.Contents[0]
	if c.MIMEType != "application/schema+json" {
		t.Errorf("MIMEType=%q; want application/schema+json", c.MIMEType)
	}
	var schema map[string]any
	if err := json.Unmarshal([]byte(c.Text), &schema); err != nil {
		t.Fatalf("schema body not JSON: %v", err)
	}
	if got, _ := schema["$id"].(string); got != ref {
		t.Errorf("$id=%q; want %q", got, ref)
	}
}

// TestSchemaResourceUnknownRef pins the RESOURCE_NOT_FOUND fallback for a
// well-formed ref that is absent from every source.
func TestSchemaResourceUnknownRef(t *testing.T) {
	defer goleak.VerifyNone(t)
	ctx, cs, _, cleanup := connectResourceClient(t)
	defer cleanup()

	const uri = "gum://schema/no.such.ref.exists.v1"
	_, err := cs.ReadResource(ctx, &sdkmcp.ReadResourceParams{URI: uri})
	if err == nil {
		t.Fatal("ReadResource succeeded; want RESOURCE_NOT_FOUND for unknown ref")
	}
	assertResourceNotFound(t, err, uri)
}

// TestSchemaResourceInactivePluginRef — spec §13 line 3156: inactive plugin
// refs (installed_pending_restart, needs_configuration) return
// RESOURCE_NOT_FOUND rather than the schema_only response used for op/variant.
func TestSchemaResourceInactivePluginRef(t *testing.T) {
	defer goleak.VerifyNone(t)

	for _, status := range []string{"installed_pending_restart", "needs_configuration"} {
		t.Run(status, func(t *testing.T) {
			ctx, cs, profileDir, cleanup := connectResourceClient(t)
			defer cleanup()

			const (
				plugin = "p-inactive"
				ref    = "plug.p-inactive.do_thing.response"
			)
			body := []byte(`{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object"}`)
			hash := writePluginSchemaBody(t, profileDir, ref, body)
			seedPluginSchemaInventoryWithStatus(t, profileDir, plugin, ref, hash, status, false, "")

			uri := "gum://schema/" + ref
			_, err := cs.ReadResource(ctx, &sdkmcp.ReadResourceParams{URI: uri})
			if err == nil {
				t.Fatalf("ReadResource succeeded for %s ref; want RESOURCE_NOT_FOUND", status)
			}
			assertResourceNotFound(t, err, uri)
		})
	}
}

// TestSchemaResourceQuarantinedPluginRef pins the §13 line 3156 quarantine
// branch: even with a readable body on disk, a quarantined owner forces
// RESOURCE_NOT_FOUND (NOT the VARIANT_QUARANTINED envelope used for op/
// variant resources — schemas can't be "quarantined", only their owners can).
func TestSchemaResourceQuarantinedPluginRef(t *testing.T) {
	defer goleak.VerifyNone(t)

	ctx, cs, profileDir, cleanup := connectResourceClient(t)
	defer cleanup()

	const (
		plugin = "p-bad"
		ref    = "plug.p-bad.do_thing.response"
	)
	body := []byte(`{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object"}`)
	hash := writePluginSchemaBody(t, profileDir, ref, body)
	seedPluginSchemaInventoryWithStatus(t, profileDir, plugin, ref, hash, "active", true, "binary checksum mismatch")

	uri := "gum://schema/" + ref
	_, err := cs.ReadResource(ctx, &sdkmcp.ReadResourceParams{URI: uri})
	if err == nil {
		t.Fatal("ReadResource succeeded for quarantined-owner ref; want RESOURCE_NOT_FOUND")
	}
	envelope := assertResourceNotFound(t, err, uri)
	// Quarantine MUST surface as RESOURCE_NOT_FOUND, not VARIANT_QUARANTINED.
	if got, _ := envelope["error_code"].(string); got != "RESOURCE_NOT_FOUND" {
		t.Errorf("envelope.error_code=%q; want RESOURCE_NOT_FOUND (not VARIANT_QUARANTINED — schema refs use the not-found shape per §13 line 3156)", got)
	}
}

// TestSchemaResourceGrammarRejection — spec §8.2 line 1601: grammar check
// happens BEFORE any filesystem path construction. Uppercase letters, `..`,
// path separators (raw and percent-encoded), and over-length refs all
// resolve to RESOURCE_NOT_FOUND without touching disk.
func TestSchemaResourceGrammarRejection(t *testing.T) {
	defer goleak.VerifyNone(t)

	cases := []struct {
		name string
		ref  string
	}{
		{"uppercase", "Test.v1"},
		{"dotdot", "good..bad"},
		// `/` and `?` are already stripped by parseTemplateParam; cover the
		// post-decode grammar gate by routing `%2f` (encodes to `/`).
		{"percent_slash", "good%2fbad"},
		{"percent_backslash", "good%5cbad"},
		{"leading_dot", ".hidden.v1"},
		{"empty_after_decode", "%00"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cs, _, cleanup := connectResourceClient(t)
			defer cleanup()
			uri := "gum://schema/" + tc.ref
			_, err := cs.ReadResource(ctx, &sdkmcp.ReadResourceParams{URI: uri})
			if err == nil {
				t.Fatalf("ReadResource(%s) succeeded; want RESOURCE_NOT_FOUND from grammar gate", uri)
			}
			var rpcErr *jsonrpc.Error
			if !errors.As(err, &rpcErr) {
				t.Fatalf("error type=%T; want *jsonrpc.Error", err)
			}
			if rpcErr.Code != -32002 {
				t.Errorf("JSON-RPC error.code=%d; want -32002", rpcErr.Code)
			}
			var envelope map[string]any
			_ = json.Unmarshal(rpcErr.Data, &envelope)
			detail, _ := envelope["detail"].(string)
			if !strings.Contains(detail, "grammar") {
				t.Errorf("envelope.detail=%q; want a grammar-violation diagnostic", detail)
			}
		})
	}
}

// connectResourceClientWithCatalog mirrors connectResourceClient but lets
// the test inject a custom catalog snapshot (used by the first-party schema
// test to add an op that references the embedded `test-fixture.v1` schema).
func connectResourceClientWithCatalog(t *testing.T, snapshot *catalog.Catalog) (context.Context, *sdkmcp.ClientSession, string, func()) {
	t.Helper()
	dataHome := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dataHome)

	srv := gummcp.NewServerWithCatalog(stubDispatcher{}, snapshot)
	srvTransport, clientTransport := sdkmcp.NewInMemoryTransports()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- srv.Run(ctx, srvTransport) }()

	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "test-client", Version: "0.0.1"}, nil)
	cs, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		cancel()
		t.Fatalf("client.Connect: %v", err)
	}
	cleanup := func() {
		_ = cs.Close()
		cancel()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Error("server.Run did not stop within 2s after cancel")
		}
	}
	profileDir := filepath.Join(dataHome, "gum", "default")
	return ctx, cs, profileDir, cleanup
}

// writePluginSchemaBody writes raw to `<profileDir>/plugin-schemas/<ref>.<sha256>.json`
// and returns the hex-encoded SHA-256 of raw (the digest that goes into the
// plugin-catalog.json variants[].schema_hashes map).
func writePluginSchemaBody(t *testing.T, profileDir, ref string, raw []byte) string {
	t.Helper()
	dir := filepath.Join(profileDir, "plugin-schemas")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll %s: %v", dir, err)
	}
	sum := sha256.Sum256(raw)
	hash := hex.EncodeToString(sum[:])
	path := filepath.Join(dir, ref+"."+hash+".json")
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatalf("WriteFile %s: %v", path, err)
	}
	return hash
}

// seedPluginSchemaInventory writes plugin-catalog.json + plugins.lock +
// plugin-state.json for a plugin whose single variant carries one
// schema_hashes entry. Status defaults to "active" so the body is reachable.
func seedPluginSchemaInventory(t *testing.T, profileDir, plugin, ref, hash string) {
	t.Helper()
	seedPluginSchemaInventoryWithStatus(t, profileDir, plugin, ref, hash, "active", false, "")
}

// seedPluginSchemaInventoryWithStatus is the parameterised form used by the
// inactive/quarantine tests.
func seedPluginSchemaInventoryWithStatus(t *testing.T, profileDir, plugin, ref, hash, status string, quarantined bool, reason string) {
	t.Helper()
	opID := "plug." + plugin + ".do_thing"
	variantID := opID + ".v1"
	cat := map[string]any{
		"plugin_catalog_schema_version": 1,
		"variants": []map[string]any{
			{
				"variant_id":   variantID,
				"op_id":        opID,
				"owner_plugin": plugin,
				"risk_class":   "read",
				"schema_hashes": map[string]any{
					ref: hash,
				},
				"binding": map[string]any{
					"binding_schema_version": 1,
					"adapter_key":            "plugin.mcp",
					"operation_key":          opID,
					"plugin_name":            plugin,
					"tool_name":              "do_thing",
				},
			},
		},
	}
	lock := map[string]any{
		"plugins_lock_schema_version": 1,
		"plugins": []map[string]any{
			{"name": plugin, "version": "1.0.0", "shape": "mcp-plugin"},
		},
	}
	stateRow := map[string]any{
		"name":   plugin,
		"status": status,
	}
	if quarantined {
		stateRow["quarantined"] = true
	}
	if reason != "" {
		stateRow["reason"] = reason
	}
	state := map[string]any{
		"plugin_state_schema_version": 1,
		"plugins":                     []map[string]any{stateRow},
	}
	writePluginRegistryFiles(t, profileDir, cat, lock, state)
}

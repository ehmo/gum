// gum-l6fx acceptance: gum://plugin/{name} full §13 line 3161 surface.
//
// Spec §13 line 3158 prescribes a three-source assembly: runtime status from
// plugin-state.json, variant metadata from plugin-catalog.json, package fields
// from plugins.lock. Spec §13 line 3161 lists the required field set ({name,
// version, description, namespace_owner, shape, status, tos, risk,
// variant_count, variant_ids, package, install_generation} plus status-
// specific add-ons). These tests pin both — first by reading a fully-populated
// happy-path fixture and asserting every required field, then by forcing
// per-source disagreement and confirming the precedence rule.
//
// The fixtures write the three JSON files directly (no WriteTransaction) so
// the tests stay decoupled from the install path: a regression in the reader
// surfaces here regardless of writer drift.

package mcp_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"go.uber.org/goleak"
)

// writePluginRegistryFiles seeds the three §8.7 files at profileDir. Caller
// supplies the JSON-marshalable payloads; this helper only handles the file
// boundary so test bodies stay readable.
func writePluginRegistryFiles(t *testing.T, profileDir string, catalog, lock, state any) {
	t.Helper()
	if err := os.MkdirAll(profileDir, 0o755); err != nil {
		t.Fatalf("MkdirAll %s: %v", profileDir, err)
	}
	for name, payload := range map[string]any{
		"plugin-catalog.json": catalog,
		"plugins.lock":        lock,
		"plugin-state.json":   state,
	} {
		data, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("marshal %s: %v", name, err)
		}
		if err := os.WriteFile(filepath.Join(profileDir, name), data, 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
}


// TestPluginResourceFullSurface verifies the §13 line 3161 happy path: every
// required field is present and sourced from the file the spec names.
func TestPluginResourceFullSurface(t *testing.T) {
	defer goleak.VerifyNone(t)

	ctx, cs, profileDir, cleanup := connectResourceClient(t)
	defer cleanup()

	catalog := map[string]any{
		"plugin_catalog_schema_version": 1,
		"variants": []map[string]any{
			{
				"variant_id":   "plug.google-flights.flights_search.v1",
				"op_id":        "plug.google-flights.flights_search",
				"owner_plugin": "google-flights",
				"risk_class":   "read",
				"binding": map[string]any{
					"binding_schema_version": 1,
					"adapter_key":            "plugin.mcp",
					"operation_key":          "plug.google-flights.flights_search",
					"plugin_name":            "google-flights",
					"tool_name":              "flights_search",
				},
			},
			{
				"variant_id":   "plug.google-flights.flights_book.v1",
				"op_id":        "plug.google-flights.flights_book",
				"owner_plugin": "google-flights",
				"risk_class":   "high_stakes_write",
			},
		},
	}
	lock := map[string]any{
		"plugins_lock_schema_version": 1,
		"install_generation":          7,
		"install_txid":                "tx-aaaa",
		"plugins": []map[string]any{
			{
				"name":            "google-flights",
				"version":         "1.2.3",
				"description":     "Search and book flights via the fli MCP plugin.",
				"namespace_owner": "io.example.flights",
				"prefix":          "google-flights",
				"shape":           "mcp-plugin",
				"tos":             "accepted",
				"risk":            "read",
				"variant_count":   2,
				"package": map[string]any{
					"source":   "https://example.com/fli.tar.gz",
					"ref":      "v1.2.3",
					"checksum": "sha256:abc",
				},
				"executable": map[string]any{
					"argv_normalized":   []string{"uvx", "fli"},
					"executable_sha256": "sha256:exec",
					"install_root":      "/var/lib/gum/plugins/google-flights",
				},
			},
		},
	}
	state := map[string]any{
		"plugin_state_schema_version": 1,
		"install_generation":          7,
		"install_txid":                "tx-aaaa",
		"plugins": []map[string]any{
			{
				"name":         "google-flights",
				"status":       "active",
				"installed_at": "2026-01-15T10:00:00Z",
				"activated_at": "2026-01-15T10:05:00Z",
			},
		},
	}
	writePluginRegistryFiles(t, profileDir, catalog, lock, state)

	payload := readPluginResourceAt(t, ctx, cs, profileDir, "google-flights")

	// §13 line 3161 required-for-every-status fields.
	required := map[string]any{
		"name":               "google-flights",
		"version":            "1.2.3",
		"description":        "Search and book flights via the fli MCP plugin.",
		"namespace_owner":    "io.example.flights",
		"shape":              "mcp-plugin",
		"status":             "active",
		"tos":                "accepted",
		"risk":               "read",
		"variant_count":      float64(2),
		"install_generation": float64(7),
	}
	for k, want := range required {
		if got, ok := payload[k]; !ok {
			t.Errorf("payload[%q] absent", k)
		} else if !reflect.DeepEqual(got, want) {
			t.Errorf("payload[%q] = %#v; want %#v", k, got, want)
		}
	}

	// variant_ids sorted lexicographically across catalog variants.
	gotVariantIDs, _ := payload["variant_ids"].([]any)
	wantVariantIDs := []string{
		"plug.google-flights.flights_book.v1",
		"plug.google-flights.flights_search.v1",
	}
	if len(gotVariantIDs) != len(wantVariantIDs) {
		t.Fatalf("variant_ids len=%d; want %d", len(gotVariantIDs), len(wantVariantIDs))
	}
	for i, v := range gotVariantIDs {
		if v != wantVariantIDs[i] {
			t.Errorf("variant_ids[%d] = %v; want %s", i, v, wantVariantIDs[i])
		}
	}
	if !sort.SliceIsSorted(gotVariantIDs, func(i, j int) bool {
		return gotVariantIDs[i].(string) < gotVariantIDs[j].(string)
	}) {
		t.Error("variant_ids not lexicographically sorted")
	}

	// package fields from plugins.lock.
	pkg, ok := payload["package"].(map[string]any)
	if !ok {
		t.Fatalf("payload.package missing or wrong type: %T", payload["package"])
	}
	for k, want := range map[string]string{
		"source":   "https://example.com/fli.tar.gz",
		"ref":      "v1.2.3",
		"checksum": "sha256:abc",
	} {
		if got, _ := pkg[k].(string); got != want {
			t.Errorf("package[%q] = %q; want %q", k, got, want)
		}
	}

	// executable block surfaces when plugins.lock carries it.
	exe, ok := payload["executable"].(map[string]any)
	if !ok {
		t.Fatalf("payload.executable missing; spec §13 line 3161 requires it when lockfile has executable binding data")
	}
	if got, _ := exe["executable_sha256"].(string); got != "sha256:exec" {
		t.Errorf("executable.executable_sha256 = %q; want sha256:exec", got)
	}
	if got, _ := exe["install_root"].(string); got != "/var/lib/gum/plugins/google-flights" {
		t.Errorf("executable.install_root = %q; want /var/lib/gum/plugins/google-flights", got)
	}
	argv, _ := exe["argv_normalized"].([]any)
	if len(argv) != 2 || argv[0] != "uvx" || argv[1] != "fli" {
		t.Errorf("executable.argv_normalized = %#v; want [uvx fli]", argv)
	}

	// active-status add-on: activated_at present; reason and credential_descriptors absent.
	if got, _ := payload["activated_at"].(string); got != "2026-01-15T10:05:00Z" {
		t.Errorf("activated_at = %q; want 2026-01-15T10:05:00Z", got)
	}
	if _, present := payload["reason"]; present {
		t.Error("active-status payload must not include reason field")
	}
	if _, present := payload["credential_descriptors"]; present {
		t.Error("active-status payload must not include credential_descriptors")
	}

	// metadata_warning absent when sources agree.
	if _, present := payload["metadata_warning"]; present {
		t.Errorf("metadata_warning = %v; want absent when sources agree", payload["metadata_warning"])
	}
}

// TestPluginMetadataPrecedence pins the three-source precedence rule from
// spec §13 line 3158. Each subtest forces disagreement on a different axis
// and asserts the winner per the spec.
func TestPluginMetadataPrecedence(t *testing.T) {
	defer goleak.VerifyNone(t)

	t.Run("plugin_state_wins_for_status", func(t *testing.T) {
		ctx, cs, profileDir, cleanup := connectResourceClient(t)
		defer cleanup()
		writePluginRegistryFiles(t, profileDir,
			map[string]any{
				"plugin_catalog_schema_version": 1,
				"variants":                      []any{},
			},
			map[string]any{
				"plugins_lock_schema_version": 1,
				"plugins": []map[string]any{
					{"name": "p1", "version": "1.0.0", "shape": "mcp-plugin", "status": "active"},
				},
			},
			map[string]any{
				"plugin_state_schema_version": 1,
				"plugins": []map[string]any{
					{"name": "p1", "status": "needs_configuration", "reason": "missing_credentials"},
				},
			},
		)
		payload := readPluginResourceAt(t, ctx, cs, profileDir, "p1")
		if got, _ := payload["status"].(string); got != "needs_configuration" {
			t.Errorf("status = %q; want needs_configuration (plugin-state.json wins)", got)
		}
	})

	t.Run("plugin_catalog_wins_for_variant_metadata", func(t *testing.T) {
		ctx, cs, profileDir, cleanup := connectResourceClient(t)
		defer cleanup()
		writePluginRegistryFiles(t, profileDir,
			map[string]any{
				"plugin_catalog_schema_version": 1,
				"variants": []map[string]any{
					{"variant_id": "plug.p2.b.v1", "owner_plugin": "p2"},
					{"variant_id": "plug.p2.a.v1", "owner_plugin": "p2"},
				},
			},
			map[string]any{
				"plugins_lock_schema_version": 1,
				"plugins": []map[string]any{
					{"name": "p2", "version": "1.0.0", "variant_count": 99},
				},
			},
			map[string]any{
				"plugin_state_schema_version": 1,
				"plugins":                     []map[string]any{{"name": "p2", "status": "active"}},
			},
		)
		payload := readPluginResourceAt(t, ctx, cs, profileDir, "p2")
		ids, _ := payload["variant_ids"].([]any)
		want := []string{"plug.p2.a.v1", "plug.p2.b.v1"}
		if len(ids) != 2 || ids[0] != want[0] || ids[1] != want[1] {
			t.Errorf("variant_ids = %v; want %v (plugin-catalog.json wins, sorted)", ids, want)
		}
		// Disagreement: lockfile says 99 but catalog has 2.
		if got, _ := payload["metadata_warning"].(string); got != "lock_catalog_mismatch" {
			t.Errorf("metadata_warning = %q; want lock_catalog_mismatch when lock.variant_count diverges from catalog variant count", got)
		}
	})

	t.Run("plugins_lock_wins_for_package_fields", func(t *testing.T) {
		ctx, cs, profileDir, cleanup := connectResourceClient(t)
		defer cleanup()
		writePluginRegistryFiles(t, profileDir,
			map[string]any{
				"plugin_catalog_schema_version": 1,
				"variants":                      []any{},
			},
			map[string]any{
				"plugins_lock_schema_version": 1,
				"plugins": []map[string]any{
					{
						"name":    "p3",
						"version": "2.0.0",
						"package": map[string]any{
							"source":   "https://lock.example/pkg",
							"ref":      "v2.0.0",
							"checksum": "sha256:lock",
						},
					},
				},
			},
			map[string]any{
				"plugin_state_schema_version": 1,
				"plugins":                     []map[string]any{{"name": "p3", "status": "active"}},
			},
		)
		payload := readPluginResourceAt(t, ctx, cs, profileDir, "p3")
		pkg, _ := payload["package"].(map[string]any)
		if pkg == nil {
			t.Fatal("payload.package missing; lockfile should provide it")
		}
		if got, _ := pkg["source"].(string); got != "https://lock.example/pkg" {
			t.Errorf("package.source = %q; want https://lock.example/pkg (lockfile wins)", got)
		}
		if got, _ := pkg["checksum"].(string); got != "sha256:lock" {
			t.Errorf("package.checksum = %q; want sha256:lock", got)
		}
	})
}

// readPluginResourceAt is the lookup primitive used by both happy-path and
// precedence tests. It calls resources/read for gum://plugin/<name> and
// returns the JSON-decoded body.
func readPluginResourceAt(t *testing.T, ctx context.Context, cs *sdkmcp.ClientSession, _profileDir, name string) map[string]any {
	t.Helper()
	uri := "gum://plugin/" + name
	res, err := cs.ReadResource(ctx, &sdkmcp.ReadResourceParams{URI: uri})
	if err != nil {
		t.Fatalf("ReadResource(%s): %v", uri, err)
	}
	if len(res.Contents) != 1 {
		t.Fatalf("Contents=%d; want 1", len(res.Contents))
	}
	c := res.Contents[0]
	if c.MIMEType != "application/json" {
		t.Errorf("MIMEType=%q; want application/json", c.MIMEType)
	}
	if c.URI != uri {
		t.Errorf("Contents[0].URI=%q; want %q", c.URI, uri)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(c.Text), &payload); err != nil {
		t.Fatalf("payload not JSON: %v; raw=%s", err, c.Text)
	}
	return payload
}

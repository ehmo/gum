package plugins_test

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

	"github.com/ehmo/gum/internal/output/jcs"
	"github.com/ehmo/gum/internal/plugins"
	"github.com/ehmo/gum/internal/plugins/registry"
)

// schemaPluginSource writes a minimal mcp-plugin source tree under a fresh
// temp dir: manifest.json, an executable stub, and zero or more schema
// bundles. bundles maps `<schema_ref>.json` basenames to the bundle body.
func schemaPluginSource(t *testing.T, pluginID, schemaRef string, bundles map[string]any) string {
	t.Helper()
	dir := t.TempDir()

	tool := map[string]any{
		"name":        "search",
		"description": "Search something",
		"risk_class":  "read",
	}
	if schemaRef != "" {
		tool["schema_ref"] = schemaRef
	}
	manifest := map[string]any{
		"manifest_schema_version": 1,
		"plugin_id":               pluginID,
		"name":                    pluginID,
		"version":                 "0.1.0",
		"namespace_owner":         "io.example." + pluginID,
		"shape":                   "mcp-plugin",
		"executable":              "executable",
		"advertised_tools":        []any{tool},
		"declared_capabilities": map[string]any{
			"network":      true,
			"fs_write_dir": "",
			"env_allow":    []any{},
		},
	}
	raw, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), raw, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "executable"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write executable: %v", err)
	}

	if len(bundles) > 0 {
		schemaDir := filepath.Join(dir, "schemas")
		if err := os.MkdirAll(schemaDir, 0o755); err != nil {
			t.Fatalf("mkdir schemas: %v", err)
		}
		for name, body := range bundles {
			b, err := json.Marshal(body)
			if err != nil {
				t.Fatalf("marshal bundle %s: %v", name, err)
			}
			if err := os.WriteFile(filepath.Join(schemaDir, name), b, 0o644); err != nil {
				t.Fatalf("write bundle %s: %v", name, err)
			}
		}
	}
	return dir
}

// schemaBundle is a JSON Schema 2020-12 document carrying the two $defs the
// contract requires. `marker` varies the request body so two plugins can
// declare the same ref with divergent digests.
func schemaBundle(marker string) map[string]any {
	return map[string]any{
		"$schema": "https://json-schema.org/draft/2020-12/schema",
		"$defs": map[string]any{
			"request": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{"type": "string", "title": marker},
				},
				"required": []any{"query"},
			},
			"response": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"results": map[string]any{"type": "array"},
				},
			},
		},
	}
}

func installSchemaPlugin(t *testing.T, profileDir, source string) (*registry.Registry, error) {
	t.Helper()
	reg := registry.New(profileDir)
	host := plugins.NewHost(plugins.HostConfig{InstallRoot: t.TempDir()})
	_, err := host.InstallWithRegistry(context.Background(), source, plugins.InstallOptions{Registry: reg})
	return reg, err
}

// variantForOwner returns the single plugin-catalog variant row owned by
// pluginID.
func variantForOwner(t *testing.T, reg *registry.Registry, pluginID string) map[string]any {
	t.Helper()
	files, err := reg.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	for _, raw := range files.Catalog.Variants {
		row, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if owner, _ := row["owner_plugin"].(string); owner == pluginID {
			return row
		}
	}
	t.Fatalf("no plugin-catalog variant owned by %q", pluginID)
	return nil
}

// TestPluginSchemaBundleMaterialization pins docs/test-matrix.md:
// one manifest `schema_ref` whose bundle carries `$defs.request` and
// `$defs.response` materializes `request_ref=<schema_ref>.request` and
// `response_ref=<schema_ref>.response`, the served bodies are the JCS
// canonicalization of each subdocument, and the recorded hash is that body's
// SHA-256.
func TestPluginSchemaBundleMaterialization(t *testing.T) {
	bundle := schemaBundle("alpha")
	src := schemaPluginSource(t, "alpha", "alpha.v1", map[string]any{"alpha.v1.json": bundle})
	profileDir := t.TempDir()

	reg, err := installSchemaPlugin(t, profileDir, src)
	if err != nil {
		t.Fatalf("InstallWithRegistry: %v", err)
	}

	row := variantForOwner(t, reg, "alpha")
	binding, _ := row["binding"].(map[string]any)
	if got, _ := binding["request_ref"].(string); got != "alpha.v1.request" {
		t.Errorf("binding.request_ref = %q; want alpha.v1.request", got)
	}
	if got, _ := binding["response_ref"].(string); got != "alpha.v1.response" {
		t.Errorf("binding.response_ref = %q; want alpha.v1.response", got)
	}

	hashes, ok := row["schema_hashes"].(map[string]any)
	if !ok {
		t.Fatalf("variant has no schema_hashes object: %#v", row)
	}
	if len(hashes) != 2 {
		t.Errorf("schema_hashes entries = %d; want 2 (%v)", len(hashes), hashes)
	}

	defs, _ := bundle["$defs"].(map[string]any)
	for part, ref := range map[string]string{
		"request":  "alpha.v1.request",
		"response": "alpha.v1.response",
	} {
		hash, _ := hashes[ref].(string)
		if len(hash) != 64 {
			t.Fatalf("schema_hashes[%q] = %q; want a 64-char hex digest", ref, hash)
		}

		path := filepath.Join(profileDir, "plugin-schemas", ref+"."+hash+".json")
		stored, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read served schema %s: %v", ref, err)
		}

		want, err := jcs.Marshal(defs[part])
		if err != nil {
			t.Fatalf("canonicalize $defs.%s: %v", part, err)
		}
		if string(stored) != string(want) {
			t.Errorf("stored %s body = %s; want the JCS-canonical $defs.%s %s", ref, stored, part, want)
		}
		sum := sha256.Sum256(stored)
		if got := hex.EncodeToString(sum[:]); got != hash {
			t.Errorf("sha256(stored %s) = %s; want the recorded hash %s", ref, got, hash)
		}
	}
}

// TestPluginSchemaBundleMissingDefs pins the bundle row's rejection half: a bundle
// without an object-valued `$defs.response` (or `$defs` at all) fails install
// with PLUGIN_SCHEMA_REF_INVALID before anything reaches the registry.
func TestPluginSchemaBundleMissingDefs(t *testing.T) {
	cases := []struct {
		name   string
		bundle map[string]any
	}{
		{"no $defs", map[string]any{"type": "object"}},
		{"$defs not an object", map[string]any{"$defs": "nope"}},
		{"no request def", map[string]any{"$defs": map[string]any{
			"response": map[string]any{"type": "object"},
		}}},
		{"no response def", map[string]any{"$defs": map[string]any{
			"request": map[string]any{"type": "object"},
		}}},
		{"response def not an object", map[string]any{"$defs": map[string]any{
			"request":  map[string]any{"type": "object"},
			"response": []any{"array"},
		}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := schemaPluginSource(t, "alpha", "alpha.v1", map[string]any{"alpha.v1.json": tc.bundle})
			profileDir := t.TempDir()

			reg, err := installSchemaPlugin(t, profileDir, src)
			if !errors.Is(err, plugins.ErrSchemaRefInvalid) {
				t.Fatalf("InstallWithRegistry err = %v; want ErrSchemaRefInvalid", err)
			}

			files, loadErr := reg.Load()
			if loadErr != nil {
				t.Fatalf("Load: %v", loadErr)
			}
			if got := len(files.Catalog.Variants); got != 0 {
				t.Errorf("plugin-catalog variants = %d after rejected install; want 0", got)
			}
			if _, statErr := os.Stat(filepath.Join(profileDir, "plugin-schemas")); !os.IsNotExist(statErr) {
				t.Errorf("schema store created for a rejected install: err=%v", statErr)
			}
		})
	}
}

// TestPluginSchemaRefThirdPartyInstall pins docs/test-matrix.md for the
// runtime third-party install path: unsafe refs are rejected before any path
// is constructed, and a missing bundle file fails the same way.
func TestPluginSchemaRefThirdPartyInstall(t *testing.T) {
	cases := []struct {
		name string
		ref  string
	}{
		{"path separator", "alpha/v1"},
		{"traversal", "../alpha"},
		{"traversal inside segment", "al..pha"},
		{"percent-encoded separator", "alpha%2fv1"},
		{"backslash separator", `alpha\v1`},
		{"uppercase", "Alpha.v1"},
		{"leading dot", ".alpha"},
		{"control character", "alpha\x00v1"},
		{"too long", "a" + strings.Repeat("b", 128)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if plugins.IsSafeServedRef(tc.ref) {
				t.Fatalf("IsSafeServedRef(%q) = true; want false", tc.ref)
			}
			// The bundle body is well-formed, so only the ref grammar can
			// make this install fail.
			src := schemaPluginSource(t, "alpha", tc.ref, map[string]any{"alpha.v1.json": schemaBundle("alpha")})
			profileDir := t.TempDir()

			reg, err := installSchemaPlugin(t, profileDir, src)
			if !errors.Is(err, plugins.ErrSchemaRefInvalid) {
				t.Fatalf("InstallWithRegistry err = %v; want ErrSchemaRefInvalid", err)
			}
			files, loadErr := reg.Load()
			if loadErr != nil {
				t.Fatalf("Load: %v", loadErr)
			}
			if got := len(files.Catalog.Variants); got != 0 {
				t.Errorf("plugin-catalog variants = %d after rejected install; want 0", got)
			}
		})
	}

	t.Run("missing bundle file", func(t *testing.T) {
		src := schemaPluginSource(t, "alpha", "alpha.v1", nil)
		_, err := installSchemaPlugin(t, t.TempDir(), src)
		if !errors.Is(err, plugins.ErrSchemaRefInvalid) {
			t.Fatalf("InstallWithRegistry err = %v; want ErrSchemaRefInvalid", err)
		}
	})

	t.Run("no schema_ref materializes nothing", func(t *testing.T) {
		src := schemaPluginSource(t, "alpha", "", nil)
		profileDir := t.TempDir()
		reg, err := installSchemaPlugin(t, profileDir, src)
		if err != nil {
			t.Fatalf("InstallWithRegistry: %v", err)
		}
		row := variantForOwner(t, reg, "alpha")
		if _, ok := row["schema_hashes"]; ok {
			t.Errorf("variant carries schema_hashes for a tool with no schema_ref: %#v", row)
		}
		binding, _ := row["binding"].(map[string]any)
		if _, ok := binding["request_ref"]; ok {
			t.Errorf("binding carries request_ref for a tool with no schema_ref: %#v", binding)
		}
		if _, err := os.Stat(filepath.Join(profileDir, "plugin-schemas")); !os.IsNotExist(err) {
			t.Errorf("schema store created with nothing to store: err=%v", err)
		}
	})
}

// TestPluginSchemaRefCrossPluginCollision pins the collision
// half: a second plugin claiming a ref already in the profile inventory under
// a different JCS digest fails with SCHEMA_REF_COLLISION, while an identical
// body is reused without error.
func TestPluginSchemaRefCrossPluginCollision(t *testing.T) {
	profileDir := t.TempDir()

	first := schemaPluginSource(t, "alpha", "shared.v1", map[string]any{"shared.v1.json": schemaBundle("alpha")})
	if _, err := installSchemaPlugin(t, profileDir, first); err != nil {
		t.Fatalf("install alpha: %v", err)
	}

	divergent := schemaPluginSource(t, "beta", "shared.v1", map[string]any{"shared.v1.json": schemaBundle("beta")})
	reg, err := installSchemaPlugin(t, profileDir, divergent)
	if !errors.Is(err, plugins.ErrSchemaRefCollision) {
		t.Fatalf("divergent install err = %v; want ErrSchemaRefCollision", err)
	}
	files, err := reg.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := len(files.Catalog.Variants); got != 1 {
		t.Errorf("plugin-catalog variants = %d after rejected collision; want 1 (alpha only)", got)
	}

	identical := schemaPluginSource(t, "beta", "shared.v1", map[string]any{"shared.v1.json": schemaBundle("alpha")})
	if _, err := installSchemaPlugin(t, profileDir, identical); err != nil {
		t.Fatalf("identical-body reuse must be allowed, got: %v", err)
	}
}

// TestPluginSchemaRefReinstallIsNotSelfCollision pins the owner-exclusion in
// ValidateNewPluginSchemas: a plugin changing its own schema body must not
// collide with the rows the same install is about to replace.
func TestPluginSchemaRefReinstallIsNotSelfCollision(t *testing.T) {
	profileDir := t.TempDir()

	first := schemaPluginSource(t, "alpha", "alpha.v1", map[string]any{"alpha.v1.json": schemaBundle("v1")})
	if _, err := installSchemaPlugin(t, profileDir, first); err != nil {
		t.Fatalf("install alpha: %v", err)
	}

	second := schemaPluginSource(t, "alpha", "alpha.v1", map[string]any{"alpha.v1.json": schemaBundle("v2")})
	reg, err := installSchemaPlugin(t, profileDir, second)
	if err != nil {
		t.Fatalf("reinstall with a changed schema body: %v", err)
	}

	row := variantForOwner(t, reg, "alpha")
	hashes, _ := row["schema_hashes"].(map[string]any)
	got, _ := hashes["alpha.v1.request"].(string)

	defs, _ := schemaBundle("v2")["$defs"].(map[string]any)
	body, err := jcs.Marshal(defs["request"])
	if err != nil {
		t.Fatalf("canonicalize: %v", err)
	}
	sum := sha256.Sum256(body)
	if want := hex.EncodeToString(sum[:]); got != want {
		t.Errorf("recorded request hash = %s; want the reinstalled body's %s", got, want)
	}
}

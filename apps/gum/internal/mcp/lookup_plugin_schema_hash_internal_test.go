package mcp

import "testing"

// TestLookupPluginSchemaHashBranches drives every continue/return path
// of the variant scanner so a refactor that silently changes precedence
// (or surfaces an ownerless row) is caught.
func TestLookupPluginSchemaHashBranches(t *testing.T) {
	t.Run("happy_path_returns_hash_owner_true", func(t *testing.T) {
		top := map[string]any{
			"variants": []any{
				map[string]any{
					"owner_plugin":  "acme.maps",
					"schema_hashes": map[string]any{"places.search.input": "sha256:abc"},
				},
			},
		}
		h, owner, ok := lookupPluginSchemaHash(top, "places.search.input")
		if !ok || h != "sha256:abc" || owner != "acme.maps" {
			t.Errorf("got (%q, %q, %v); want (sha256:abc, acme.maps, true)", h, owner, ok)
		}
	})

	t.Run("non_map_row_skipped", func(t *testing.T) {
		top := map[string]any{
			"variants": []any{
				"not-a-map",
				map[string]any{
					"owner_plugin":  "acme.maps",
					"schema_hashes": map[string]any{"r": "h"},
				},
			},
		}
		h, _, ok := lookupPluginSchemaHash(top, "r")
		if !ok || h != "h" {
			t.Errorf("got (%q, _, %v); want (h, _, true)", h, ok)
		}
	})

	t.Run("missing_schema_hashes_skipped", func(t *testing.T) {
		top := map[string]any{
			"variants": []any{
				map[string]any{"owner_plugin": "x"}, // no schema_hashes
				map[string]any{
					"owner_plugin":  "y",
					"schema_hashes": map[string]any{"r": "h"},
				},
			},
		}
		_, owner, ok := lookupPluginSchemaHash(top, "r")
		if !ok || owner != "y" {
			t.Errorf("owner=%q ok=%v; want (y, true)", owner, ok)
		}
	})

	t.Run("missing_or_blank_hash_skipped", func(t *testing.T) {
		top := map[string]any{
			"variants": []any{
				map[string]any{
					"owner_plugin":  "x",
					"schema_hashes": map[string]any{"r": ""}, // blank
				},
				map[string]any{
					"owner_plugin":  "y",
					"schema_hashes": map[string]any{"r": 42}, // wrong type
				},
				map[string]any{
					"owner_plugin":  "z",
					"schema_hashes": map[string]any{"r": "sha256:final"},
				},
			},
		}
		h, owner, ok := lookupPluginSchemaHash(top, "r")
		if !ok || h != "sha256:final" || owner != "z" {
			t.Errorf("got (%q, %q, %v); want (sha256:final, z, true)", h, owner, ok)
		}
	})

	t.Run("blank_owner_skipped", func(t *testing.T) {
		top := map[string]any{
			"variants": []any{
				map[string]any{
					// owner_plugin missing → skipped, even though hash present.
					"schema_hashes": map[string]any{"r": "h1"},
				},
				map[string]any{
					"owner_plugin":  "claimed",
					"schema_hashes": map[string]any{"r": "h2"},
				},
			},
		}
		h, owner, ok := lookupPluginSchemaHash(top, "r")
		if !ok || h != "h2" || owner != "claimed" {
			t.Errorf("got (%q, %q, %v); want (h2, claimed, true)", h, owner, ok)
		}
	})

	t.Run("no_match_returns_false", func(t *testing.T) {
		top := map[string]any{
			"variants": []any{
				map[string]any{
					"owner_plugin":  "x",
					"schema_hashes": map[string]any{"other": "h"},
				},
			},
		}
		_, _, ok := lookupPluginSchemaHash(top, "missing.ref")
		if ok {
			t.Errorf("want (_, _, false) for unknown ref")
		}
	})

	t.Run("missing_variants_key_returns_false", func(t *testing.T) {
		_, _, ok := lookupPluginSchemaHash(map[string]any{}, "r")
		if ok {
			t.Errorf("want false for empty top")
		}
	})
}

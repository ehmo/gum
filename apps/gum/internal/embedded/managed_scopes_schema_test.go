package embedded_test

import (
	"encoding/json"
	"testing"

	"github.com/ehmo/gum/internal/embedded"
	"github.com/google/jsonschema-go/jsonschema"
)

// TestManagedScopeManifestSchema is the spec §7 line 1454 build gate:
// data/auth-managed-scopes.v1.json MUST validate against
// data/auth-managed-scopes.v1.schema.json (JSON Schema 2020-12). The gate runs
// on every `go test ./...`, so an invalid manifest fails CI before a release
// build regenerates the catalog.
//
// The subtests after the first one mutate an in-memory copy of the manifest to
// prove the schema's §7 promotion rule actually bites: a scope with
// status="active" must carry verification_state="verified",
// project_evidence_state="ready", live_canary_state="passing" and non-empty
// evidence. Without those mutations the test would pass against a schema whose
// conditional block had been deleted.
func TestManagedScopeManifestSchema(t *testing.T) {
	resolved := resolveManagedScopeSchema(t)
	manifest := decodeManagedScopeManifest(t)

	t.Run("the shipped manifest validates", func(t *testing.T) {
		if err := resolved.Validate(manifest); err != nil {
			t.Fatalf("shipped manifest fails its own schema: %v", err)
		}
	})

	// Vacuity guard. The promotion rule is conditional on status="active", so
	// a manifest with no active scope would make every mutation below moot.
	activeIdx := -1
	for i, entry := range manifestScopes(t, manifest) {
		if entry["status"] == "active" {
			activeIdx = i
			break
		}
	}
	if activeIdx < 0 {
		t.Fatal("manifest declares no active scope; the §7 promotion rule below would be vacuous")
	}

	t.Run("an active scope that regresses fails the promotion rule", func(t *testing.T) {
		cases := []struct {
			field string
			value string
		}{
			{"verification_state", "pending"},
			{"verification_state", "expired"},
			{"project_evidence_state", "pending"},
			{"live_canary_state", "pending"},
			{"live_canary_state", "failing"},
			{"evidence", ""},
		}
		for _, tc := range cases {
			t.Run(tc.field+"="+tc.value, func(t *testing.T) {
				doc := cloneManifest(t, manifest)
				manifestScopes(t, doc)[activeIdx][tc.field] = tc.value
				if err := resolved.Validate(doc); err == nil {
					t.Fatalf("active scope with %s=%q accepted; §7 requires the promoted terminal value",
						tc.field, tc.value)
				}
			})
		}
	})

	t.Run("a planned scope promoted without evidence is rejected", func(t *testing.T) {
		doc := cloneManifest(t, manifest)
		scopes := manifestScopes(t, doc)
		promoted := -1
		for i, entry := range scopes {
			if entry["status"] == "planned" {
				promoted = i
				break
			}
		}
		if promoted < 0 {
			t.Skip("manifest has no planned scope to promote")
		}
		scopes[promoted]["status"] = "active"
		if err := resolved.Validate(doc); err == nil {
			t.Fatal("planned scope flipped to active with pending states accepted; §7 forbids it")
		}
	})

	t.Run("structural violations are rejected", func(t *testing.T) {
		mutations := []struct {
			name  string
			apply func(doc map[string]any, scopes []map[string]any)
		}{
			{"unknown root key", func(doc map[string]any, _ []map[string]any) {
				doc["surprise"] = true
			}},
			{"unknown scope key", func(_ map[string]any, scopes []map[string]any) {
				scopes[activeIdx]["surprise"] = true
			}},
			{"future schema_version", func(doc map[string]any, _ []map[string]any) {
				doc["schema_version"] = 2
			}},
			{"non-googleapis scope host", func(_ map[string]any, scopes []map[string]any) {
				scopes[activeIdx]["scope"] = "https://www.googleapis.mil/auth/gmail.readonly"
			}},
			{"unknown status", func(_ map[string]any, scopes []map[string]any) {
				scopes[activeIdx]["status"] = "retired"
			}},
			{"unknown publishing_status", func(doc map[string]any, _ []map[string]any) {
				project, ok := doc["managed_project"].(map[string]any)
				if !ok {
					t.Fatal("managed_project is not an object")
				}
				project["publishing_status"] = "shipped"
			}},
			{"embedded client secret", func(doc map[string]any, _ []map[string]any) {
				policy, ok := doc["client_policy"].(map[string]any)
				if !ok {
					t.Fatal("client_policy is not an object")
				}
				policy["embedded_client_secret"] = true
			}},
			{"missing required scope field", func(_ map[string]any, scopes []map[string]any) {
				delete(scopes[activeIdx], "live_canary_state")
			}},
		}
		for _, m := range mutations {
			t.Run(m.name, func(t *testing.T) {
				doc := cloneManifest(t, manifest)
				m.apply(doc, manifestScopes(t, doc))
				if err := resolved.Validate(doc); err == nil {
					t.Fatalf("%s accepted; the schema must reject it", m.name)
				}
			})
		}
	})
}

// resolveManagedScopeSchema compiles the embedded manifest schema.
func resolveManagedScopeSchema(t *testing.T) *jsonschema.Resolved {
	t.Helper()
	var schema jsonschema.Schema
	if err := json.Unmarshal(embedded.AuthManagedScopesSchemaJSON, &schema); err != nil {
		t.Fatalf("parse managed-scope schema: %v", err)
	}
	resolved, err := schema.Resolve(nil)
	if err != nil {
		t.Fatalf("compile managed-scope schema: %v", err)
	}
	return resolved
}

// decodeManagedScopeManifest decodes the embedded manifest into the generic
// shape the validator consumes.
func decodeManagedScopeManifest(t *testing.T) map[string]any {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal(embedded.AuthManagedScopesJSON, &doc); err != nil {
		t.Fatalf("parse managed-scope manifest: %v", err)
	}
	return doc
}

// cloneManifest deep-copies a decoded manifest so a mutation cannot leak into
// the next subtest.
func cloneManifest(t *testing.T, doc map[string]any) map[string]any {
	t.Helper()
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("clone manifest: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("clone manifest: %v", err)
	}
	return out
}

// manifestScopes returns the scopes array as addressable objects.
func manifestScopes(t *testing.T, doc map[string]any) []map[string]any {
	t.Helper()
	raw, ok := doc["scopes"].([]any)
	if !ok {
		t.Fatal("manifest scopes is not an array")
	}
	out := make([]map[string]any, 0, len(raw))
	for i, entry := range raw {
		obj, ok := entry.(map[string]any)
		if !ok {
			t.Fatalf("scopes[%d] is not an object", i)
		}
		out = append(out, obj)
	}
	return out
}

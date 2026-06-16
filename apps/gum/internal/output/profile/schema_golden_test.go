package profile_test

import (
	"encoding/json"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/ehmo/gum/internal/embedded"
	"github.com/ehmo/gum/internal/testutil/golden"
)

// TestDSLSchemaCanonicalJSONGolden pins the canonical JSON form of the
// embedded expression-profile DSL schema (`docs/expression-profile-dsl.json`)
// under testdata/golden/schema/ via the shared golden helper (gum-b22o.2).
//
// This is the v0.1.0 representative "output schema" golden: a drift detector
// at the schema-bytes level (the existing TestDSLSchemaDoesNotDrift covers
// the source<->embedded byte-equality; this golden covers the post-Unmarshal
// canonical re-emit, catching changes that would otherwise pass through
// Go's whitespace-insensitive Unmarshal).
func TestDSLSchemaCanonicalJSONGolden(t *testing.T) {
	var tree any
	if err := json.Unmarshal(embedded.ExpressionProfileDSLJSON, &tree); err != nil {
		t.Fatalf("unmarshal embedded DSL schema: %v", err)
	}
	canon, err := json.MarshalIndent(tree, "", "  ")
	if err != nil {
		t.Fatalf("marshal canonical: %v", err)
	}
	canon = append(canon, '\n')
	golden.Bytes(t, schemaGoldenPath(t, "expression-profile-dsl.canonical.json"), canon)
}

func schemaGoldenPath(t *testing.T, name string) string {
	t.Helper()
	_, thisFile, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(thisFile), "testdata", "golden", "schema", name)
}

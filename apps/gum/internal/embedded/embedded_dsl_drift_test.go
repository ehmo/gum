package embedded_test

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/ehmo/gum/internal/embedded"
)

// TestEmbeddedExpressionProfileDSLMatchesDocs is the drift gate ensuring
// internal/embedded/data/expression-profile-dsl.json is byte-identical to
// docs/expression-profile-dsl.json (the normative source).
func TestEmbeddedExpressionProfileDSLMatchesDocs(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	docPath := filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "..", "docs", "expression-profile-dsl.json")
	want, err := os.ReadFile(docPath)
	if err != nil {
		t.Fatalf("read canonical schema at %s: %v", docPath, err)
	}
	if !bytes.Equal(embedded.ExpressionProfileDSLJSON, want) {
		t.Fatalf("internal/embedded/data/expression-profile-dsl.json drifted from docs/expression-profile-dsl.json — re-sync (overwrite the embedded copy with the docs copy)")
	}
}

package profile

import (
	"strings"
	"sync"
	"testing"

	"github.com/ehmo/gum/internal/embedded"
)

// resetDSLSchemaState resets the sync.Once + cached results so a test
// can exercise the compileDSLSchema body again. t.Cleanup restores
// state so later tests still see the real embedded schema.
func resetDSLSchemaState(t *testing.T) {
	t.Helper()
	savedResolved, savedErr := dslSchemaResolved, dslSchemaErr
	savedBlob := embedded.ExpressionProfileDSLJSON
	t.Cleanup(func() {
		embedded.ExpressionProfileDSLJSON = savedBlob
		dslSchemaOnce = sync.Once{}
		dslSchemaResolved = savedResolved
		dslSchemaErr = savedErr
		if dslSchemaResolved == nil && dslSchemaErr == nil {
			_, _ = compileDSLSchema()
		} else {
			dslSchemaOnce.Do(func() {})
		}
	})
	dslSchemaOnce = sync.Once{}
	dslSchemaResolved = nil
	dslSchemaErr = nil
}

// TestCompileDSLSchemaUnmarshalErrorPropagates pins the
// json.Unmarshal error arm of compileDSLSchema: a corrupt embed (non-
// JSON bytes) MUST surface 'parse embedded DSL schema' wrap, not a
// silently-nil Resolved that downstream Validate calls would NPE on.
func TestCompileDSLSchemaUnmarshalErrorPropagates(t *testing.T) {
	resetDSLSchemaState(t)
	embedded.ExpressionProfileDSLJSON = []byte("{not valid json")

	_, err := compileDSLSchema()
	if err == nil {
		t.Fatal("want unmarshal error; got nil")
	}
	if !strings.Contains(err.Error(), "parse embedded DSL schema") {
		t.Errorf("err=%v; want 'parse embedded DSL schema' wrap", err)
	}
}

// TestCompileDSLSchemaResolveErrorPropagates pins the s.Resolve error
// arm: a JSON-syntactically-valid schema that refers to an undefined
// $ref (no $defs entry) MUST surface 'compile embedded DSL schema'
// wrap from Resolve. Without this, an upstream typo in the embedded
// schema would silently fail to enforce constraints at runtime.
func TestCompileDSLSchemaResolveErrorPropagates(t *testing.T) {
	resetDSLSchemaState(t)
	// $ref to "#/$defs/notDefined" — parses fine, fails Resolve.
	embedded.ExpressionProfileDSLJSON = []byte(`{
		"$schema": "https://json-schema.org/draft/2020-12/schema",
		"$ref": "#/$defs/notDefined"
	}`)

	_, err := compileDSLSchema()
	if err == nil {
		t.Fatal("want resolve error; got nil")
	}
	if !strings.Contains(err.Error(), "compile embedded DSL schema") {
		t.Errorf("err=%v; want 'compile embedded DSL schema' wrap", err)
	}
}

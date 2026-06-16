package fieldmask_test

import (
	"testing"

	"github.com/ehmo/gum/internal/output/fieldmask"
)

// TestProjectValueScalarUnderNestedMaskReturnsNil pins the `default →
// return nil` arm of projectValue. When the field mask has children
// (i.e. the operator asked for a nested projection like `user.name`)
// but the source body has a scalar at that path instead of a map or
// array, the function MUST surface nil rather than panicking or
// preserving the scalar — the operator's projection contract said
// they wanted nested fields; surfacing the scalar would silently
// re-shape the response in a way the projection didn't promise.
func TestProjectValueScalarUnderNestedMaskReturnsNil(t *testing.T) {
	mask, err := fieldmask.Parse("user(name)")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	got := mask.Project(map[string]any{
		"user": "alice", // scalar where mask expects a nested object
	})
	// The output MUST contain the "user" key (mask explicitly asked
	// for it) but with a nil value (default branch of projectValue).
	v, ok := got["user"]
	if !ok {
		t.Fatalf("output missing 'user' key: %v", got)
	}
	if v != nil {
		t.Errorf("user=%v (%T); want nil (default arm drops scalars under nested mask)", v, v)
	}
}

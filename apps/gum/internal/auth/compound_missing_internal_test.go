package auth

import (
	"reflect"
	"testing"

	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/dispatch"
)

// TestCompoundMissingComponentsBranches covers the fallback arms: a nil
// resolved variant, a nil variant, a variant that declares no
// auth_components, and one whose only component has an empty kind. Each
// must reach the "see_setup_command" marker, which is what keeps spec §7's
// "MUST include missing_components" true on an under-declared variant.
// The declared-taxonomy path is in compound_components_test.go.
func TestCompoundMissingComponentsBranches(t *testing.T) {
	want := []string{"see_setup_command"}

	if got := compoundMissingComponents(nil); !reflect.DeepEqual(got, want) {
		t.Errorf("nil rv: got %v; want %v", got, want)
	}
	if got := compoundMissingComponents(&dispatch.ResolvedVariant{}); !reflect.DeepEqual(got, want) {
		t.Errorf("nil Variant: got %v; want %v", got, want)
	}
	rv := &dispatch.ResolvedVariant{Variant: &catalog.Variant{VariantID: "anything"}}
	if got := compoundMissingComponents(rv); !reflect.DeepEqual(got, want) {
		t.Errorf("no declared components: got %v; want %v", got, want)
	}
	empty := &dispatch.ResolvedVariant{Variant: &catalog.Variant{
		VariantID:      "anything",
		AuthComponents: []catalog.AuthComponent{{Kind: ""}},
	}}
	if got := compoundMissingComponents(empty); !reflect.DeepEqual(got, want) {
		t.Errorf("empty component kind: got %v; want %v", got, want)
	}
}

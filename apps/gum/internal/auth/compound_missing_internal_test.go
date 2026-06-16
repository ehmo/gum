package auth

import (
	"reflect"
	"testing"

	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/dispatch"
)

// TestCompoundMissingComponentsBranches covers the three nil-guard
// outcomes plus the populated-variant happy path. The function is the
// per-variant compound-auth marker the envelope serializer reads — until
// the catalog ABI exposes a real component list (spec §7 line 1296) all
// branches return the placeholder sentinel.
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
		t.Errorf("populated rv: got %v; want %v", got, want)
	}
}

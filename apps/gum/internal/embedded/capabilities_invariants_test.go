package embedded_test

import (
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/catalog"
)

// curatedCapabilityVariants pins the shipped artifact against the curated table
// in apps/gum/cmd/gen-catalog/capabilities.go, the same two-sided pin
// TestCatalogCarriesDefaultFields uses. The generator derives the atoms of 225
// variants from their request records; these are the variants whose declared
// atoms contradict what the adapters can run, so a human wrote them. Adding a
// curated entry and forgetting to rerun `gen-catalog -apply-capabilities` fails
// here.
var curatedCapabilityVariants = map[string]struct {
	support     catalog.ExecutionSupport
	unsupported []string
}{
	"drive.v3.rest.files.export": {
		support:     catalog.ExecutionSupportPartial,
		unsupported: []string{catalog.CapabilityMediaDownload},
	},
	"drive.v3.rest.files.get": {
		support:     catalog.ExecutionSupportPartial,
		unsupported: []string{catalog.CapabilityMediaDownload},
	},
}

// TestCatalogDeclaresCapabilities is the §5.8 shipped-artifact invariant.
// `capabilities[]` drives the §5.8 dispatch gate, the `lro_return` code-mode
// refusal, and describe_op's execution_support. A variant that declares no atom
// tells all three that it needs nothing, which is never true of a real op.
func TestCatalogDeclaresCapabilities(t *testing.T) {
	cat := loadEmbeddedCatalog(t)

	var bare []string
	for _, op := range cat.Ops {
		for _, v := range op.Variants {
			if len(v.Capabilities) == 0 {
				bare = append(bare, v.VariantID)
				continue
			}
			for _, atom := range v.Capabilities {
				if !catalog.CapabilityKnown(atom) {
					t.Errorf("variant %s declares %q, which is outside the §5.8 enum", v.VariantID, atom)
				}
			}
		}
	}
	if len(bare) > 0 {
		sort.Strings(bare)
		t.Fatalf("%d variant(s) declare no capabilities; run `gen-catalog -apply-capabilities`:\n%s",
			len(bare), strings.Join(bare, "\n"))
	}
}

// TestCatalogCuratedExecutionSupport holds the other half: exactly the curated
// variants carry a non-`full` execution_support, and they carry the list §5.8
// binds to it. A derived variant that drifts to `partial` would silently stop
// dispatching part of its work with no one having decided that.
func TestCatalogCuratedExecutionSupport(t *testing.T) {
	cat := loadEmbeddedCatalog(t)

	seen := map[string]bool{}
	for _, op := range cat.Ops {
		for _, v := range op.Variants {
			want, curated := curatedCapabilityVariants[v.VariantID]
			if !curated {
				if v.ExecutionSupport != "" && v.ExecutionSupport != catalog.ExecutionSupportFull {
					t.Errorf("variant %s carries execution_support %q but is not in the curated table",
						v.VariantID, v.ExecutionSupport)
				}
				continue
			}
			seen[v.VariantID] = true

			if v.ExecutionSupport != want.support {
				t.Errorf("variant %s: execution_support = %q, want %q", v.VariantID, v.ExecutionSupport, want.support)
			}
			if !slices.Equal(v.UnsupportedCapabilities, want.unsupported) {
				t.Errorf("variant %s: unsupported_capabilities = %v, want %v",
					v.VariantID, v.UnsupportedCapabilities, want.unsupported)
			}
			for _, atom := range want.unsupported {
				if !slices.Contains(v.Capabilities, atom) {
					t.Errorf("variant %s: blocks %q without declaring it in capabilities[]", v.VariantID, atom)
				}
			}
		}
	}
	for id := range curatedCapabilityVariants {
		if !seen[id] {
			t.Errorf("curated variant %s is not in the shipped catalog", id)
		}
	}
}

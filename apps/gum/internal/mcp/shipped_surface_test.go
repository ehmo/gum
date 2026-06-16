// Package mcp_test — shipped-surface coverage (gum-hms7).
//
// The gum.code P0 (gum-7ras) shipped a DEAD flagship tool: the MCP server
// advertised gum.code, but the embedded catalog the released binary loads had
// no gum.code op, so every call returned OP_NOT_FOUND. The existing tests
// missed it because the MCP roundtrip test loaded testdata/kernel-catalog.json
// (which DOES contain gum.code) instead of the embedded snapshot, and the CLI
// branch test discarded the dispatch error.
//
// This tier closes that gap for the whole tool surface: every advertised tool
// that dispatches to a FIXED catalog op_id (gum.code + the 18 Tier A
// convenience tools) MUST have its backing op present in the EMBEDDED catalog
// with a resolvable default variant. If any advertised tool's op falls out of
// the shipped catalog again, this fails loudly at op-lookup.
package mcp_test

import (
	"encoding/json"
	"testing"

	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/embedded"
	gummcp "github.com/ehmo/gum/internal/mcp"
)

// loadEmbeddedCatalogForSurface parses the production embedded catalog — the
// exact bytes loadCatalog() reads in the shipped binary.
func loadEmbeddedCatalogForSurface(t *testing.T) *catalog.Catalog {
	t.Helper()
	if len(embedded.CatalogJSON) == 0 {
		t.Skip("no embedded catalog in this build; shipped-surface check is vacuous")
	}
	var c catalog.Catalog
	if err := json.Unmarshal(embedded.CatalogJSON, &c); err != nil {
		t.Fatalf("unmarshal embedded catalog: %v", err)
	}
	return &c
}

// fixedOpBackedTools returns every advertised tool whose dispatch targets a
// fixed catalog op_id: gum.code (hardcoded) plus the 18 convenience tools whose
// op_id comes from the ABI table. Tools that take op_id as a parameter
// (gum.read/write/destructive) or that do not dispatch to the catalog at all
// (gum.search_apis, gum.describe_op, gum.poll, gum.cache_stats, gum.gain) are
// excluded — they have no fixed backing op to verify here.
func fixedOpBackedTools(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{
		"gum.code": "gum.code",
	}
	for _, def := range gummcp.TierAConvenienceToolDefs() {
		abi := gummcp.ConvenienceToolABI(def.Name)
		if abi == nil {
			t.Fatalf("convenience tool %q advertised but has no ABI binding", def.Name)
		}
		if abi.OpID == "" {
			t.Fatalf("convenience tool %q has empty ABI op_id", def.Name)
		}
		out[def.Name] = abi.OpID
	}
	return out
}

// TestAdvertisedToolsResolveInEmbeddedCatalog pins that every advertised tool
// backed by a fixed catalog op resolves past op-lookup in the SHIPPED catalog.
func TestAdvertisedToolsResolveInEmbeddedCatalog(t *testing.T) {
	cat := loadEmbeddedCatalogForSurface(t)

	// Index the embedded catalog: op_id -> op.
	byID := make(map[string]*catalog.Op, len(cat.Ops))
	for i := range cat.Ops {
		byID[cat.Ops[i].OpID] = &cat.Ops[i]
	}

	for tool, opID := range fixedOpBackedTools(t) {
		t.Run(tool, func(t *testing.T) {
			op, ok := byID[opID]
			if !ok {
				t.Fatalf("advertised tool %q dispatches to op %q, but that op is ABSENT from the embedded catalog (OP_NOT_FOUND in the shipped binary)", tool, opID)
			}
			// The op must have a default variant that actually exists, or
			// dispatch fails after op-lookup at variant routing.
			if op.DefaultVariantID == "" {
				t.Fatalf("op %q (tool %q) has no default_variant_id", opID, tool)
			}
			found := false
			for _, v := range op.Variants {
				if v.VariantID == op.DefaultVariantID {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("op %q (tool %q) default_variant_id=%q references no real variant", opID, tool, op.DefaultVariantID)
			}
		})
	}
}

// TestEmbeddedCatalogHasGumCodeMetaOp is the focused guard for the exact P0:
// the embedded catalog must contain the gum.code meta-op wired to the Risor
// adapter with auth_strategy=none. A regression here means the flagship verb is
// dead again.
func TestEmbeddedCatalogHasGumCodeMetaOp(t *testing.T) {
	cat := loadEmbeddedCatalogForSurface(t)

	var code *catalog.Op
	for i := range cat.Ops {
		if cat.Ops[i].OpID == "gum.code" {
			code = &cat.Ops[i]
			break
		}
	}
	if code == nil {
		t.Fatal("embedded catalog is missing the gum.code meta-op")
	}
	if len(code.Variants) == 0 {
		t.Fatal("gum.code op has no variants")
	}
	v := code.Variants[0]
	if v.Binding == nil || v.Binding.AdapterKey != "code.risor" {
		t.Errorf("gum.code default variant adapter_key = %v, want code.risor", v.Binding)
	}
	if v.AuthStrategy != catalog.AuthStrategyNone {
		t.Errorf("gum.code auth_strategy = %q, want none", v.AuthStrategy)
	}
}

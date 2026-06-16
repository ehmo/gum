package mcp

import (
	"context"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/ehmo/gum/internal/catalog"
)

// assertGrammarRejectionDetail checks that err is a jsonrpc.Error whose
// Data field contains "grammar rejected" — the canonical detail string
// emitted by resourceTemplateNotFound on a pre-lookup parse failure.
func assertGrammarRejectionDetail(t *testing.T, err error) {
	t.Helper()
	rpcErr, ok := err.(*jsonrpc.Error)
	if !ok {
		t.Fatalf("err type=%T; want *jsonrpc.Error (got %v)", err, err)
	}
	if !strings.Contains(string(rpcErr.Data), "grammar rejected") {
		t.Errorf("rpcErr.Data=%q; want 'grammar rejected' detail", string(rpcErr.Data))
	}
}

// makeTemplateErrServer returns a Server backed by a noop dispatcher
// and a minimal catalog so the resource-template handlers can be
// invoked directly. The catalog is intentionally empty so post-grammar
// hits go through the inactive-plugin fallback (which also misses)
// → exercises the canonical RESOURCE_NOT_FOUND path.
func makeTemplateErrServer() *Server {
	return NewServerWithCatalog(noopDispatcher{}, &catalog.Catalog{
		CatalogSchemaVersion: 1,
		GeneratorVersion:     "test",
	})
}

// TestHandleOpReadGrammarRejectionBeforeLookup pins handleOpRead's
// `!ok → resourceTemplateNotFound("op_id grammar rejected")` arm
// (resource_templates.go:87-89). Reached when the URI starts with
// `gum://op/` but the trailing segment violates grammar (empty, too
// long, or contains '/?#'). The grammar check sits BEFORE the catalog
// lookup so malformed inputs can't poison the snapshot scan path.
func TestHandleOpReadGrammarRejectionBeforeLookup(t *testing.T) {
	s := makeTemplateErrServer()

	for _, tc := range []struct {
		name string
		uri  string
	}{
		{"empty_tail", "gum://op/"},
		{"contains_slash", "gum://op/foo/bar"},
		{"contains_query", "gum://op/foo?x=1"},
		{"contains_fragment", "gum://op/foo#x"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := &sdkmcp.ReadResourceRequest{
				Params: &sdkmcp.ReadResourceParams{URI: tc.uri},
			}
			res, err := s.handleOpRead(context.Background(), req)
			if err == nil {
				t.Fatalf("handleOpRead(%q)=%+v nil err; want grammar-rejection RESOURCE_NOT_FOUND", tc.uri, res)
			}
			assertGrammarRejectionDetail(t, err)
		})
	}
}

// TestHandleVariantReadGrammarRejectionBeforeLookup pins handleVariantRead's
// `!ok → resourceTemplateNotFound("variant_id grammar rejected")` arm
// (resource_templates.go:114-116). Same grammar guard as handleOpRead,
// distinct branch in the variant handler.
func TestHandleVariantReadGrammarRejectionBeforeLookup(t *testing.T) {
	s := makeTemplateErrServer()
	req := &sdkmcp.ReadResourceRequest{
		Params: &sdkmcp.ReadResourceParams{URI: "gum://variant/with/slash"},
	}
	res, err := s.handleVariantRead(context.Background(), req)
	if err == nil {
		t.Fatalf("handleVariantRead(bad)=%+v nil err; want grammar-rejection", res)
	}
	assertGrammarRejectionDetail(t, err)
}

// TestHandleSchemaReadGrammarRejectionBeforeLookup pins handleSchemaRead's
// `!ok → resourceTemplateNotFound("schema ref grammar rejected")` arm
// (resource_templates.go:158-160). Same guard as the other handlers;
// schema refs use a slightly different decode step downstream so
// grammar rejection MUST happen first.
func TestHandleSchemaReadGrammarRejectionBeforeLookup(t *testing.T) {
	s := makeTemplateErrServer()
	req := &sdkmcp.ReadResourceRequest{
		Params: &sdkmcp.ReadResourceParams{URI: "gum://schema/"}, // empty tail
	}
	res, err := s.handleSchemaRead(context.Background(), req)
	if err == nil {
		t.Fatalf("handleSchemaRead(bad)=%+v nil err; want grammar-rejection", res)
	}
	assertGrammarRejectionDetail(t, err)
}

// TestHandlePluginReadGrammarRejectionBeforeLookup pins handlePluginRead's
// `!ok → resourceTemplateNotFound("plugin name grammar rejected")` arm
// (resource_templates.go:182-183).
func TestHandlePluginReadGrammarRejectionBeforeLookup(t *testing.T) {
	s := makeTemplateErrServer()
	req := &sdkmcp.ReadResourceRequest{
		Params: &sdkmcp.ReadResourceParams{URI: "gum://plugin/foo?bad"},
	}
	res, err := s.handlePluginRead(context.Background(), req)
	if err == nil {
		t.Fatalf("handlePluginRead(bad)=%+v nil err; want grammar-rejection", res)
	}
	assertGrammarRejectionDetail(t, err)
}

// TestFindOpNilCatalogReturnsNil pins findOp's `c == nil → return nil`
// arm (resource_templates.go:223-225). A nil catalog must NOT cause a
// nil-deref crash on the c.Ops range — defensive guard for fresh-
// process startup before the first snapshot loads.
func TestFindOpNilCatalogReturnsNil(t *testing.T) {
	if got := findOp(nil, "any.op"); got != nil {
		t.Errorf("findOp(nil, _)=%+v; want nil", got)
	}
}

// TestFindVariantNilCatalogReturnsNilNil pins findVariant's
// `c == nil → return nil, nil` arm (resource_templates.go:236-238).
// Symmetric guard to findOp; both must tolerate nil snapshot.
func TestFindVariantNilCatalogReturnsNilNil(t *testing.T) {
	op, v := findVariant(nil, "any.variant")
	if op != nil || v != nil {
		t.Errorf("findVariant(nil, _)=(%+v, %+v); want (nil, nil)", op, v)
	}
}

package mcp

import (
	"context"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/ehmo/gum/internal/catalog"
)

// makeHelpServer returns a Server backed by an empty catalog so the
// help-resource handlers can be invoked directly.
func makeHelpServer() *Server {
	return NewServerWithCatalog(noopDispatcher{}, &catalog.Catalog{
		CatalogSchemaVersion: 1,
		GeneratorVersion:     "test",
	})
}

// TestHandleHelpTopicReadGrammarRejectionReturnsResourceNotFound pins
// handleHelpTopicRead's `parseHelpTopicURI !ok → resourceNotFound` arm
// (help_resource.go:104-106). Reached when the URI starts with
// `gum://help/` but the trailing slug violates grammar (empty, embedded
// '/', '?', '#', or non-kebab-lowercase chars). The grammar check sits
// BEFORE the manifest lookup so malformed inputs can't poison the
// embedded-data scan path.
func TestHandleHelpTopicReadGrammarRejectionReturnsResourceNotFound(t *testing.T) {
	s := makeHelpServer()

	for _, tc := range []struct {
		name string
		uri  string
	}{
		{"empty_tail", "gum://help/"},
		{"reserved_topics", "gum://help/topics"},
		{"contains_slash", "gum://help/auth/sub"},
		{"contains_query", "gum://help/auth?x=1"},
		{"contains_fragment", "gum://help/auth#x"},
		{"uppercase", "gum://help/Auth"},
		{"underscore", "gum://help/some_topic"},
		{"wrong_prefix", "gum://other/auth"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := &sdkmcp.ReadResourceRequest{
				Params: &sdkmcp.ReadResourceParams{URI: tc.uri},
			}
			res, err := s.handleHelpTopicRead(context.Background(), req)
			if err == nil {
				t.Fatalf("handleHelpTopicRead(%q)=%+v nil err; want RESOURCE_NOT_FOUND", tc.uri, res)
			}
			if _, ok := err.(*jsonrpc.Error); !ok {
				t.Fatalf("err type=%T; want *jsonrpc.Error", err)
			}
		})
	}
}

// TestHandleHelpTopicReadUnknownTopicReturnsResourceNotFound pins
// handleHelpTopicRead's `findTopicRow !found → resourceNotFound` arm
// (help_resource.go:112-114). Reached when the URI passes grammar but
// the slug doesn't match any row in the embedded manifest — clients
// MUST see RESOURCE_NOT_FOUND rather than an empty-body success.
func TestHandleHelpTopicReadUnknownTopicReturnsResourceNotFound(t *testing.T) {
	s := makeHelpServer()
	req := &sdkmcp.ReadResourceRequest{
		Params: &sdkmcp.ReadResourceParams{URI: "gum://help/nonexistent-topic"},
	}
	res, err := s.handleHelpTopicRead(context.Background(), req)
	if err == nil {
		t.Fatalf("handleHelpTopicRead(unknown)=%+v nil err; want RESOURCE_NOT_FOUND", res)
	}
	if rpcErr, ok := err.(*jsonrpc.Error); ok {
		if !strings.Contains(string(rpcErr.Data), "nonexistent-topic") {
			t.Errorf("rpcErr.Data=%q; want topic name in detail", string(rpcErr.Data))
		}
	} else {
		t.Errorf("err type=%T; want *jsonrpc.Error", err)
	}
}

package mcp

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// connectPaginationClient starts srv over in-memory transports and returns a
// connected client session. The session closes when the test ends.
func connectPaginationClient(t *testing.T, srv *Server) (context.Context, *sdkmcp.ClientSession) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	srvTransport, clientTransport := sdkmcp.NewInMemoryTransports()
	go func() { _ = srv.Run(ctx, srvTransport) }()
	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "test", Version: "0"}, nil)
	cs, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client.Connect: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return ctx, cs
}

// addFillerResources registers n extra fixed-URI resources so the inventory
// crosses the page cap. gum itself serves far fewer than 100 resources, so
// the cap is unobservable without them.
func addFillerResources(srv *Server, n int) {
	for i := 0; i < n; i++ {
		uri := fmt.Sprintf("gum://test/filler/%03d", i)
		srv.sdkSrv.AddResource(
			&sdkmcp.Resource{Name: fmt.Sprintf("filler_%03d", i), URI: uri, MIMEType: "text/plain"},
			func(context.Context, *sdkmcp.ReadResourceRequest) (*sdkmcp.ReadResourceResult, error) {
				return &sdkmcp.ReadResourceResult{}, nil
			},
		)
	}
}

func addFillerTemplates(srv *Server, n int) {
	for i := 0; i < n; i++ {
		srv.sdkSrv.AddResourceTemplate(
			&sdkmcp.ResourceTemplate{
				Name:        fmt.Sprintf("filler_tpl_%03d", i),
				URITemplate: fmt.Sprintf("gum://test/tpl/%03d/{id}", i),
				MIMEType:    "text/plain",
			},
			func(context.Context, *sdkmcp.ReadResourceRequest) (*sdkmcp.ReadResourceResult, error) {
				return &sdkmcp.ReadResourceResult{}, nil
			},
		)
	}
}

// TestMCPResourceCursorPagination is the docs/test-matrix.md row 35 proof:
// resources/list and resources/templates/list take the optional MCP `cursor`,
// return `nextCursor` while entries remain, cap a page at 100 entries, and
// accept no client page-size parameter.
func TestMCPResourceCursorPagination(t *testing.T) {
	t.Run("page cap is the spec value", func(t *testing.T) {
		if listPageCap != 100 {
			t.Fatalf("listPageCap = %d; spec §13 fixes the v0.1.0 cap at 100", listPageCap)
		}
	})

	t.Run("no client page-size parameter exists", func(t *testing.T) {
		// Spec §13: "there is no client page-size input in the MCP
		// 2025-11-25 contract, so tests MUST NOT require one." A client
		// cannot send what the params type cannot carry.
		for _, params := range []any{sdkmcp.ListResourcesParams{}, sdkmcp.ListResourceTemplatesParams{}} {
			typ := reflect.TypeOf(params)
			for i := 0; i < typ.NumField(); i++ {
				name := typ.Field(i).Name
				if strings.Contains(strings.ToLower(name), "pagesize") || strings.Contains(strings.ToLower(name), "limit") {
					t.Errorf("%s carries a client page-size field %q", typ.Name(), name)
				}
			}
			if _, ok := typ.FieldByName("Cursor"); !ok {
				t.Errorf("%s has no Cursor field; cursor pagination is mandatory", typ.Name())
			}
		}
	})

	t.Run("gum inventory fits one page and reports no cursor", func(t *testing.T) {
		ctx, cs := connectPaginationClient(t, NewServer(schemaTestDispatcher{}))
		res, err := cs.ListResources(ctx, nil)
		if err != nil {
			t.Fatalf("ListResources: %v", err)
		}
		if len(res.Resources) == 0 {
			t.Fatal("resources/list returned nothing; gum registers fixed-URI resources")
		}
		if len(res.Resources) > listPageCap {
			t.Errorf("resources/list returned %d entries; cap is %d", len(res.Resources), listPageCap)
		}
		if res.NextCursor != "" {
			t.Errorf("nextCursor = %q on a single-page inventory; want empty", res.NextCursor)
		}
		tpl, err := cs.ListResourceTemplates(ctx, nil)
		if err != nil {
			t.Fatalf("ListResourceTemplates: %v", err)
		}
		if len(tpl.ResourceTemplates) == 0 {
			t.Fatal("resources/templates/list returned nothing; spec §13 requires parametric URIs there")
		}
		if tpl.NextCursor != "" {
			t.Errorf("templates nextCursor = %q on a single-page inventory; want empty", tpl.NextCursor)
		}
	})

	t.Run("resources/list caps a page at 100 and pages with the cursor", func(t *testing.T) {
		srv := NewServer(schemaTestDispatcher{})
		const filler = 150
		addFillerResources(srv, filler)
		ctx, cs := connectPaginationClient(t, srv)

		first, err := cs.ListResources(ctx, &sdkmcp.ListResourcesParams{})
		if err != nil {
			t.Fatalf("ListResources page 1: %v", err)
		}
		if len(first.Resources) != listPageCap {
			t.Fatalf("page 1 returned %d entries; want the %d cap", len(first.Resources), listPageCap)
		}
		if first.NextCursor == "" {
			t.Fatal("page 1 returned no nextCursor while entries remain")
		}

		seen := map[string]bool{}
		for _, r := range first.Resources {
			seen[r.URI] = true
		}
		cursor := first.NextCursor
		pages := 1
		for cursor != "" {
			if pages > 10 {
				t.Fatal("pagination did not terminate within 10 pages")
			}
			next, err := cs.ListResources(ctx, &sdkmcp.ListResourcesParams{Cursor: cursor})
			if err != nil {
				t.Fatalf("ListResources page %d: %v", pages+1, err)
			}
			if len(next.Resources) > listPageCap {
				t.Errorf("page %d returned %d entries; cap is %d", pages+1, len(next.Resources), listPageCap)
			}
			for _, r := range next.Resources {
				if seen[r.URI] {
					t.Errorf("resource %s repeated across pages", r.URI)
				}
				seen[r.URI] = true
			}
			cursor = next.NextCursor
			pages++
		}
		if pages < 2 {
			t.Fatalf("walked %d page(s); %d resources must span more than one", pages, filler)
		}
		if len(seen) < filler {
			t.Errorf("paged over %d unique resources; want at least the %d added", len(seen), filler)
		}
		for i := 0; i < filler; i++ {
			uri := fmt.Sprintf("gum://test/filler/%03d", i)
			if !seen[uri] {
				t.Errorf("resource %s never appeared while paging", uri)
			}
		}
	})

	t.Run("templates/list caps a page at 100 and pages with the cursor", func(t *testing.T) {
		srv := NewServer(schemaTestDispatcher{})
		const filler = 150
		addFillerTemplates(srv, filler)
		ctx, cs := connectPaginationClient(t, srv)

		first, err := cs.ListResourceTemplates(ctx, &sdkmcp.ListResourceTemplatesParams{})
		if err != nil {
			t.Fatalf("ListResourceTemplates page 1: %v", err)
		}
		if len(first.ResourceTemplates) != listPageCap {
			t.Fatalf("page 1 returned %d templates; want the %d cap", len(first.ResourceTemplates), listPageCap)
		}
		if first.NextCursor == "" {
			t.Fatal("page 1 returned no nextCursor while templates remain")
		}
		second, err := cs.ListResourceTemplates(ctx, &sdkmcp.ListResourceTemplatesParams{Cursor: first.NextCursor})
		if err != nil {
			t.Fatalf("ListResourceTemplates page 2: %v", err)
		}
		if len(second.ResourceTemplates) == 0 {
			t.Error("page 2 was empty; the cursor did not advance")
		}
		firstNames := map[string]bool{}
		for _, tpl := range first.ResourceTemplates {
			firstNames[tpl.URITemplate] = true
		}
		for _, tpl := range second.ResourceTemplates {
			if firstNames[tpl.URITemplate] {
				t.Errorf("template %s repeated across pages", tpl.URITemplate)
			}
		}
	})

	t.Run("an unparseable cursor is refused", func(t *testing.T) {
		// Cursors are opaque; a client cannot mint one.
		ctx, cs := connectPaginationClient(t, NewServer(schemaTestDispatcher{}))
		if _, err := cs.ListResources(ctx, &sdkmcp.ListResourcesParams{Cursor: "not-a-real-cursor"}); err == nil {
			t.Error("a forged cursor was accepted; want an invalid-params error")
		}
	})

	t.Run("the Tier A roster still fits one page", func(t *testing.T) {
		// The cap applies to tools/list too. A roster that outgrew it would
		// silently truncate for clients that never send a cursor.
		ctx, cs := connectPaginationClient(t, NewServer(schemaTestDispatcher{}))
		tools, err := cs.ListTools(ctx, nil)
		if err != nil {
			t.Fatalf("ListTools: %v", err)
		}
		if tools.NextCursor != "" {
			t.Errorf("tools/list paginated at %d tools; the Tier A roster must fit the %d cap", len(tools.Tools), listPageCap)
		}
	})
}

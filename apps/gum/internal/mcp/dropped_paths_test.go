package mcp

import (
	"context"
	"strings"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ehmo/gum/internal/dispatch"
)

// droppedDispatcher returns a shaped body plus the kernel's dropped-path report.
type droppedDispatcher struct {
	body     string
	dropped  []string
	full     string
	resource string
}

func (d droppedDispatcher) Dispatch(context.Context, *dispatch.Invocation) (*dispatch.ShapedResponse, error) {
	return &dispatch.ShapedResponse{
		Body:               []byte(d.body),
		DroppedPaths:       d.dropped,
		FullResultPath:     d.full,
		FullResultResource: d.resource,
	}, nil
}

// TestDroppedFieldsGetTheirOwnTextBlock closes gum-bpx0 at the MCP seam. An
// agent reading a shaped body has no way to tell a field the profile removed
// from a field the upstream API never returned: both are simply absent. The
// notice block names the paths and the recovery artifact.
func TestDroppedFieldsGetTheirOwnTextBlock(t *testing.T) {
	srv := NewServer(droppedDispatcher{
		body:    `{"results":[{"text":"a"}]}`,
		dropped: []string{"results.keywordMetrics.monthlySearchVolumes"},
		full:    "/tmp/gum/tee/2026-08-05/op/abc.json.gz",
	})

	res, err := srv.dispatchAndShape(context.Background(), &dispatch.Invocation{OpID: "googleads.x"})
	if err != nil {
		t.Fatalf("dispatchAndShape: %v", err)
	}
	if len(res.Content) != 2 {
		t.Fatalf("Content length = %d; want 2 (body + notice): %+v", len(res.Content), res.Content)
	}
	// The body stays first so a client that reads only content[0] is unaffected.
	if body, ok := res.Content[0].(*sdkmcp.TextContent); !ok || !strings.Contains(body.Text, `"results"`) {
		t.Fatalf("Content[0] is not the shaped body: %+v", res.Content[0])
	}
	notice, ok := res.Content[1].(*sdkmcp.TextContent)
	if !ok {
		t.Fatalf("Content[1] is not a text block: %+v", res.Content[1])
	}
	for _, want := range []string{
		"results.keywordMetrics.monthlySearchVolumes",
		`format: "raw"`,
		"/tmp/gum/tee/2026-08-05/op/abc.json.gz",
	} {
		if !strings.Contains(notice.Text, want) {
			t.Errorf("notice missing %q; got: %s", want, notice.Text)
		}
	}
}

// TestNoNoticeBlockWhenNothingDropped: a lossless response returns the body
// alone. An unconditional notice would train the reader to skip the block.
func TestNoNoticeBlockWhenNothingDropped(t *testing.T) {
	srv := NewServer(droppedDispatcher{body: `{"results":[]}`})

	res, err := srv.dispatchAndShape(context.Background(), &dispatch.Invocation{OpID: "googleads.x"})
	if err != nil {
		t.Fatalf("dispatchAndShape: %v", err)
	}
	if len(res.Content) != 1 {
		t.Fatalf("Content length = %d; want 1 (body only): %+v", len(res.Content), res.Content)
	}
}

// TestNoticeBlockPrecedesResourceLink: with both present, the order is body,
// notice, resource_link. The resource_link block must stay exactly one
// (spec §9.0 line 1847) with the notice inserted before it.
func TestNoticeBlockPrecedesResourceLink(t *testing.T) {
	srv := NewServer(droppedDispatcher{
		body:     `{"results":[]}`,
		dropped:  []string{"results.closeVariants"},
		full:     "/tmp/gum/tee/2026-08-05/op/abc.json.gz",
		resource: "gum://results/abc",
	})

	res, err := srv.dispatchAndShape(context.Background(), &dispatch.Invocation{OpID: "googleads.x"})
	if err != nil {
		t.Fatalf("dispatchAndShape: %v", err)
	}
	if len(res.Content) != 3 {
		t.Fatalf("Content length = %d; want 3 (body + notice + resource_link): %+v", len(res.Content), res.Content)
	}
	if _, ok := res.Content[1].(*sdkmcp.TextContent); !ok {
		t.Errorf("Content[1] = %+v; want the notice text block", res.Content[1])
	}
	link, ok := res.Content[2].(*sdkmcp.ResourceLink)
	if !ok {
		t.Fatalf("Content[2] = %+v; want the resource_link", res.Content[2])
	}
	if link.URI != "gum://results/abc" {
		t.Errorf("resource_link.uri = %q; want gum://results/abc", link.URI)
	}
}

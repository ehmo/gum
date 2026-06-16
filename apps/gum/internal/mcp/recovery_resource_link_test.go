// gum-49v: spec §9.0 lines 1845-1847 + §13 line 3313 + line 3238.
// When recovery=resource_link and tee_mode=always, the MCP tool result
// content[] MUST contain exactly one resource_link block whose URI matches
// _expression.full_result_resource carried in structuredContent. CLI mode
// omits both, but the seam under test is the MCP presentation layer.
//
// This test pins the wire contract at the dispatchAndShape() seam so
// regressions cannot silently drop the resource_link block, duplicate it,
// or invent a second link for the same hash.

package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ehmo/gum/internal/dispatch"
)

// recoveryDispatcher is a stub dispatcher that returns a ShapedResponse with a
// caller-supplied FullResultResource and optional StructuredContent. It lets
// the test exercise dispatchAndShape across both the resource-link-present
// and resource-link-absent branches without spinning up a real expression
// pipeline.
type recoveryDispatcher struct {
	body              []byte
	structured        any
	fullResultPath    string
	fullResultURI     string
	fullResultSize    *int64
	err               error
}

func (d recoveryDispatcher) Dispatch(_ context.Context, _ *dispatch.Invocation) (*dispatch.ShapedResponse, error) {
	if d.err != nil {
		return nil, d.err
	}
	return &dispatch.ShapedResponse{
		Body:               d.body,
		StructuredContent:  d.structured,
		FullResultPath:     d.fullResultPath,
		FullResultResource: d.fullResultURI,
		FullResultSize:     d.fullResultSize,
	}, nil
}

// TestRecoveryResourceLinkContentBlock — bead-named acceptance for gum-49v.
//
// Spec §9.0 line 1845-1847: when shaped.FullResultResource is set, the
// MCP CallToolResult.Content slice MUST contain exactly one *ResourceLink
// with the matching URI, name non-empty, mimeType "application/json", and
// description ≤120 chars. The original text content block stays first;
// the resource_link is appended.
//
// Spec §9.0 line 1847 also forbids duplicates for the same hash.
func TestRecoveryResourceLinkContentBlock(t *testing.T) {
	const wantURI = "gum://results/abc123def456"

	structured := map[string]any{
		"rows": []any{},
		"_expression": map[string]any{
			"full_result_path":     "/tmp/gum/tee/2026-05-23/op/abc123def456.json.gz",
			"full_result_resource": wantURI,
		},
	}

	srv := NewServer(recoveryDispatcher{
		body:           []byte(`{"rows":[]}`),
		structured:     structured,
		fullResultPath: "/tmp/gum/tee/2026-05-23/op/abc123def456.json.gz",
		fullResultURI:  wantURI,
	})

	inv := &dispatch.Invocation{OpID: "gmail.users.messages.list"}
	res, err := srv.dispatchAndShape(context.Background(), inv)
	if err != nil {
		t.Fatalf("dispatchAndShape: %v", err)
	}
	if res == nil {
		t.Fatal("dispatchAndShape returned nil result")
	}
	if res.IsError {
		t.Fatalf("result.IsError=true; want false. Content=%v", res.Content)
	}

	// Content[0] is the text block (shaped body); the resource_link is
	// appended after it.
	if len(res.Content) < 2 {
		t.Fatalf("Content length=%d; want ≥2 (text + resource_link). Got=%+v", len(res.Content), res.Content)
	}

	// Exactly one resource_link block, no duplicates (spec §9.0 line 1847).
	var links []*sdkmcp.ResourceLink
	for _, c := range res.Content {
		if rl, ok := c.(*sdkmcp.ResourceLink); ok {
			links = append(links, rl)
		}
	}
	if len(links) != 1 {
		t.Fatalf("found %d resource_link content blocks; want exactly 1 (spec §9.0 line 1847 forbids duplicates for the same hash)", len(links))
	}

	link := links[0]
	if link.URI != wantURI {
		t.Errorf("resource_link.uri=%q; want %q (must match _expression.full_result_resource)", link.URI, wantURI)
	}
	if link.Name == "" {
		t.Error("resource_link.name is empty; spec §9.0 line 1846 requires a name field")
	}
	if link.MIMEType != "application/json" {
		t.Errorf("resource_link.mimeType=%q; want application/json (spec §9.0 line 1846)", link.MIMEType)
	}
	if len(link.Description) > 120 {
		t.Errorf("resource_link.description length=%d; spec §9.0 line 1847 caps it at 120 chars", len(link.Description))
	}

	// StructuredContent.full_result_resource must equal the link URI
	// (cross-check the spec §9.0 line 1847 invariant from the structured side).
	sc, _ := res.StructuredContent.(map[string]any)
	if sc == nil {
		t.Fatal("StructuredContent is nil; want pass-through of shaped.StructuredContent")
	}
	exp, _ := sc["_expression"].(map[string]any)
	if exp == nil {
		t.Fatal("StructuredContent._expression missing")
	}
	if got, _ := exp["full_result_resource"].(string); got != wantURI {
		t.Errorf("_expression.full_result_resource=%q; want %q (must match resource_link.uri)", got, wantURI)
	}
}

// TestRecoveryResourceLinkAbsentWhenURIEmpty — bead-named acceptance for gum-49v.
//
// Spec §9.0 line 1845: the resource_link block is emitted iff the active
// profile uses recovery=resource_link in MCP mode (signalled by a non-empty
// shaped.FullResultResource). CLI calls always leave it empty, as do MCP
// calls whose profile uses recovery=none or recovery=local_artifact. A
// stray resource_link in those cases would mis-advertise a non-existent
// fetch handle.
func TestRecoveryResourceLinkAbsentWhenURIEmpty(t *testing.T) {
	srv := NewServer(recoveryDispatcher{
		body:           []byte(`{"rows":[]}`),
		structured:     map[string]any{"rows": []any{}},
		fullResultPath: "/tmp/gum/tee/2026-05-23/op/abc.json.gz", // tee fired but recovery=local_artifact
		fullResultURI:  "",                                       // no resource link
	})

	inv := &dispatch.Invocation{OpID: "gmail.users.messages.list"}
	res, err := srv.dispatchAndShape(context.Background(), inv)
	if err != nil {
		t.Fatalf("dispatchAndShape: %v", err)
	}
	for _, c := range res.Content {
		if _, ok := c.(*sdkmcp.ResourceLink); ok {
			t.Fatalf("found resource_link block when FullResultResource was empty; spec §9.0 line 1845 forbids emission outside recovery=resource_link MCP mode")
		}
	}
}

// TestRecoveryResourceLinkDescriptionUnder120Chars — bead-named acceptance for
// gum-49v. The description hint is constructed from the op_id, which can be
// long (e.g. "google.cloud.aiplatform.v1.projects.locations.models.predict").
// Spec §9.0 line 1847 caps at 120 chars; this asserts the truncation path.
func TestRecoveryResourceLinkDescriptionUnder120Chars(t *testing.T) {
	longOpID := strings.Repeat("aaaaaaaaaa.", 20) + "tail" // ~224 chars, well over the 120-char cap when concatenated with prefix
	srv := NewServer(recoveryDispatcher{
		body:          []byte(`{}`),
		structured:    map[string]any{},
		fullResultURI: "gum://results/cafef00d",
	})
	inv := &dispatch.Invocation{OpID: longOpID}
	res, err := srv.dispatchAndShape(context.Background(), inv)
	if err != nil {
		t.Fatalf("dispatchAndShape: %v", err)
	}
	for _, c := range res.Content {
		rl, ok := c.(*sdkmcp.ResourceLink)
		if !ok {
			continue
		}
		if len(rl.Description) > 120 {
			t.Errorf("description length=%d for long op_id; spec §9.0 line 1847 cap is 120", len(rl.Description))
		}
	}
}

// TestRecoveryResourceLinkSize — bead-named acceptance for gum-6krt.
//
// Spec §9.0 line 1846 lists "size when known" as a field on the resource_link
// block; v0.1.0 wires this from the decompressed tee payload length (matching
// what gum://results/<hash> returns to a resources/read). When the dispatcher
// reports ShapedResponse.FullResultSize, the MCP layer MUST forward it as
// ResourceLink.Size; when it is nil the field MUST stay nil so clients can
// distinguish "size unknown" from "size zero".
func TestRecoveryResourceLinkSize(t *testing.T) {
	const wantURI = "gum://results/sizefield"
	wantSize := int64(2048)
	srv := NewServer(recoveryDispatcher{
		body:           []byte(`{}`),
		structured:     map[string]any{},
		fullResultURI:  wantURI,
		fullResultSize: &wantSize,
	})
	res, err := srv.dispatchAndShape(context.Background(), &dispatch.Invocation{OpID: "x.y"})
	if err != nil {
		t.Fatalf("dispatchAndShape: %v", err)
	}
	var got *sdkmcp.ResourceLink
	for _, c := range res.Content {
		if rl, ok := c.(*sdkmcp.ResourceLink); ok {
			got = rl
			break
		}
	}
	if got == nil {
		t.Fatal("no resource_link content block emitted")
	}
	if got.Size == nil {
		t.Fatal("resource_link.size is nil; want forward of shaped.FullResultSize")
	}
	if *got.Size != wantSize {
		t.Errorf("resource_link.size = %d; want %d", *got.Size, wantSize)
	}

	// Nil-size branch: when FullResultSize is unset the resource_link Size
	// MUST stay nil (clients distinguish "unknown" from "zero").
	srv2 := NewServer(recoveryDispatcher{
		body:          []byte(`{}`),
		structured:    map[string]any{},
		fullResultURI: wantURI,
		// fullResultSize: nil
	})
	res2, err := srv2.dispatchAndShape(context.Background(), &dispatch.Invocation{OpID: "x.y"})
	if err != nil {
		t.Fatalf("dispatchAndShape (nil size): %v", err)
	}
	for _, c := range res2.Content {
		if rl, ok := c.(*sdkmcp.ResourceLink); ok && rl.Size != nil {
			t.Errorf("resource_link.size = %d; want nil when shaped.FullResultSize is nil", *rl.Size)
		}
	}
}

// TestRecoveryResourceLinkSurvivesJSONRoundTrip — bead-named acceptance for
// gum-49v. The resource_link must marshal to a JSON-RPC wire form that
// clients can parse back into the matching MCP block kind. This catches
// regressions where the block is appended structurally but lacks the
// type:"resource_link" discriminator on the wire.
func TestRecoveryResourceLinkSurvivesJSONRoundTrip(t *testing.T) {
	const wantURI = "gum://results/wire-shape"
	srv := NewServer(recoveryDispatcher{
		body:          []byte(`{}`),
		structured:    map[string]any{},
		fullResultURI: wantURI,
	})
	res, err := srv.dispatchAndShape(context.Background(), &dispatch.Invocation{OpID: "x.y"})
	if err != nil {
		t.Fatalf("dispatchAndShape: %v", err)
	}
	raw, err := json.Marshal(res)
	if err != nil {
		t.Fatalf("marshal CallToolResult: %v", err)
	}
	var wire struct {
		Content []map[string]any `json:"content"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatalf("unmarshal CallToolResult: %v; raw=%s", err, raw)
	}
	found := 0
	for _, c := range wire.Content {
		kind, _ := c["type"].(string)
		if kind != "resource_link" {
			continue
		}
		found++
		if uri, _ := c["uri"].(string); uri != wantURI {
			t.Errorf("wire resource_link uri=%q; want %q", uri, wantURI)
		}
		if mime, _ := c["mimeType"].(string); mime != "application/json" {
			t.Errorf("wire resource_link mimeType=%q; want application/json", mime)
		}
	}
	if found != 1 {
		t.Errorf("wire resource_link count=%d; want 1", found)
	}
}

package mcp_test

import (
	"encoding/json"
	"strings"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"go.uber.org/goleak"

	"github.com/ehmo/gum/internal/embedded"
)

// TestStaticResourceRegistration is the bead-named acceptance for gum-cw1:
// resources/list must surface the five static resources required by spec
// §13 lines 3146-3150 (gum://catalog, gum://status/canaries, gum://plugins,
// gum://status/health, gum://help/topics). Each fixed URI must be readable
// and the catalog row must carry the Size annotation + the
// x-gum-do-not-auto-inject Meta flag.
func TestStaticResourceRegistration(t *testing.T) {
	defer goleak.VerifyNone(t)

	ctx, cs, _, cleanup := connectResourceClient(t)
	defer cleanup()

	res, err := cs.ListResources(ctx, &sdkmcp.ListResourcesParams{})
	if err != nil {
		t.Fatalf("ListResources: %v", err)
	}
	if len(res.Resources) < 5 {
		t.Fatalf("ListResources returned %d resources; want ≥5", len(res.Resources))
	}

	required := map[string]string{
		"gum://catalog":         "application/json",
		"gum://status/canaries": "text/plain",
		"gum://plugins":         "text/plain",
		"gum://status/health":   "text/plain",
		"gum://help/topics":     "text/plain",
	}
	byURI := make(map[string]*sdkmcp.Resource, len(res.Resources))
	for _, r := range res.Resources {
		byURI[r.URI] = r
	}
	for uri, wantMIME := range required {
		r, ok := byURI[uri]
		if !ok {
			t.Errorf("%s absent from resources/list; want present", uri)
			continue
		}
		if r.MIMEType != wantMIME {
			t.Errorf("%s MIMEType = %q; want %q", uri, r.MIMEType, wantMIME)
		}
	}

	catalogRow, ok := byURI["gum://catalog"]
	if !ok {
		t.Fatal("gum://catalog missing; cannot verify Size + Meta")
	}
	if want := int64(len(embedded.CatalogJSON)); catalogRow.Size != want {
		t.Errorf("gum://catalog Size = %d; want %d (embedded.CatalogJSON length)", catalogRow.Size, want)
	}
	if flag, _ := catalogRow.Meta["x-gum-do-not-auto-inject"].(bool); !flag {
		t.Errorf("gum://catalog Meta[x-gum-do-not-auto-inject] = %v; want true", catalogRow.Meta["x-gum-do-not-auto-inject"])
	}

	for uri := range required {
		got, err := cs.ReadResource(ctx, &sdkmcp.ReadResourceParams{URI: uri})
		if err != nil {
			t.Errorf("ReadResource(%s): %v", uri, err)
			continue
		}
		if len(got.Contents) != 1 {
			t.Errorf("ReadResource(%s) returned %d contents; want 1", uri, len(got.Contents))
			continue
		}
		if got.Contents[0].URI != uri {
			t.Errorf("ReadResource(%s) Contents[0].URI = %q; want %q", uri, got.Contents[0].URI, uri)
		}
		if got.Contents[0].Text == "" {
			t.Errorf("ReadResource(%s) Text is empty", uri)
		}
	}
}

// TestStaticCatalogResourceBodyMatchesEmbedded asserts the catalog body
// returned over MCP is byte-identical to the embedded JSON. A drift here
// means the resource handler is mutating the snapshot, which would break the
// Size annotation contract.
func TestStaticCatalogResourceBodyMatchesEmbedded(t *testing.T) {
	defer goleak.VerifyNone(t)

	ctx, cs, _, cleanup := connectResourceClient(t)
	defer cleanup()

	got, err := cs.ReadResource(ctx, &sdkmcp.ReadResourceParams{URI: "gum://catalog"})
	if err != nil {
		t.Fatalf("ReadResource: %v", err)
	}
	if len(got.Contents) != 1 {
		t.Fatalf("got %d contents; want 1", len(got.Contents))
	}
	if got.Contents[0].Text != string(embedded.CatalogJSON) {
		t.Errorf("gum://catalog body diverges from embedded.CatalogJSON (lens %d vs %d)",
			len(got.Contents[0].Text), len(embedded.CatalogJSON))
	}
	// Sanity check: body is valid JSON so clients can parse it.
	var probe map[string]any
	if err := json.Unmarshal([]byte(got.Contents[0].Text), &probe); err != nil {
		t.Errorf("catalog body not valid JSON: %v", err)
	}
}

// TestStaticHealthResourceClosedEnum pins the six-subsystem closed enum from
// spec §13 line 3149. Adding or removing a row requires a minor-version spec
// PR; this test is the canary.
func TestStaticHealthResourceClosedEnum(t *testing.T) {
	defer goleak.VerifyNone(t)

	ctx, cs, _, cleanup := connectResourceClient(t)
	defer cleanup()

	got, err := cs.ReadResource(ctx, &sdkmcp.ReadResourceParams{URI: "gum://status/health"})
	if err != nil {
		t.Fatalf("ReadResource: %v", err)
	}
	if len(got.Contents) != 1 {
		t.Fatalf("got %d contents; want 1", len(got.Contents))
	}
	body := got.Contents[0].Text
	for _, sub := range []string{
		"audit_log", "cache_sqlite", "canary_runner",
		"gain_ledger", "keychain", "tee_filesystem",
	} {
		if !strings.Contains(body, sub+",healthy,") {
			t.Errorf("gum://status/health body missing %s,healthy row; got:\n%s", sub, body)
		}
	}
	if !strings.Contains(body, "count: 6") {
		t.Errorf("gum://status/health body missing 'count: 6'; got:\n%s", body)
	}
}

// TestStaticCanariesResourceInitialStale asserts the spec §13 line 3147
// initial-state rule: until the §8.5 passive canary runner is wired in,
// the resource returns count:0 — never a stale entry for a plugin that
// never registered.
func TestStaticCanariesResourceInitialStale(t *testing.T) {
	defer goleak.VerifyNone(t)

	ctx, cs, _, cleanup := connectResourceClient(t)
	defer cleanup()

	got, err := cs.ReadResource(ctx, &sdkmcp.ReadResourceParams{URI: "gum://status/canaries"})
	if err != nil {
		t.Fatalf("ReadResource: %v", err)
	}
	body := got.Contents[0].Text
	if !strings.Contains(body, "op: gum.status.canaries") {
		t.Errorf("body missing op header; got:\n%s", body)
	}
	if !strings.Contains(body, "count: 0") {
		t.Errorf("body missing 'count: 0' (initial state); got:\n%s", body)
	}
}

package mcp_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"go.uber.org/goleak"
)

// TestResourceTemplateRegistration — bead-named acceptance for gum-k9k.
// Spec §13 line 3168: six parameterized resource templates MUST be advertised
// via resources/templates/list (not resources/list). The v0.1.0 closed
// inventory is: gum://op/{id}, gum://variant/{id}, gum://schema/{ref},
// gum://results/{hash}, gum://plugin/{name}, gum://help/{topic}.
func TestResourceTemplateRegistration(t *testing.T) {
	defer goleak.VerifyNone(t)
	ctx, cs, _, cleanup := connectResourceClient(t)
	defer cleanup()

	res, err := cs.ListResourceTemplates(ctx, &sdkmcp.ListResourceTemplatesParams{})
	if err != nil {
		t.Fatalf("ListResourceTemplates: %v", err)
	}
	want := map[string]string{
		"gum://op/{id}":        "application/json",
		"gum://variant/{id}":   "application/json",
		"gum://schema/{ref}":   "application/schema+json",
		"gum://results/{hash}": "application/json",
		"gum://plugin/{name}":  "application/json",
		"gum://help/{topic}":   "text/markdown",
	}
	got := map[string]string{}
	for _, tmpl := range res.ResourceTemplates {
		got[tmpl.URITemplate] = tmpl.MIMEType
	}
	for uri, mime := range want {
		if g, ok := got[uri]; !ok {
			t.Errorf("template %q absent from resources/templates/list", uri)
		} else if g != mime {
			t.Errorf("template %q mimeType=%q; want %q", uri, g, mime)
		}
	}
	for uri := range got {
		if _, ok := want[uri]; !ok {
			t.Errorf("unexpected template %q advertised; v0.1.0 inventory is closed at 6", uri)
		}
	}
}

// TestOpVariantResourceWireShape — bead-named acceptance for gum-k9k.
// Spec §13 line 3154-3155 + line 1427: gum://op/{id} and gum://variant/{id}
// MUST return exactly one text resource content item with uri equal to the
// requested URI, mimeType "application/json", and JCS-canonical JSON text.
func TestOpVariantResourceWireShape(t *testing.T) {
	defer goleak.VerifyNone(t)
	ctx, cs, _, cleanup := connectResourceClient(t)
	defer cleanup()

	t.Run("op_happy_path", func(t *testing.T) {
		const uri = "gum://op/gmail.users.messages.list"
		res, err := cs.ReadResource(ctx, &sdkmcp.ReadResourceParams{URI: uri})
		if err != nil {
			t.Fatalf("ReadResource(%s): %v", uri, err)
		}
		if len(res.Contents) != 1 {
			t.Fatalf("Contents=%d; want 1", len(res.Contents))
		}
		c := res.Contents[0]
		if c.URI != uri {
			t.Errorf("content.uri=%q; want %q", c.URI, uri)
		}
		if c.MIMEType != "application/json" {
			t.Errorf("content.mimeType=%q; want application/json", c.MIMEType)
		}
		var payload map[string]any
		if err := json.Unmarshal([]byte(c.Text), &payload); err != nil {
			t.Fatalf("payload not JSON: %v", err)
		}
		if got, _ := payload["op_id"].(string); got != "gmail.users.messages.list" {
			t.Errorf("op_id=%q; want gmail.users.messages.list", got)
		}
		if _, ok := payload["variants"].([]any); !ok {
			t.Errorf("payload.variants missing or wrong type; got %T", payload["variants"])
		}
	})

	t.Run("op_not_found_returns_RESOURCE_NOT_FOUND", func(t *testing.T) {
		const uri = "gum://op/no.such.op.exists"
		_, err := cs.ReadResource(ctx, &sdkmcp.ReadResourceParams{URI: uri})
		assertResourceNotFound(t, err, uri)
	})

	t.Run("variant_happy_path", func(t *testing.T) {
		const uri = "gum://variant/gmail.v1.rest.users.messages.list"
		res, err := cs.ReadResource(ctx, &sdkmcp.ReadResourceParams{URI: uri})
		if err != nil {
			t.Fatalf("ReadResource(%s): %v", uri, err)
		}
		if len(res.Contents) != 1 {
			t.Fatalf("Contents=%d; want 1", len(res.Contents))
		}
		c := res.Contents[0]
		if c.URI != uri {
			t.Errorf("content.uri=%q; want %q", c.URI, uri)
		}
		if c.MIMEType != "application/json" {
			t.Errorf("content.mimeType=%q; want application/json", c.MIMEType)
		}
		var payload map[string]any
		if err := json.Unmarshal([]byte(c.Text), &payload); err != nil {
			t.Fatalf("payload not JSON: %v", err)
		}
		if got, _ := payload["op_id"].(string); got != "gmail.users.messages.list" {
			t.Errorf("op_id parent=%q; want gmail.users.messages.list", got)
		}
		variant, ok := payload["variant"].(map[string]any)
		if !ok {
			t.Fatalf("payload.variant missing; got %T", payload["variant"])
		}
		if got, _ := variant["variant_id"].(string); got != "gmail.v1.rest.users.messages.list" {
			t.Errorf("variant.variant_id=%q; want gmail.v1.rest.users.messages.list", got)
		}
	})

	t.Run("variant_not_found_returns_RESOURCE_NOT_FOUND", func(t *testing.T) {
		const uri = "gum://variant/no.such.variant.exists"
		_, err := cs.ReadResource(ctx, &sdkmcp.ReadResourceParams{URI: uri})
		assertResourceNotFound(t, err, uri)
	})
}

// TestSchemaResourceLookup — bead-named acceptance for gum-k9k.
// Spec §13 line 3156: gum://schema/{ref} returns the full JSON Schema 2020-12
// document. A ref that is unknown to both the active snapshot and the
// profile-local plugin inventory MUST resolve to the canonical
// RESOURCE_NOT_FOUND envelope. The body-materialiser happy paths live in
// schema_resource_test.go (gum-kqvf); this test pins the unknown-ref case
// so the existing gum-k9k contract stays asserted after kqvf closed the
// v0.2.0 deferral.
func TestSchemaResourceLookup(t *testing.T) {
	defer goleak.VerifyNone(t)
	ctx, cs, _, cleanup := connectResourceClient(t)
	defer cleanup()

	const uri = "gum://schema/gmail.users.messages.list.response"
	_, err := cs.ReadResource(ctx, &sdkmcp.ReadResourceParams{URI: uri})
	if err == nil {
		t.Fatal("ReadResource succeeded; want RESOURCE_NOT_FOUND envelope (no v0.1.0 catalog op references this schema_ref)")
	}
	envelope := assertResourceNotFound(t, err, uri)
	if d, _ := envelope["detail"].(string); !strings.Contains(d, "not in active snapshot") {
		t.Errorf("envelope.detail=%q; want a snapshot-miss diagnostic", d)
	}
}

// assertResourceNotFound parses the JSON-RPC error returned by ReadResource
// and asserts both the JSON-RPC error code (-32002, matching the SDK's
// CodeResourceNotFound) and the envelope.error_code field. Returns the parsed
// envelope so callers can probe additional fields.
func assertResourceNotFound(t *testing.T, err error, wantURI string) map[string]any {
	t.Helper()
	var rpcErr *jsonrpc.Error
	if !errors.As(err, &rpcErr) {
		t.Fatalf("error type=%T; want *jsonrpc.Error, value=%v", err, err)
	}
	if rpcErr.Code != -32002 {
		t.Errorf("JSON-RPC error.code=%d; want -32002 (SDK collision with -32004; see known-divergences.md)", rpcErr.Code)
	}
	var envelope map[string]any
	if err := json.Unmarshal(rpcErr.Data, &envelope); err != nil {
		t.Fatalf("envelope unmarshal: %v; raw=%s", err, string(rpcErr.Data))
	}
	if code, _ := envelope["error_code"].(string); code != "RESOURCE_NOT_FOUND" {
		t.Errorf("envelope.error_code=%q; want RESOURCE_NOT_FOUND", code)
	}
	if uri, _ := envelope["uri"].(string); uri != wantURI {
		t.Errorf("envelope.uri=%q; want %q", uri, wantURI)
	}
	return envelope
}

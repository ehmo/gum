package mcp_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/ehmo/gum/internal/embedded"
	"github.com/ehmo/gum/internal/help/topics"
)

// v0.1.0 active seed set from spec §13 line 3150 plus the five per-service
// quickstarts added by gum-72y. The test enforces this list literally so a
// topic add/remove forces a deliberate spec update.
var expectedActiveTopics = []string{
	"auth", "calendar", "code-mode", "docs", "drive",
	"field-masks", "gain", "gmail", "plugins", "profiles",
	"recovery", "sheets", "toon-format",
}

// TestHelpTopicsSeedSet is the spec §13 acceptance: the active seed set in
// docs/help-topics.v1.json matches exactly the eight topics, every active
// topic returns a successful resources/read, and no gum://help/{topic}
// handler exists for a topic absent from the manifest.
func TestHelpTopicsSeedSet(t *testing.T) {
	ctx, cs, _, cleanup := connectResourceClient(t)
	defer cleanup()

	// (a) Manifest seed-set matches the spec literal.
	var manifest struct {
		Topics []struct {
			Topic              string `json:"topic"`
			Status             string `json:"status"`
			OneLineDescription string `json:"one_line_description"`
			RedirectTopic      string `json:"redirect_topic"`
		} `json:"topics"`
	}
	if err := json.Unmarshal(embedded.HelpTopicsJSON, &manifest); err != nil {
		t.Fatalf("unmarshal manifest: %v", err)
	}
	got := make([]string, 0, len(manifest.Topics))
	for _, row := range manifest.Topics {
		if row.Status != "active" {
			continue
		}
		got = append(got, row.Topic)
	}
	if !equalStringSetSorted(got, expectedActiveTopics) {
		t.Fatalf("active topics = %v; want %v", got, expectedActiveTopics)
	}

	// (b) Embedded markdown files match the manifest active set 1:1 (no
	// dangling files, no missing files).
	embedded := topics.Names()
	if !equalStringSetSorted(embedded, expectedActiveTopics) {
		t.Fatalf("embedded *.md = %v; want %v", embedded, expectedActiveTopics)
	}

	// (c) Every active topic returns a successful resources/read with
	// mimeType=text/markdown and non-empty body.
	for _, topic := range expectedActiveTopics {
		uri := "gum://help/" + topic
		res, err := cs.ReadResource(ctx, &sdkmcp.ReadResourceParams{URI: uri})
		if err != nil {
			t.Errorf("ReadResource(%s): %v", uri, err)
			continue
		}
		if n := len(res.Contents); n != 1 {
			t.Errorf("ReadResource(%s) returned %d content items; want 1", uri, n)
			continue
		}
		c := res.Contents[0]
		if c.URI != uri {
			t.Errorf("content.URI = %q; want %q", c.URI, uri)
		}
		if c.MIMEType != "text/markdown" {
			t.Errorf("%s MIMEType = %q; want text/markdown", topic, c.MIMEType)
		}
		if len(c.Text) == 0 {
			t.Errorf("%s body empty", topic)
		}
	}

	// (d) Build-time size ceiling enforced (no topic exceeds 8 KiB).
	if err := topics.ValidateSizes(); err != nil {
		t.Errorf("topics.ValidateSizes: %v", err)
	}
}

// TestHelpResourceNotFound asserts the spec §13 line 1425 envelope for the
// canonical case "template matches but the parameter value does not resolve
// to any known resource". URIs that fail to match the template at all are
// rejected by the SDK with its own RESOURCE_NOT_FOUND before our handler
// runs; spec §13 line 1425 carves out the matches-but-unknown case as the
// envelope-bearing path.
func TestHelpResourceNotFound(t *testing.T) {
	ctx, cs, _, cleanup := connectResourceClient(t)
	defer cleanup()

	// Group A — template matches, value rejected by our handler. Envelope
	// MUST carry error_code=RESOURCE_NOT_FOUND with user_message + suggestion
	// per spec §13 line 1425.
	envelopeCases := []string{
		"gum://help/does-not-exist",
		"gum://help/UPPER",            // non-kebab-lowercase
		"gum://help/topic_with_under", // underscore not allowed
	}
	for _, uri := range envelopeCases {
		_, err := cs.ReadResource(ctx, &sdkmcp.ReadResourceParams{URI: uri})
		if err == nil {
			t.Errorf("ReadResource(%s) succeeded; want RESOURCE_NOT_FOUND", uri)
			continue
		}
		var rpcErr *jsonrpc.Error
		if !errors.As(err, &rpcErr) {
			t.Errorf("ReadResource(%s) err = %v (%T); want *jsonrpc.Error", uri, err, err)
			continue
		}
		// -32002 is the SDK's CodeResourceNotFound (matches the MCP 2025-11-25
		// wire spec). spec §13 line 1427 says -32004, but that code collides
		// with jsonrpc2.ErrServerClosing in the SDK; see help_resource.go for
		// the divergence rationale.
		if rpcErr.Code != -32002 {
			t.Errorf("ReadResource(%s) Code = %d; want -32002", uri, rpcErr.Code)
		}
		var env struct {
			ErrorCode   string `json:"error_code"`
			URI         string `json:"uri"`
			UserMessage string `json:"user_message"`
			Suggestion  string `json:"suggestion"`
		}
		if err := json.Unmarshal(rpcErr.Data, &env); err != nil {
			t.Errorf("ReadResource(%s) error.data not JSON: %v", uri, err)
			continue
		}
		if env.ErrorCode != "RESOURCE_NOT_FOUND" {
			t.Errorf("ReadResource(%s) error_code = %q; want RESOURCE_NOT_FOUND", uri, env.ErrorCode)
		}
		if env.URI != uri {
			t.Errorf("ReadResource(%s) envelope.uri = %q; want %q", uri, env.URI, uri)
		}
		if env.UserMessage == "" || env.Suggestion == "" {
			t.Errorf("ReadResource(%s) envelope missing user_message/suggestion: %+v", uri, env)
		}
	}

	// Group B — URIs that do not match the gum://help/{topic} template at
	// all. The SDK returns -32002 directly; the envelope is the SDK's
	// default (no error_code, no suggestion). We still assert the code so a
	// future SDK upgrade that returns 200-success-with-empty-body breaks
	// this test loudly.
	templateMissCases := []string{
		"gum://help/",                  // empty tail
		"gum://help/a/b",               // multi-segment substitution
		"gum://help/auth?x=1",          // query string
		"gum://help/topics/extra-path", // 'topics' is the list URI, never a topic name
	}
	for _, uri := range templateMissCases {
		_, err := cs.ReadResource(ctx, &sdkmcp.ReadResourceParams{URI: uri})
		if err == nil {
			t.Errorf("ReadResource(%s) succeeded; want RESOURCE_NOT_FOUND", uri)
			continue
		}
		var rpcErr *jsonrpc.Error
		if !errors.As(err, &rpcErr) {
			t.Errorf("ReadResource(%s) err = %v (%T); want *jsonrpc.Error", uri, err, err)
			continue
		}
		if rpcErr.Code != -32002 {
			t.Errorf("ReadResource(%s) Code = %d; want -32002", uri, rpcErr.Code)
		}
	}
}

// TestHelpTopicsListAdvertised verifies gum://help/topics appears in
// resources/list (it is a fixed URI, not a template).
func TestHelpTopicsListAdvertised(t *testing.T) {
	ctx, cs, _, cleanup := connectResourceClient(t)
	defer cleanup()

	res, err := cs.ListResources(ctx, &sdkmcp.ListResourcesParams{})
	if err != nil {
		t.Fatalf("ListResources: %v", err)
	}
	found := false
	for _, r := range res.Resources {
		if r.URI == "gum://help/topics" {
			found = true
			if r.MIMEType != "text/plain" {
				t.Errorf("gum://help/topics MIMEType = %q; want text/plain", r.MIMEType)
			}
		}
	}
	if !found {
		t.Errorf("gum://help/topics absent from resources/list; want present")
	}
}

// TestHelpTopicTemplateAdvertised verifies gum://help/{topic} appears in
// resources/templates/list per spec §13 line 3168.
func TestHelpTopicTemplateAdvertised(t *testing.T) {
	ctx, cs, _, cleanup := connectResourceClient(t)
	defer cleanup()

	res, err := cs.ListResourceTemplates(ctx, &sdkmcp.ListResourceTemplatesParams{})
	if err != nil {
		t.Fatalf("ListResourceTemplates: %v", err)
	}
	found := false
	for _, tmpl := range res.ResourceTemplates {
		if tmpl.URITemplate == "gum://help/{topic}" {
			found = true
			if tmpl.MIMEType != "text/markdown" {
				t.Errorf("gum://help/{topic} MIMEType = %q; want text/markdown", tmpl.MIMEType)
			}
		}
	}
	if !found {
		t.Errorf("gum://help/{topic} absent from resources/templates/list; want present")
	}
}

// TestHelpTopicsListBody asserts gum://help/topics returns a TOON body
// containing every active topic, sorted lexicographically by topic.
func TestHelpTopicsListBody(t *testing.T) {
	ctx, cs, _, cleanup := connectResourceClient(t)
	defer cleanup()

	res, err := cs.ReadResource(ctx, &sdkmcp.ReadResourceParams{URI: "gum://help/topics"})
	if err != nil {
		t.Fatalf("ReadResource: %v", err)
	}
	if len(res.Contents) != 1 {
		t.Fatalf("contents = %d; want 1", len(res.Contents))
	}
	body := res.Contents[0].Text
	if !strings.Contains(body, "format_version: 1") {
		t.Errorf("body missing format_version: %q", body)
	}
	if !strings.Contains(body, "fields: topic,status,one_line_description,redirect_topic") {
		t.Errorf("body missing fields header: %q", body)
	}
	// Active topics must all appear, in sorted order.
	prev := ""
	for _, topic := range expectedActiveTopics {
		idx := strings.Index(body, "\n"+topic+",")
		if idx < 0 {
			t.Errorf("body missing row for %q", topic)
			continue
		}
		if prev != "" && strings.Index(body, "\n"+prev+",") > idx {
			t.Errorf("topic %q appears before %q; want lexicographic order", topic, prev)
		}
		prev = topic
	}
}

// equalStringSetSorted compares two slices as sets, sort-insensitive.
func equalStringSetSorted(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	seen := make(map[string]int, len(a))
	for _, s := range a {
		seen[s]++
	}
	for _, s := range b {
		seen[s]--
		if seen[s] < 0 {
			return false
		}
	}
	return true
}

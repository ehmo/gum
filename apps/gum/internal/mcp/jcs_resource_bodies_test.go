package mcp_test

// Spec §13 line 1562 (normative): every JSON-valued GUM resource —
// gum://op/{id}, gum://variant/{id}, gum://schema/{ref}, gum://results/{hash},
// gum://plugin/{name}, and deprecated gum://help/{topic} redirects — returns
// one text content item whose text is JCS-canonical JSON (RFC 8785).
//
// The tests that previously proved that row checked only that the body
// parsed. Parseability does not imply canonical form, and Go's encoding/json
// is not a JCS encoder: it HTML-escapes '&', '<' and '>' into &, <
// and >, which RFC 8785 §3.2.2.2 forbids. Any body built with
// json.Marshal therefore leaves the canonical form the moment a string value
// carries one of those three bytes, and upstream payloads carry them
// routinely (a Drive webViewLink query string, an HTML-escaped Gmail
// snippet).
//
// assertJCSCanonical re-canonicalizes the served bytes and demands they come
// back unchanged, so the assertion holds for any payload instead of pinning
// one hand-written literal (bead gum-yi62).

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ehmo/gum/internal/embedded"
	"github.com/ehmo/gum/internal/output/jcs"
)

// escapeBait is a string value whose canonical JSON form differs from
// encoding/json's. jcs.Marshal keeps the three bytes literal; json.Marshal
// writes &, < and >.
const escapeBait = "a&b<c>d"

// assertJCSCanonical fails unless body is already RFC 8785 canonical.
// Decoding with UseNumber keeps integer precision through the round trip, so
// a large int in the payload cannot masquerade as a canonicalization defect.
func assertJCSCanonical(t *testing.T, uri, body string) {
	t.Helper()

	dec := json.NewDecoder(strings.NewReader(body))
	dec.UseNumber()
	var tree any
	if err := dec.Decode(&tree); err != nil {
		t.Fatalf("%s body is not JSON: %v\nbody=%s", uri, err, body)
	}

	want, err := jcs.Marshal(tree)
	if err != nil {
		t.Fatalf("%s: jcs.Marshal: %v", uri, err)
	}
	if string(want) != body {
		t.Errorf("%s body is not JCS-canonical\n got: %s\nwant: %s", uri, body, want)
	}
}

// readOneJSONContent reads uri and returns the single content item's text
// after checking the shape spec §13 line 1562 also fixes.
func readOneJSONContent(t *testing.T, ctx context.Context, cs *sdkmcp.ClientSession, uri, wantMIME string) string {
	t.Helper()

	res, err := cs.ReadResource(ctx, &sdkmcp.ReadResourceParams{URI: uri})
	if err != nil {
		t.Fatalf("ReadResource(%s): %v", uri, err)
	}
	if n := len(res.Contents); n != 1 {
		t.Fatalf("ReadResource(%s) returned %d content items; want 1", uri, n)
	}
	c := res.Contents[0]
	if c.URI != uri {
		t.Errorf("content.URI = %q; want %q", c.URI, uri)
	}
	if c.MIMEType != wantMIME {
		t.Errorf("%s MIMEType = %q; want %q", uri, c.MIMEType, wantMIME)
	}
	return c.Text
}

// swapEmbeddedHelpManifest replaces the embedded manifest for one test. The
// shipped manifest carries no deprecated row, so the redirect arm is
// unreachable without it.
func swapEmbeddedHelpManifest(t *testing.T, body string) {
	t.Helper()
	prev := embedded.HelpTopicsJSON
	embedded.HelpTopicsJSON = []byte(body)
	t.Cleanup(func() { embedded.HelpTopicsJSON = prev })
}

func TestDeprecatedHelpRedirectBodyIsJCSCanonical(t *testing.T) {
	swapEmbeddedHelpManifest(t, `{"schema_version":1,"topics":[
		{"topic":"old-topic","status":"deprecated","redirect_topic":"`+escapeBait+`"}]}`)

	ctx, cs, _, cleanup := connectResourceClient(t)
	defer cleanup()

	uri := "gum://help/old-topic"
	body := readOneJSONContent(t, ctx, cs, uri, "application/json")
	assertJCSCanonical(t, uri, body)
}

func TestInactivePluginResourceBodyIsJCSCanonical(t *testing.T) {
	ctx, cs, profileDir, cleanup := connectResourceClient(t)
	defer cleanup()

	// An op the embedded catalog does not carry, so the read falls through
	// to the inventory-owned inactive-plugin branch. needs_configuration is
	// the only inactive shape that echoes plugin-supplied strings
	// (credential_descriptors[].alias) into the body.
	const opID = "acme.widget.get"
	writePluginFixture(t, profileDir, opID)

	uri := "gum://op/" + opID
	body := readOneJSONContent(t, ctx, cs, uri, "application/json")
	assertJCSCanonical(t, uri, body)
}

func TestResultArtifactBodyIsJCSCanonical(t *testing.T) {
	ctx, cs, profileDir, cleanup := connectResourceClient(t)
	defer cleanup()

	hash := writeTestArtifact(t, profileDir, "gmail.messages.list", map[string]any{
		"webViewLink": "https://example.test/open?id=1" + escapeBait,
		"count":       3,
	})

	uri := "gum://results/" + hash
	body := readOneJSONContent(t, ctx, cs, uri, "application/json")
	assertJCSCanonical(t, uri, body)
}

// writePluginFixture installs an inventory row for one op owned by a plugin
// stuck in needs_configuration, with a credential alias that carries the
// escape bait.
func writePluginFixture(t *testing.T, profileDir, opID string) {
	t.Helper()

	if err := os.MkdirAll(profileDir, 0o700); err != nil {
		t.Fatalf("mkdir %s: %v", profileDir, err)
	}

	catalogJSON := `{"schema_version":1,"variants":[
		{"op_id":"` + opID + `","variant_id":"` + opID + `.v1","owner_plugin":"acme.plugin"}]}`
	stateJSON := `{"schema_version":1,"plugins":[
		{"name":"acme.plugin","status":"needs_configuration","credential_descriptors":[
			{"alias":"` + escapeBait + `"}]}]}`

	for name, body := range map[string]string{
		"plugin-catalog.json": catalogJSON,
		"plugin-state.json":   stateJSON,
	} {
		if err := os.WriteFile(filepath.Join(profileDir, name), []byte(body), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
}

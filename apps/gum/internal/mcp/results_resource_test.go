package mcp_test

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"go.uber.org/goleak"

	gummcp "github.com/ehmo/gum/internal/mcp"
	"github.com/ehmo/gum/internal/output/tee"
)

// connectResourceClient builds a Server, wires it through an in-memory
// transport, and returns a client session. The caller is responsible for
// closing the returned cleanup func; this also pins XDG_DATA_HOME to a temp
// dir so the resource handler resolves to a known profile location.
func connectResourceClient(t *testing.T) (context.Context, *sdkmcp.ClientSession, string, func()) {
	t.Helper()
	dataHome := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dataHome)

	srv := gummcp.NewServer(stubDispatcher{})
	srvTransport, clientTransport := sdkmcp.NewInMemoryTransports()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- srv.Run(ctx, srvTransport)
	}()

	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "test-client", Version: "0.0.1"}, nil)
	cs, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		cancel()
		t.Fatalf("client.Connect: %v", err)
	}

	cleanup := func() {
		_ = cs.Close()
		cancel()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Error("server.Run did not stop within 2s after cancel")
		}
	}
	profileDir := filepath.Join(dataHome, "gum", "default")
	return ctx, cs, profileDir, cleanup
}

// writeTestArtifact computes a deterministic hash with a fresh secret and
// writes a gzip JSON payload at the canonical artifact path. Returns the hash
// the resource handler will see at gum://results/<hash>.
func writeTestArtifact(t *testing.T, profileDir, opID string, payload any) string {
	t.Helper()
	secret, err := tee.LoadOrCreateSecret(profileDir)
	if err != nil {
		t.Fatalf("LoadOrCreateSecret: %v", err)
	}
	hash, err := tee.ComputeHash(secret, tee.HashInput{
		OpID:                   opID,
		VariantIDResolved:      "v1",
		Args:                   map[string]any{"q": "hello"},
		AuthSubjectFingerprint: "fp-test",
	})
	if err != nil {
		t.Fatalf("ComputeHash: %v", err)
	}
	if _, err := tee.WriteJSON(profileDir, time.Now().UTC(), opID, hash, payload); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}
	return hash
}

// TestResultsResourceTemplateAdvertised verifies the resource template appears
// in resources/templates/list so MCP clients can discover the URI shape.
func TestResultsResourceTemplateAdvertised(t *testing.T) {
	defer goleak.VerifyNone(t)
	ctx, cs, _, cleanup := connectResourceClient(t)
	defer cleanup()

	res, err := cs.ListResourceTemplates(ctx, &sdkmcp.ListResourceTemplatesParams{})
	if err != nil {
		t.Fatalf("ListResourceTemplates: %v", err)
	}
	found := false
	for _, tmpl := range res.ResourceTemplates {
		if tmpl.URITemplate == "gum://results/{hash}" {
			found = true
			if tmpl.MIMEType != "application/json" {
				t.Errorf("template MIMEType = %q; want application/json", tmpl.MIMEType)
			}
		}
	}
	if !found {
		t.Errorf("ListResourceTemplates: gum://results/{hash} not advertised; got %d templates", len(res.ResourceTemplates))
	}
}

// TestResourceReadResultsHit writes a tee artifact and asserts that
// resources/read returns its decompressed JSON payload as a single text
// content item with MIMEType application/json.
func TestResourceReadResultsHit(t *testing.T) {
	defer goleak.VerifyNone(t)
	ctx, cs, profileDir, cleanup := connectResourceClient(t)
	defer cleanup()

	payload := map[string]any{
		"hits":      []string{"a", "b", "c"},
		"truncated": false,
	}
	hash := writeTestArtifact(t, profileDir, "gmail.users.messages.list", payload)
	uri := "gum://results/" + hash

	res, err := cs.ReadResource(ctx, &sdkmcp.ReadResourceParams{URI: uri})
	if err != nil {
		t.Fatalf("ReadResource(%s): %v", uri, err)
	}
	if got, want := len(res.Contents), 1; got != want {
		t.Fatalf("Contents len = %d; want %d", got, want)
	}
	c := res.Contents[0]
	if c.URI != uri {
		t.Errorf("Contents[0].URI = %q; want %q", c.URI, uri)
	}
	if c.MIMEType != "application/json" {
		t.Errorf("Contents[0].MIMEType = %q; want application/json", c.MIMEType)
	}
	var roundtrip map[string]any
	if err := json.Unmarshal([]byte(c.Text), &roundtrip); err != nil {
		t.Fatalf("Contents[0].Text not JSON: %v (text=%q)", err, c.Text)
	}
	if got := roundtrip["truncated"]; got != false {
		t.Errorf("payload truncated = %v; want false", got)
	}
}

// TestResultArtifactExpiredError asserts the JSON-RPC error envelope when the
// hash cannot be located: code -32010, message non-empty, and error.data
// matches the spec §1423 RESULT_ARTIFACT_EXPIRED schema.
func TestResultArtifactExpiredError(t *testing.T) {
	defer goleak.VerifyNone(t)
	ctx, cs, _, cleanup := connectResourceClient(t)
	defer cleanup()

	// A well-formed but never-written hash.
	uri := "gum://results/deadbeefcafef00ddeadbeefcafef00ddeadbeefcafef00ddeadbeefcafef00d"
	_, err := cs.ReadResource(ctx, &sdkmcp.ReadResourceParams{URI: uri})
	if err == nil {
		t.Fatalf("ReadResource(%s): nil error; want RESULT_ARTIFACT_EXPIRED", uri)
	}
	var rpcErr *jsonrpc.Error
	if !errors.As(err, &rpcErr) {
		t.Fatalf("got error type %T (%v); want *jsonrpc.Error", err, err)
	}
	if rpcErr.Code != -32010 {
		t.Errorf("rpcErr.Code = %d; want -32010", rpcErr.Code)
	}
	if rpcErr.Message == "" {
		t.Error("rpcErr.Message empty; want non-empty")
	}
	if len(rpcErr.Data) == 0 {
		t.Fatal("rpcErr.Data empty; want §1423 envelope")
	}
	var env map[string]any
	if err := json.Unmarshal(rpcErr.Data, &env); err != nil {
		t.Fatalf("rpcErr.Data not JSON: %v", err)
	}
	if env["error_code"] != "RESULT_ARTIFACT_EXPIRED" {
		t.Errorf("envelope.error_code = %v; want RESULT_ARTIFACT_EXPIRED", env["error_code"])
	}
	if env["uri"] != uri {
		t.Errorf("envelope.uri = %v; want %q", env["uri"], uri)
	}
	if _, ok := env["expires_at"]; !ok {
		t.Error("envelope missing expires_at key (must be present, may be null)")
	}
	if env["expires_at"] != nil {
		t.Errorf("envelope.expires_at = %v; want null in v0.1.0", env["expires_at"])
	}
	if env["user_message"] == "" || env["user_message"] == nil {
		t.Error("envelope.user_message empty")
	}
	if env["suggestion"] == "" || env["suggestion"] == nil {
		t.Error("envelope.suggestion empty")
	}
}

// TestResultMalformedURIIsExpired guarantees that we don't differentiate
// "unparseable URI" from "missing artifact": the response shape is identical
// (code -32010 plus envelope), so clients have exactly one recovery path.
func TestResultMalformedURIIsExpired(t *testing.T) {
	defer goleak.VerifyNone(t)
	ctx, cs, _, cleanup := connectResourceClient(t)
	defer cleanup()

	uri := "gum://results/UPPERCASE"
	_, err := cs.ReadResource(ctx, &sdkmcp.ReadResourceParams{URI: uri})
	if err == nil {
		t.Fatal("ReadResource: nil error; want -32010")
	}
	var rpcErr *jsonrpc.Error
	if !errors.As(err, &rpcErr) {
		t.Fatalf("got %T; want *jsonrpc.Error", err)
	}
	if rpcErr.Code != -32010 {
		t.Errorf("rpcErr.Code = %d; want -32010", rpcErr.Code)
	}
}

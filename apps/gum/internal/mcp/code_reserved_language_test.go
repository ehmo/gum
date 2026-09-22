package mcp

// Reserved gum.code language rejection (test-matrix row 216, bead gum-q6v7).
//
// v0.1.0 ships one scripting language. The strings starlark, yaegi, js and
// python are reserved for later releases and are absent from the language
// enum (spec §411). The closed-enum claim is one assertion per reserved name,
// not one name standing in for four: an enum widened by a single entry still
// passes a single-case test.
//
// Spec §307 fixes the rejection transport. go-sdk validates tool input only
// inside its generic AddTool[In, Out] helper and gum registers raw schemas
// through the untyped AddTool(*Tool, ToolHandler), so the validation seam is
// gum's own validatedHandler: the response is a tools/call result carrying the
// INVALID_ARGS envelope, and the JSON-RPC error member stays absent. The
// assertions read the wire because that distinction disappears once an SDK
// client has decoded the frame.

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/ehmo/gum/internal/dispatch"
)

// reservedCodeLanguages is the §411 reserved list. unknownCodeLanguage stands
// for "any other unknown value", which the same enum check rejects.
var reservedCodeLanguages = []string{"starlark", "yaegi", "js", "python"}

const unknownCodeLanguage = "ruby"

// kernelCounter answers every call with an empty body and counts how many
// invocations reached it. A rejected call must never increment it.
type kernelCounter struct {
	mu sync.Mutex
	n  int
}

func (d *kernelCounter) Dispatch(context.Context, *dispatch.Invocation) (*dispatch.ShapedResponse, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.n++
	return &dispatch.ShapedResponse{Body: []byte("{}")}, nil
}

func (d *kernelCounter) count() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.n
}

// callCode runs one gum.code call over the raw wire and returns the result
// member, the JSON-RPC error member, and the dispatcher that was behind the
// server.
func callCode(t *testing.T, language string) (result, rpcErr json.RawMessage, disp *kernelCounter) {
	t.Helper()

	isolateAuditSentinel(t)
	t.Setenv("HOME", t.TempDir())

	disp = &kernelCounter{}
	conn := newWireConn(t, NewServer(disp))
	conn.handshake()

	result, rpcErr = conn.callRaw("tools/call", map[string]any{
		"name":      "gum.code",
		"arguments": map[string]any{"language": language, "source": "1"},
	})
	return result, rpcErr, disp
}

func TestCodeReservedLanguageRejection(t *testing.T) {
	for _, language := range append(append([]string{}, reservedCodeLanguages...), unknownCodeLanguage) {
		t.Run(language, func(t *testing.T) {
			result, rpcErr, disp := callCode(t, language)

			if len(rpcErr) > 0 && string(rpcErr) != "null" {
				t.Fatalf("language %q produced JSON-RPC error %s; spec §307 rejects a schema violation with an INVALID_ARGS result envelope, not a transport error", language, rpcErr)
			}

			var res struct {
				IsError bool `json:"isError"`
				Content []struct {
					Text string `json:"text"`
				} `json:"content"`
			}
			if err := json.Unmarshal(result, &res); err != nil {
				t.Fatalf("decode tools/call result %s: %v", result, err)
			}
			if !res.IsError {
				t.Fatalf("language %q was accepted: %s", language, result)
			}
			if len(res.Content) == 0 {
				t.Fatalf("language %q rejected with no content: %s", language, result)
			}

			var envelope struct {
				ErrorCode string `json:"error_code"`
				Message   string `json:"message"`
				Retryable bool   `json:"retryable"`
			}
			if err := json.Unmarshal([]byte(res.Content[0].Text), &envelope); err != nil {
				t.Fatalf("decode error envelope %q: %v", res.Content[0].Text, err)
			}
			if envelope.ErrorCode != "INVALID_ARGS" {
				t.Errorf("language %q: error_code = %q; want INVALID_ARGS (spec §307)", language, envelope.ErrorCode)
			}
			if envelope.Retryable {
				t.Errorf("language %q: retryable = true; a closed enum never accepts the value on a retry", language)
			}
			if !strings.Contains(envelope.Message, "gum.code") {
				t.Errorf("language %q: message %q does not name the tool, which §307 requires", language, envelope.Message)
			}
			if !strings.Contains(envelope.Message, language) {
				t.Errorf("language %q: message %q does not name the rejected value", language, envelope.Message)
			}

			if n := disp.count(); n != 0 {
				t.Errorf("language %q reached the kernel %d time(s); the handler must not run, so no script executes", language, n)
			}
		})
	}
}

// TestCodeAcceptsRisor is the control. Without it the rejection test above
// passes just as well against a gum.code that rejects every language, and the
// dispatcher count proves nothing.
func TestCodeAcceptsRisor(t *testing.T) {
	result, rpcErr, disp := callCode(t, "risor")

	if len(rpcErr) > 0 && string(rpcErr) != "null" {
		t.Fatalf("risor produced JSON-RPC error %s", rpcErr)
	}

	var res struct {
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(result, &res); err != nil {
		t.Fatalf("decode tools/call result %s: %v", result, err)
	}
	if res.IsError {
		t.Fatalf("risor was rejected: %s", result)
	}
	if n := disp.count(); n != 1 {
		t.Fatalf("risor reached the kernel %d time(s); want 1", n)
	}
}

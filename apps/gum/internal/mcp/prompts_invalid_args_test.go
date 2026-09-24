package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// syncBuffer collects the raw JSON-RPC frames a LoggingTransport writes. The
// transport logs from the read goroutine, so the buffer needs its own lock.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// TestPromptsGetInvalidArgs is the docs/test-matrix.md proof. Spec §7:
// an argument map sent to a zero-argument prompt MUST come back as JSON-RPC
// -32602 with error.data.error_code = "INVALID_ARGS".
func TestPromptsGetInvalidArgs(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	srv := NewServer(schemaTestDispatcher{})
	srvTransport, clientTransport := sdkmcp.NewInMemoryTransports()
	go func() { _ = srv.Run(ctx, srvTransport) }()

	// The logging transport records the encoded frames, so the assertions
	// below read the bytes the client actually received, not a Go value
	// reconstructed beside them.
	wire := &syncBuffer{}
	logged := &sdkmcp.LoggingTransport{Transport: clientTransport, Writer: wire}
	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "test", Version: "0"}, nil)
	cs, err := client.Connect(ctx, logged, nil)
	if err != nil {
		t.Fatalf("client.Connect: %v", err)
	}
	defer func() { _ = cs.Close() }()

	listed, err := cs.ListPrompts(ctx, &sdkmcp.ListPromptsParams{})
	if err != nil {
		t.Fatalf("ListPrompts: %v", err)
	}
	if len(listed.Prompts) == 0 {
		t.Fatal("prompts/list returned nothing; the roster is not empty")
	}

	for _, p := range listed.Prompts {
		name := p.Name
		t.Run(name, func(t *testing.T) {
			if len(p.Arguments) != 0 {
				t.Fatalf("prompt %q declares %d arguments; the matrix row covers zero-argument prompts", name, len(p.Arguments))
			}
			_, err := cs.GetPrompt(ctx, &sdkmcp.GetPromptParams{
				Name:      name,
				Arguments: map[string]string{"unexpected": "value"},
			})
			if err == nil {
				t.Fatalf("GetPrompt(%s) with an argument map succeeded; want -32602", name)
			}

			var rpcErr *jsonrpc.Error
			if !errors.As(err, &rpcErr) {
				t.Fatalf("GetPrompt(%s) returned %T (%v); want a *jsonrpc.Error", name, err, err)
			}
			if rpcErr.Code != jsonrpc.CodeInvalidParams {
				t.Errorf("error.code = %d; want %d (InvalidParams)", rpcErr.Code, jsonrpc.CodeInvalidParams)
			}
			if rpcErr.Message == "" {
				t.Error("error.message is empty; spec requires a short human-readable summary")
			}
			if len(rpcErr.Data) == 0 {
				t.Fatal("error.data is absent; spec §7 requires the INVALID_ARGS envelope")
			}

			var envelope struct {
				ErrorCode   string `json:"error_code"`
				Prompt      string `json:"prompt"`
				UserMessage string `json:"user_message"`
			}
			if err := json.Unmarshal(rpcErr.Data, &envelope); err != nil {
				t.Fatalf("error.data is not a JSON object: %v (%s)", err, rpcErr.Data)
			}
			if envelope.ErrorCode != "INVALID_ARGS" {
				t.Errorf("error.data.error_code = %q; want INVALID_ARGS", envelope.ErrorCode)
			}
			if envelope.Prompt != name {
				t.Errorf("error.data.prompt = %q; want %q", envelope.Prompt, name)
			}
			wantMsg := "Prompt '" + name + "' takes no arguments; remove the arguments field or upgrade once dynamic prompts ship."
			if envelope.UserMessage != wantMsg {
				t.Errorf("error.data.user_message = %q; want %q", envelope.UserMessage, wantMsg)
			}

			// The prompt body must not ride along on the rejection path.
			if strings.Contains(string(rpcErr.Data), "gum.search_apis") {
				t.Error("error.data leaked the prompt body")
			}
		})
	}

	t.Run("the rejection is on the wire", func(t *testing.T) {
		frames := wire.String()
		if !strings.Contains(frames, `"code":-32602`) {
			t.Errorf("no -32602 frame was transmitted; frames:\n%s", frames)
		}
		if !strings.Contains(frames, `"error_code":"INVALID_ARGS"`) {
			t.Errorf("the transmitted frame carries no INVALID_ARGS data envelope; frames:\n%s", frames)
		}
	})

	t.Run("no arguments still succeeds", func(t *testing.T) {
		got, err := cs.GetPrompt(ctx, &sdkmcp.GetPromptParams{Name: listed.Prompts[0].Name})
		if err != nil {
			t.Fatalf("GetPrompt without arguments: %v", err)
		}
		if len(got.Messages) == 0 {
			t.Error("a valid prompts/get returned no messages")
		}
	})
}

// TestPromptBodiesUnderSizeCap enforces the spec §13 cap of 6 KiB per rendered
// template. The cap was normative and unenforced: §13 stated it for both
// prompts while nothing measured either body.
//
// Both prompts are zero-argument (TestPromptZeroArgumentContract pins that), so
// the stored body is the rendered template. A prompt that grew arguments would
// need this measured after rendering instead.
func TestPromptBodiesUnderSizeCap(t *testing.T) {
	if len(staticPrompts) == 0 {
		t.Fatal("staticPrompts is empty; the roster is closed at two entries")
	}

	for _, p := range staticPrompts {
		if promptBodyOverCap(p) {
			t.Errorf("prompt %s body is %d bytes; §13 caps the rendered template at %d",
				p.Name, len(p.Body), maxPromptBodyBytes)
		}
	}
}

// promptBodyOverCap reports whether one prompt body exceeds the §13 cap.
func promptBodyOverCap(p staticPrompt) bool {
	return len(p.Body) > maxPromptBodyBytes
}

// TestPromptBodySizeCapDetectsAnOversizedBody pins the predicate at its edge.
// The walk above passes on a clean roster whatever the comparison says, which
// is how an unenforced cap looks from the outside.
func TestPromptBodySizeCapDetectsAnOversizedBody(t *testing.T) {
	cases := []struct {
		name string
		size int
		want bool
	}{
		{"at the cap", maxPromptBodyBytes, false},
		{"one byte over", maxPromptBodyBytes + 1, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := staticPrompt{Name: "gum.test", Body: strings.Repeat("x", tc.size)}
			if got := promptBodyOverCap(p); got != tc.want {
				t.Errorf("promptBodyOverCap(%d bytes) = %v; want %v", tc.size, got, tc.want)
			}
		})
	}
}

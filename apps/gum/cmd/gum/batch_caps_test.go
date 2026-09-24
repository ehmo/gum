package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// mcpProcess is one live `gum mcp --stdio` child with its stdout captured.
type mcpProcess struct {
	t      *testing.T
	stdin  interface{ Write([]byte) (int, error) }
	tap    *stdioTap
	stderr *bytes.Buffer
}

// startMCPProcess spawns the built binary on the stdio transport.
func startMCPProcess(t *testing.T, bin string) *mcpProcess {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)

	cmd := exec.CommandContext(ctx, bin, "mcp", "--stdio")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	p := &mcpProcess{t: t, stdin: stdin, tap: &stdioTap{}, stderr: &bytes.Buffer{}}
	cmd.Stdout = p.tap
	cmd.Stderr = p.stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start mcp: %v", err)
	}
	t.Cleanup(func() {
		_ = stdin.Close()
		_ = cmd.Wait()
	})
	return p
}

func (p *mcpProcess) send(line string) {
	p.t.Helper()
	if _, err := p.stdin.Write([]byte(line + "\n")); err != nil {
		p.t.Fatalf("write %s: %v", line, err)
	}
}

// await blocks until a complete frame carrying marker is on stdout, or fails
// the test. firstFrameWithID decodes what it returns, so a mid-frame return
// would hand it a truncated line.
func (p *mcpProcess) await(marker string) string {
	p.t.Helper()
	return awaitFrames(p.t, p.tap, p.stderr, marker)
}

// legacyBatchFrame is a JSON-RPC batch: one array frame carrying two calls.
// prompts/get is the payload because its handler returns a fixed body, so a
// handler that ran leaves a recognisable string on stdout.
const legacyBatchFrame = `[{"jsonrpc":"2.0","id":10,"method":"prompts/get","params":{"name":"gum.audit_recent_writes"}},` +
	`{"jsonrpc":"2.0","id":11,"method":"tools/list","params":{}}]`

// TestMCPBatchCaps is the docs/test-matrix.md proof: gum advertises
// no JSON-RPC batching, and no legacy batch frame reaches a tool handler.
//
// MCP removed batching in 2025-06-18 and the pinned transport refuses a batch
// frame from that version on, so the row's size caps (32 requests, 1 MiB
// body, 256 KiB params) have nothing to gate: a batch that could carry them
// needs a pre-2025-06-18 session, and internal/mcp now refuses to negotiate
// one. Both halves are checked below.
func TestMCPBatchCaps(t *testing.T) {
	bin := buildSmokeBinary(t)

	t.Run("a pre-2025-06-18 handshake is refused", func(t *testing.T) {
		p := startMCPProcess(t, bin)
		p.send(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"legacy","version":"0"}}}`)
		frame := firstFrameWithID(t, p.await(`"id":1`), 1)

		var envelope struct {
			Result json.RawMessage `json:"result"`
			Error  *struct {
				Code    int             `json:"code"`
				Message string          `json:"message"`
				Data    json.RawMessage `json:"data"`
			} `json:"error"`
		}
		if err := json.Unmarshal([]byte(frame), &envelope); err != nil {
			t.Fatalf("decode initialize response: %v", err)
		}
		if envelope.Error == nil {
			t.Fatalf("2025-03-26 was accepted; that session can carry an uncapped legacy batch\nframe=%s", frame)
		}
		if envelope.Error.Code != -32602 {
			t.Errorf("error.code = %d; want -32602 (InvalidParams)", envelope.Error.Code)
		}
		var data struct {
			ErrorCode string `json:"error_code"`
			Minimum   string `json:"min_protocol_version"`
		}
		if err := json.Unmarshal(envelope.Error.Data, &data); err != nil {
			t.Fatalf("decode error.data: %v\ndata=%s", err, envelope.Error.Data)
		}
		if data.ErrorCode != "UNSUPPORTED_CAPABILITY" {
			t.Errorf("error.data.error_code = %q; want UNSUPPORTED_CAPABILITY", data.ErrorCode)
		}
		if data.Minimum != "2025-06-18" {
			t.Errorf("error.data.min_protocol_version = %q; want 2025-06-18", data.Minimum)
		}

		// The refused session must not run a handler for a batch either.
		p.send(legacyBatchFrame)
		time.Sleep(500 * time.Millisecond)
		assertNoHandlerOutput(t, p.tap.snapshot())
	})

	t.Run("the contract version refuses the batch frame outright", func(t *testing.T) {
		p := startMCPProcess(t, bin)
		p.send(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"contract","version":"0"}}}`)
		initFrame := firstFrameWithID(t, p.await(`"id":1`), 1)
		if strings.Contains(initFrame, `"error"`) {
			t.Fatalf("the contract protocol version was refused: %s", initFrame)
		}
		p.send(`{"jsonrpc":"2.0","method":"notifications/initialized","params":{}}`)
		p.send(`{"jsonrpc":"2.0","id":2,"method":"prompts/list","params":{}}`)
		p.await(`"id":2`)

		before := p.tap.snapshot()
		p.send(legacyBatchFrame)
		time.Sleep(1 * time.Second)
		after := strings.TrimPrefix(p.tap.snapshot(), before)
		if strings.Contains(after, `"id":10`) || strings.Contains(after, `"id":11`) {
			t.Errorf("the batch frame was dispatched on a %s session:\n%s", "2025-11-25", after)
		}
		assertNoHandlerOutput(t, after)

		// Capability advertisement: the initialize result names no batching
		// support of any kind.
		if strings.Contains(strings.ToLower(initFrame), "batch") {
			t.Errorf("initialize result advertises batching: %s", initFrame)
		}
	})
}

// firstFrameWithID returns the stdout line carrying the given JSON-RPC id.
func firstFrameWithID(t *testing.T, stream string, id int) string {
	t.Helper()
	marker := `"id":` + itoa(id)
	for _, line := range strings.Split(strings.TrimSuffix(stream, "\n"), "\n") {
		if strings.Contains(line, marker) {
			return line
		}
	}
	t.Fatalf("no frame with %s in:\n%s", marker, stream)
	return ""
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

// assertNoHandlerOutput fails when the stream carries output only a prompt
// handler could have produced.
func assertNoHandlerOutput(t *testing.T, stream string) {
	t.Helper()
	// The first line of the gum.audit_recent_writes prompt body.
	const handlerFingerprint = "Audit the last 24 hours of write/destructive operations"
	if strings.Contains(stream, handlerFingerprint) {
		t.Errorf("a batch element reached the prompt handler:\n%s", stream)
	}
}

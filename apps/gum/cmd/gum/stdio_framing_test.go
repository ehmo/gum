package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"
)

// stdioTap collects everything the server writes to stdout. The reader runs
// on its own goroutine, so the test body snapshots under a lock rather than
// reading the pipe directly.
type stdioTap struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (s *stdioTap) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

func (s *stdioTap) snapshot() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

// TestStdioFramingClean is the docs/test-matrix.md row 193 proof. Spec §13.1
// pins the shape: start the MCP server with a captured stdout pipe, send
// initialize, and assert that every stdout line parses as one JSON-RPC
// message and that no non-JSON bytes appear before notifications/initialized
// is exchanged.
//
// A stray banner, a dependency's progress dot or a debug print on stdout
// corrupts the stream and breaks the handshake, and it does so silently: the
// client reports a parse error somewhere downstream of the real cause.
func TestStdioFramingClean(t *testing.T) {
	bin := buildSmokeBinary(t)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "mcp", "--stdio")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	tap := &stdioTap{}
	var stderr bytes.Buffer
	cmd.Stdout = tap
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start mcp: %v", err)
	}
	t.Cleanup(func() {
		_ = stdin.Close()
		_ = cmd.Wait()
	})

	send := func(line string) {
		t.Helper()
		if _, err := stdin.Write([]byte(line + "\n")); err != nil {
			t.Fatalf("write %s: %v", line, err)
		}
	}
	waitFor := func(marker string) string {
		t.Helper()
		deadline := time.Now().Add(45 * time.Second)
		for time.Now().Before(deadline) {
			if got := tap.snapshot(); strings.Contains(got, marker) {
				return got
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatalf("server never wrote %s\nstdout=%q\nstderr=%q", marker, tap.snapshot(), stderr.String())
		return ""
	}

	// An unprompted server writes nothing. This catches a banner printed
	// during startup; a banner printed later is caught by the full-stream
	// scan below.
	time.Sleep(300 * time.Millisecond)
	if early := tap.snapshot(); early != "" {
		t.Errorf("server wrote to stdout before any request: %q", early)
	}

	send(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"framing","version":"0"}}}`)
	waitFor(`"id":1`)

	// Everything on stdout up to this point precedes the initialized
	// notification, which is the window §13.1 names.
	preInitialized := tap.snapshot()
	assertNDJSONFrames(t, "pre-initialized", preInitialized)

	send(`{"jsonrpc":"2.0","method":"notifications/initialized","params":{}}`)
	send(`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`)
	full := waitFor(`"id":2`)
	assertNDJSONFrames(t, "full session", full)

	if strings.Index(full, `"id":1`) > strings.Index(full, `"id":2`) {
		t.Error("responses arrived out of order; the initialize response must precede tools/list")
	}
	if strings.Contains(full, "Content-Length") {
		t.Error("stdout carries an LSP-style Content-Length header; §13.1 mandates newline-delimited framing")
	}
}

// assertNDJSONFrames checks the §13.1 framing contract over a captured chunk
// of stdout: one UTF-8 JSON-RPC object per line, each terminated by a single
// \n, nothing else.
func assertNDJSONFrames(t *testing.T, label, stream string) {
	t.Helper()
	if stream == "" {
		t.Fatalf("%s: captured no stdout at all", label)
	}
	if !strings.HasSuffix(stream, "\n") {
		t.Errorf("%s: stream does not end on a frame boundary; last bytes are %q",
			label, stream[max(0, len(stream)-80):])
	}
	lines := strings.Split(strings.TrimSuffix(stream, "\n"), "\n")
	for i, line := range lines {
		if line == "" {
			t.Errorf("%s: line %d is empty; frames are separated by exactly one \\n", label, i+1)
			continue
		}
		if line != strings.TrimSpace(line) {
			t.Errorf("%s: line %d has surrounding whitespace, so it is not a bare frame: %q", label, i+1, line)
		}
		var frame map[string]json.RawMessage
		if err := json.Unmarshal([]byte(line), &frame); err != nil {
			t.Errorf("%s: line %d is not a JSON object (non-JSON bytes on stdout): %v\nline=%q", label, i+1, err, line)
			continue
		}
		version, ok := frame["jsonrpc"]
		if !ok {
			t.Errorf("%s: line %d has no jsonrpc member: %q", label, i+1, line)
			continue
		}
		if string(version) != `"2.0"` {
			t.Errorf("%s: line %d declares jsonrpc=%s; want \"2.0\"", label, i+1, version)
		}
	}
}

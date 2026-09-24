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

// framesThrough returns stdout up to and including the first
// newline-terminated frame that holds marker, and "" while no such complete
// frame has arrived.
//
// Waiting on the marker alone is a race. os/exec copies a child's stdout in
// 32 KiB chunks and the tools/list frame is 60 KiB, so the id at the head of
// that frame lands one whole chunk before its terminator. A wait that returns
// on the marker then hands a truncated last line to a caller that parses
// frames.
func (s *stdioTap) framesThrough(marker string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	stream := s.buf.String()
	start := strings.Index(stream, marker)
	if start < 0 {
		return ""
	}
	end := strings.IndexByte(stream[start:], '\n')
	if end < 0 {
		return ""
	}
	return stream[:start+end+1]
}

// completeFrames returns stdout truncated at the last frame boundary. Use it
// where a test snapshots on a timer rather than on a marker: a half-written
// frame is an unfinished write, not a protocol violation.
func (s *stdioTap) completeFrames() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	stream := s.buf.String()
	cut := strings.LastIndexByte(stream, '\n')
	if cut < 0 {
		return ""
	}
	return stream[:cut+1]
}

// awaitFrames blocks until stdout holds a complete frame carrying marker, then
// returns every frame up to and including it.
func awaitFrames(t *testing.T, tap *stdioTap, stderr *bytes.Buffer, marker string) string {
	t.Helper()
	deadline := time.Now().Add(45 * time.Second)
	for time.Now().Before(deadline) {
		if got := tap.framesThrough(marker); got != "" {
			return got
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("server never wrote a complete frame carrying %s\nstdout=%q\nstderr=%q", marker, tap.snapshot(), stderr.String())
	return ""
}

// TestStdioFramingClean is the docs/test-matrix.md proof. Spec §13.1
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

	// An unprompted server writes nothing. This catches a banner printed
	// during startup; a banner printed later is caught by the full-stream
	// scan below.
	time.Sleep(300 * time.Millisecond)
	if early := tap.snapshot(); early != "" {
		t.Errorf("server wrote to stdout before any request: %q", early)
	}

	send(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"framing","version":"0"}}}`)

	// The initialize response is the last byte on stdout before the
	// initialized notification, so this capture is the window §13.1 names.
	preInitialized := awaitFrames(t, tap, &stderr, `"id":1`)
	assertNDJSONFrames(t, "pre-initialized", preInitialized)

	send(`{"jsonrpc":"2.0","method":"notifications/initialized","params":{}}`)
	send(`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`)
	full := awaitFrames(t, tap, &stderr, `"id":2`)
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

// TestStdioTapFrameBoundary pins the wait rule the stdio tests stand on. Test
// run 35950977354 failed on TestStdioFramingClean because the wait returned on
// the marker alone: os/exec delivered the 60,078-byte tools/list frame as
// 32,768 + 27,310 bytes, and the 20 ms poll landed between the two writes, so
// the framing scan read a truncated last line and reported non-JSON bytes on
// stdout. The truncation point reproduces byte for byte at offset 32,952 of a
// real capture.
func TestStdioTapFrameBoundary(t *testing.T) {
	const marker = `"id":2`
	const head = `{"jsonrpc":"2.0","id":1,"result":{}}` + "\n" +
		`{"jsonrpc":"2.0","id":2,"result":{"tools":[{"name":"calendar_create_event"`
	const tail = `}]}}` + "\n"

	tap := &stdioTap{}
	if _, err := tap.Write([]byte(head)); err != nil {
		t.Fatal(err)
	}

	// The state the old rule returned in: the marker is on stdout and its own
	// frame is unfinished.
	partial := tap.snapshot()
	if !strings.Contains(partial, marker) {
		t.Fatalf("fixture carries no %s, so it does not reproduce the race: %q", marker, partial)
	}
	lastLine := partial[strings.LastIndexByte(partial, '\n')+1:]
	var truncated map[string]json.RawMessage
	if err := json.Unmarshal([]byte(lastLine), &truncated); err == nil {
		t.Fatalf("fixture last line parses, so it does not reproduce the truncation: %q", lastLine)
	}

	if got := tap.framesThrough(marker); got != "" {
		t.Errorf("framesThrough returned a mid-frame stream: %q", got)
	}
	if got, want := tap.completeFrames(), head[:strings.IndexByte(head, '\n')+1]; got != want {
		t.Errorf("completeFrames = %q; want %q, the one finished frame", got, want)
	}

	if _, err := tap.Write([]byte(tail)); err != nil {
		t.Fatal(err)
	}
	if got, want := tap.framesThrough(marker), head+tail; got != want {
		t.Errorf("framesThrough after the terminator = %q; want %q", got, want)
	}
	assertNDJSONFrames(t, "replayed capture", tap.framesThrough(marker))
}

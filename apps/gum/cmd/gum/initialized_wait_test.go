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

// serverNotifications returns the notifications inside a captured
// server-to-client stream. A JSON-RPC frame is a notification when it carries
// a method and no id; a response carries an id and no method.
func serverNotifications(t *testing.T, stream string) []string {
	t.Helper()
	var found []string
	for _, line := range strings.Split(strings.TrimSuffix(stream, "\n"), "\n") {
		if line == "" {
			continue
		}
		var envelope struct {
			Method string          `json:"method"`
			ID     json.RawMessage `json:"id"`
		}
		if err := json.Unmarshal([]byte(line), &envelope); err != nil {
			t.Fatalf("stdout frame is not JSON: %v\nline=%q", err, line)
		}
		if envelope.Method != "" && len(envelope.ID) == 0 {
			found = append(found, envelope.Method)
		}
	}
	return found
}

// TestMCPInitializedWaitRule is the docs/test-matrix.md row 33 proof. Spec
// §13.1 line 3393: gum MUST NOT send unsolicited server notifications before
// it receives the client's notifications/initialized, and v0.1.0 sends none
// at all (no tools/list_changed, no resources/list_changed, no
// logging/message).
//
// The SDK has no wait-rule gate of its own: Server.changeAndNotify fires on
// any AddTool / AddResource / AddPrompt whatever the session state. What
// holds the rule up is NewServer declaring ListChanged: false on all three
// capabilities, which makes shouldSendListChangedNotification refuse every
// list-changed notification. Flip one of those flags to true and this test
// goes red, because it reads the real stdout stream of the real binary.
func TestMCPInitializedWaitRule(t *testing.T) {
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

	send(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"waitrule","version":"0"}}}`)
	waitFor(`"id":1`)

	// The SDK debounces a change notification by 10ms, so a stray one has
	// time to land inside the handshake window.
	time.Sleep(200 * time.Millisecond)
	handshakeWindow := tap.snapshot()

	send(`{"jsonrpc":"2.0","method":"notifications/initialized","params":{}}`)
	send(`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`)
	waitFor(`"id":2`)
	send(`{"jsonrpc":"2.0","id":3,"method":"resources/list","params":{}}`)
	waitFor(`"id":3`)
	send(`{"jsonrpc":"2.0","id":4,"method":"prompts/list","params":{}}`)
	waitFor(`"id":4`)
	time.Sleep(200 * time.Millisecond)
	session := tap.snapshot()

	t.Run("the handshake window carries responses only", func(t *testing.T) {
		if handshakeWindow == "" {
			t.Fatal("no server frame arrived before notifications/initialized; the check is vacuous")
		}
		if got := serverNotifications(t, handshakeWindow); len(got) > 0 {
			t.Errorf("server sent %v before notifications/initialized", got)
		}
	})

	t.Run("the session emits no unsolicited notification", func(t *testing.T) {
		if got := serverNotifications(t, session); len(got) > 0 {
			t.Errorf("v0.1.0 emits no unsolicited notification; got %v", got)
		}
	})

	t.Run("the scan flags a notification", func(t *testing.T) {
		// Without this the two checks above would pass on a scanner that
		// never reports anything.
		synthetic := strings.Join([]string{
			`{"jsonrpc":"2.0","id":1,"result":{}}`,
			`{"jsonrpc":"2.0","method":"notifications/tools/list_changed"}`,
			`{"jsonrpc":"2.0","method":"notifications/message","params":{"level":"info"}}`,
		}, "\n") + "\n"
		got := serverNotifications(t, synthetic)
		want := []string{"notifications/tools/list_changed", "notifications/message"}
		if len(got) != len(want) {
			t.Fatalf("scan found %v; want %v", got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("scan[%d] = %q; want %q", i, got[i], want[i])
			}
		}
	})
}

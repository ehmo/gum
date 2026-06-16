package gain_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/output/gain"
)

// TestRunFixtureReplayMalformedRequestJSONTolerated pins replay.go:305-307 —
// readRequestOpID's `json.Unmarshal err → return ""` arm. A malformed
// request.json must NOT fail the replay (the response side is the
// authoritative token signal); processFixture falls back to the fixture
// name as the op_id.
func TestRunFixtureReplayMalformedRequestJSONTolerated(t *testing.T) {
	tmp := t.TempDir()
	fixture := filepath.Join(tmp, "leaf")
	if err := os.MkdirAll(fixture, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(fixture, "response.json"), []byte(`{"ok":true}`), 0o644); err != nil {
		t.Fatalf("response.json: %v", err)
	}
	// Malformed JSON in request.json — readRequestOpID must swallow.
	if err := os.WriteFile(filepath.Join(fixture, "request.json"), []byte("not json{"), 0o644); err != nil {
		t.Fatalf("request.json: %v", err)
	}

	res, err := gain.RunFixtureReplay(tmp, "toon")
	if err != nil {
		t.Fatalf("RunFixtureReplay(malformed request.json): %v", err)
	}
	if res.Stats.TotalCalls != 1 {
		t.Errorf("TotalCalls=%d; want 1 (malformed request.json must not skip fixture)", res.Stats.TotalCalls)
	}
}

// TestRunFixtureReplayWriteExpectedTOONFailureWraps pins replay.go:216-218
// — processFixture's `os.WriteFile(expected-toon.txt) err →
// "write expected-toon.txt:"` arm. Planting a directory at the
// expected-toon.txt path forces EISDIR on the WriteFile.
func TestRunFixtureReplayWriteExpectedTOONFailureWraps(t *testing.T) {
	tmp := t.TempDir()
	fixture := filepath.Join(tmp, "leaf")
	if err := os.MkdirAll(fixture, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(fixture, "response.json"), []byte(`{"ok":true}`), 0o644); err != nil {
		t.Fatalf("response.json: %v", err)
	}
	// Plant a directory at expected-toon.txt — WriteFile returns EISDIR.
	if err := os.Mkdir(filepath.Join(fixture, "expected-toon.txt"), 0o755); err != nil {
		t.Fatalf("plant dir: %v", err)
	}

	_, err := gain.RunFixtureReplay(tmp, "toon")
	if err == nil {
		t.Fatal("RunFixtureReplay with blocker dir at expected-toon.txt: nil err; want write-expected-toon wrap")
	}
	if !strings.Contains(err.Error(), "write expected-toon.txt:") {
		t.Errorf("err=%q; want 'write expected-toon.txt:' wrap", err)
	}
}

// TestRunFixtureReplayWriteExpectedTokensFailureWraps pins replay.go:230-232
// — `os.WriteFile(expected-tokens-cl100k.json) err →
// "write expected-tokens-cl100k.json:"` arm. The fixture must reach
// the second WriteFile, so expected-toon.txt must succeed first: plant
// the blocker only on the tokens file.
func TestRunFixtureReplayWriteExpectedTokensFailureWraps(t *testing.T) {
	tmp := t.TempDir()
	fixture := filepath.Join(tmp, "leaf")
	if err := os.MkdirAll(fixture, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(fixture, "response.json"), []byte(`{"ok":true}`), 0o644); err != nil {
		t.Fatalf("response.json: %v", err)
	}
	if err := os.Mkdir(filepath.Join(fixture, "expected-tokens-cl100k.json"), 0o755); err != nil {
		t.Fatalf("plant dir: %v", err)
	}

	_, err := gain.RunFixtureReplay(tmp, "toon")
	if err == nil {
		t.Fatal("RunFixtureReplay with blocker dir at expected-tokens-cl100k.json: nil err; want token-write wrap")
	}
	if !strings.Contains(err.Error(), "write expected-tokens-cl100k.json:") {
		t.Errorf("err=%q; want 'write expected-tokens-cl100k.json:' wrap", err)
	}
}

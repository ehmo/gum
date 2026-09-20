package gain_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ehmo/gum/internal/output/gain"
)

// tinyFixtureRoot builds a one-leaf replay tree under t.TempDir() so the
// token measurement stays cheap and no test writes into testdata.
func tinyFixtureRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	leaf := filepath.Join(root, "leaf")
	if err := os.MkdirAll(leaf, 0o755); err != nil {
		t.Fatalf("mkdir leaf: %v", err)
	}
	if err := os.WriteFile(filepath.Join(leaf, "response.json"), []byte(`{"items":[]}`), 0o644); err != nil {
		t.Fatalf("write response.json: %v", err)
	}
	return root
}

// TestRunFixtureReplayWriteBaselineFailureWraps pins replay.go:133-135 —
// runReplay's `writeBaseline err → "write baseline:"` arm. A directory
// planted at expected-baseline.json forces EISDIR on the final WriteFile,
// after both inner passes have already succeeded.
func TestRunFixtureReplayWriteBaselineFailureWraps(t *testing.T) {
	root := tinyFixtureRoot(t)
	if err := os.Mkdir(filepath.Join(root, "expected-baseline.json"), 0o755); err != nil {
		t.Fatalf("plant dir: %v", err)
	}

	_, err := gain.RunFixtureReplay(root, "toon")
	if err == nil {
		t.Fatal("RunFixtureReplay with a blocker dir at expected-baseline.json: nil err; want a write-baseline wrap")
	}
	if !strings.Contains(err.Error(), "write baseline:") {
		t.Errorf("err=%q; want a 'write baseline:' wrap", err)
	}
}

// TestRunFixtureReplaySecondPassErrorPropagates pins replay.go:82-84 — the
// determinism check runs the fixture set twice, and the second pass has its
// own error return. A shaper that succeeds once and then fails is the only
// way the two passes disagree, since nothing else differs between them.
func TestRunFixtureReplaySecondPassErrorPropagates(t *testing.T) {
	root := tinyFixtureRoot(t)
	wantErr := errors.New("second pass boom")

	var calls atomic.Int32
	shaper := func(opID, format string, rawBody []byte) (gain.ShapeResult, error) {
		if calls.Add(1) > 1 {
			return gain.ShapeResult{}, wantErr
		}
		return gain.ShapeResult{Body: []byte("x"), OutputProfile: "test/one"}, nil
	}

	_, err := gain.RunFixtureReplayWithShaper(root, "toon", shaper)
	if err == nil {
		t.Fatal("RunFixtureReplayWithShaper with a shaper that fails on pass 2: nil err")
	}
	if !errors.Is(err, wantErr) {
		t.Errorf("err=%v; want it to wrap %v", err, wantErr)
	}
	if got := calls.Load(); got != 2 {
		t.Errorf("shaper calls=%d; want 2 (one per pass over the single fixture)", got)
	}
}

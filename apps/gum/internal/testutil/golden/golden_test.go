package golden_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/testutil/golden"
)

// TestGoldenBytesMatching is the green-path: existing file, identical bytes,
// no failure.
func TestGoldenBytesMatching(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "match.txt")
	want := []byte("hello\nworld\n")
	if err := os.WriteFile(path, want, 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// If this calls t.Fatalf the test fails; that IS the assertion.
	golden.Bytes(t, path, want)
}

// TestGoldenBytesMismatch confirms the diff path runs and includes both the
// "- want" / "+ got" markers and the regenerate-hint command.
func TestGoldenBytesMismatch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mismatch.txt")
	if err := os.WriteFile(path, []byte("alpha\n"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// Use a child *testing.T proxy so we can capture the failure without
	// failing this parent test.
	var failed bool
	var msg string
	captured := &capturingTB{TB: t, onFatal: func(m string) {
		failed = true
		msg = m
	}}
	golden.Bytes(captured, path, []byte("beta\n"))
	if !failed {
		t.Fatalf("expected mismatch to call t.Fatalf")
	}
	if !strings.Contains(msg, "golden mismatch") {
		t.Errorf("expected 'golden mismatch' in failure; got %q", msg)
	}
	if !strings.Contains(msg, "- alpha") || !strings.Contains(msg, "+ beta") {
		t.Errorf("expected '- alpha' and '+ beta' in diff; got %q", msg)
	}
	if !strings.Contains(msg, "-update") {
		t.Errorf("expected regenerate hint to mention -update; got %q", msg)
	}
}

// TestGoldenBytesMissingFileSeedsFixture asserts that an absent golden file
// is created on first run (with a log notice) rather than failing the test —
// this is the on-ramp for adding new golden assertions.
func TestGoldenBytesMissingFileSeedsFixture(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "new", "fixture.txt")
	want := []byte("seed me\n")
	golden.Bytes(t, path, want)
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read seeded fixture: %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("seeded fixture content mismatch: got %q want %q", got, want)
	}
}

// TestGoldenStringIsByteAlias exercises the string-convenience wrapper.
func TestGoldenStringIsByteAlias(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s.txt")
	if err := os.WriteFile(path, []byte("ok\n"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	golden.String(t, path, "ok\n")
}

// capturingTB intercepts Fatalf so the golden-mismatch path can be unit-tested
// without crashing the parent test. Other testing.TB methods proxy through.
type capturingTB struct {
	testing.TB
	onFatal func(string)
}

func (c *capturingTB) Fatalf(format string, args ...any) {
	c.onFatal(fmt.Sprintf(format, args...))
	// Do NOT call the underlying t.Fatalf — that would propagate the failure.
}

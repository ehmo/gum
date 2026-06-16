package golden

import (
	"os"
	"path/filepath"
	"testing"
)

// TestBytesUpdateFlagInvokesWriteGolden pins Bytes's `*update → writeGolden →
// return` arm (golden.go:60-63). Reached when `-update` is passed: even if
// the on-disk file has different content, the golden is OVERWRITTEN rather
// than asserted. This is the regenerate-on-demand workflow documented in the
// package comment.
func TestBytesUpdateFlagInvokesWriteGolden(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "regen.txt")
	// Seed with stale content the test should overwrite.
	if err := os.WriteFile(path, []byte("STALE"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Toggle the package-private update flag for the duration of this test.
	prev := *update
	*update = true
	t.Cleanup(func() { *update = prev })

	fresh := []byte("FRESH-CONTENT")
	Bytes(t, path, fresh)

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read after update: %v", err)
	}
	if string(got) != string(fresh) {
		t.Errorf("after -update Bytes wrote %q; want %q", got, fresh)
	}
}

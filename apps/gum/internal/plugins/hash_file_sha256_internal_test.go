package plugins

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestHashFileSHA256Branches pins the three observable outcomes:
// happy path returns the canonical zero-bytes digest; open-fail
// surfaces the os.Open error untouched; a real file produces a
// stable 64-char lowercase hex digest that downstream lockfile
// comparison can equalDigest against.
func TestHashFileSHA256Branches(t *testing.T) {
	t.Run("empty_file_canonical_digest", func(t *testing.T) {
		p := filepath.Join(t.TempDir(), "empty")
		if err := os.WriteFile(p, nil, 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}
		got, err := hashFileSHA256(p)
		if err != nil {
			t.Fatalf("hashFileSHA256: %v", err)
		}
		// SHA-256 of the empty string is a well-known constant.
		const want = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
		if got != want {
			t.Errorf("got=%q; want %q", got, want)
		}
	})

	t.Run("missing_file_surfaces_open_error", func(t *testing.T) {
		p := filepath.Join(t.TempDir(), "does-not-exist")
		_, err := hashFileSHA256(p)
		if err == nil {
			t.Fatalf("expected error for missing file")
		}
		if !strings.Contains(err.Error(), "does-not-exist") {
			t.Errorf("err=%q; want path in message", err)
		}
	})

	t.Run("non_empty_is_deterministic", func(t *testing.T) {
		p := filepath.Join(t.TempDir(), "blob")
		if err := os.WriteFile(p, []byte("gum"), 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}
		a, err := hashFileSHA256(p)
		if err != nil {
			t.Fatalf("hashFileSHA256: %v", err)
		}
		b, err := hashFileSHA256(p)
		if err != nil {
			t.Fatalf("hashFileSHA256: %v", err)
		}
		if a != b {
			t.Errorf("non-deterministic digest: %q vs %q", a, b)
		}
		if len(a) != 64 {
			t.Errorf("digest length=%d; want 64", len(a))
		}
	})
}

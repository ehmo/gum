package dispatch

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestCurrentKeyVersionShapes pins all four branches: missing dir
// returns version=1 (fresh install); empty dir returns version=1;
// each rotated confirmation.key.N file adds 1 to the count; unrelated
// files in the dir are ignored.
func TestCurrentKeyVersionShapes(t *testing.T) {
	t.Run("missing_dir_returns_one", func(t *testing.T) {
		s, err := NewTokenStore(8, time.Minute, filepath.Join(t.TempDir(), "absent"))
		if err != nil {
			t.Fatalf("NewTokenStore: %v", err)
		}
		v, err := s.currentKeyVersion()
		if err != nil {
			t.Fatalf("currentKeyVersion: %v", err)
		}
		if v != 1 {
			t.Errorf("missing dir version=%d; want 1", v)
		}
	})

	t.Run("empty_dir_returns_one", func(t *testing.T) {
		s, err := NewTokenStore(8, time.Minute, t.TempDir())
		if err != nil {
			t.Fatalf("NewTokenStore: %v", err)
		}
		v, err := s.currentKeyVersion()
		if err != nil {
			t.Fatalf("currentKeyVersion: %v", err)
		}
		if v != 1 {
			t.Errorf("empty dir version=%d; want 1", v)
		}
	})

	t.Run("rotated_files_increment_version", func(t *testing.T) {
		keyDir := t.TempDir()
		// Two prior rotations + an unrelated file (should NOT count).
		for _, name := range []string{"confirmation.key.1", "confirmation.key.2", "unrelated.txt"} {
			if err := os.WriteFile(filepath.Join(keyDir, name), []byte("x"), 0o600); err != nil {
				t.Fatalf("plant %q: %v", name, err)
			}
		}
		s, err := NewTokenStore(8, time.Minute, keyDir)
		if err != nil {
			t.Fatalf("NewTokenStore: %v", err)
		}
		v, err := s.currentKeyVersion()
		if err != nil {
			t.Fatalf("currentKeyVersion: %v", err)
		}
		if v != 3 {
			t.Errorf("version=%d; want 3 (2 rotated + 1)", v)
		}
	})
}

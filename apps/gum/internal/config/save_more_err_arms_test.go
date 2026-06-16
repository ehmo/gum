package config_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/config"
)

// TestSavePropagatesPathHomeError pins Save's `Path err → return err`
// arm (config.go:218-220). When HOME and XDG_CONFIG_HOME are unset,
// Path returns the "config: resolve home" wrap; Save MUST short-circuit
// before MkdirAll so callers don't see a confusing 'mkdir parent' err
// against an empty path.
//
// Skipped on Windows: UserHomeDir uses USERPROFILE, not HOME.
func TestSavePropagatesPathHomeError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("HOME-unset trick is darwin/linux-specific")
	}
	t.Setenv("HOME", "")
	t.Setenv("XDG_CONFIG_HOME", "")

	c := &config.Config{Values: map[string]string{"output.profile.default": "human"}}
	err := config.Save("alpha", c)
	if err == nil {
		t.Fatalf("Save(no HOME) err=nil; want Path-err propagation")
	}
	if !strings.Contains(err.Error(), "config: resolve home") {
		t.Errorf("err=%v; want 'config: resolve home' from Path", err)
	}
}

// TestSaveRenameFailureSurfacesRenameWrap pins Save's
// `os.Rename err → return 'config: rename' wrap` arm (config.go:246-249).
// Reached when the destination path is a non-empty directory: rename(file
// → non-empty-dir) returns ENOTEMPTY on most filesystems. The wrap names
// 'rename' so operators can distinguish atomic-write failures from
// mkdir/chmod failures earlier in the path.
func TestSaveRenameFailureSurfacesRenameWrap(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)

	// Plant a non-empty directory at the final config.toml path so
	// os.Rename(tmp, config.toml) fails (rename file → non-empty dir).
	finalPath := filepath.Join(base, "gum", "alpha", "config.toml")
	if err := os.MkdirAll(finalPath, 0o755); err != nil {
		t.Fatalf("mkdir final: %v", err)
	}
	// Plant a file inside so the dir is non-empty (rename-over-dir fails
	// with ENOTEMPTY rather than EISDIR on some platforms).
	if err := os.WriteFile(filepath.Join(finalPath, "blocker"), []byte("x"), 0o600); err != nil {
		t.Fatalf("plant blocker: %v", err)
	}

	c := &config.Config{Values: map[string]string{"output.profile.default": "human"}}
	err := config.Save("alpha", c)
	if err == nil {
		t.Fatalf("Save(dir-at-dest) err=nil; want rename failure")
	}
	if !strings.Contains(err.Error(), "config: rename") {
		t.Errorf("err=%v; want 'config: rename' wrap", err)
	}
}

package plugins_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/plugins"
)

// TestInstallStatErrorReturnsWrapped pins Install's `os.Stat(source) err
// → return wrap` arm (host.go:154-156). Reached when the source path
// doesn't exist on disk — operators MUST see a clear "stat source"
// message rather than a bare ENOENT.
func TestInstallStatErrorReturnsWrapped(t *testing.T) {
	t.Parallel()
	h := plugins.NewHost(plugins.HostConfig{InstallRoot: t.TempDir()})
	_, err := h.Install(context.Background(), "/does/not/exist/anywhere")
	if err == nil {
		t.Fatal("Install(missing path) err=nil; want stat-err wrap")
	}
	if !strings.Contains(err.Error(), "stat source") {
		t.Errorf("err=%v; want 'stat source' wrap", err)
	}
}

// TestInstallSourceIsFileReturnsError pins the `!info.IsDir() → return
// "is not a directory"` arm (host.go:157-159). Reached when the source
// path exists but is a regular file — manifest loading requires a
// directory tree.
func TestInstallSourceIsFileReturnsError(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	file := filepath.Join(tmp, "not-a-dir")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatalf("plant file: %v", err)
	}
	h := plugins.NewHost(plugins.HostConfig{InstallRoot: tmp})
	_, err := h.Install(context.Background(), file)
	if err == nil {
		t.Fatal("Install(file) err=nil; want 'is not a directory'")
	}
	if !strings.Contains(err.Error(), "is not a directory") {
		t.Errorf("err=%v; want 'is not a directory'", err)
	}
}

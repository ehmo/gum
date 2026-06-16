package plugins_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/plugins"
	"github.com/ehmo/gum/internal/plugins/registry"
)

// TestInstallWithRegistryStatSourceErrorWrapsClearly pins
// InstallWithRegistry's `os.Stat source err → return wrap` arm
// (install_registry.go:74-76). A missing source path MUST surface
// "stat source" in the error message rather than a bare ENOENT.
func TestInstallWithRegistryStatSourceErrorWrapsClearly(t *testing.T) {
	t.Parallel()
	host := plugins.NewHost(plugins.HostConfig{InstallRoot: t.TempDir()})
	_, err := host.InstallWithRegistry(context.Background(), "/does/not/exist/anywhere", plugins.InstallOptions{
		Registry: registry.New(t.TempDir()),
	})
	if err == nil {
		t.Fatal("InstallWithRegistry(missing path) err=nil; want stat-source wrap")
	}
	if !strings.Contains(err.Error(), "stat source") {
		t.Errorf("err=%v; want 'stat source' in wrap", err)
	}
}

// TestInstallWithRegistrySourceIsFileReturnsNotDirectoryError pins
// the `!info.IsDir() → return "is not a directory"` arm
// (install_registry.go:79-81). Reached when the source path exists
// but is a regular file. Manifest loading requires a directory tree.
func TestInstallWithRegistrySourceIsFileReturnsNotDirectoryError(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	file := filepath.Join(tmp, "not-a-dir")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatalf("plant file: %v", err)
	}
	host := plugins.NewHost(plugins.HostConfig{InstallRoot: tmp})
	_, err := host.InstallWithRegistry(context.Background(), file, plugins.InstallOptions{
		Registry: registry.New(t.TempDir()),
	})
	if err == nil {
		t.Fatal("InstallWithRegistry(file) err=nil; want 'is not a directory'")
	}
	if !strings.Contains(err.Error(), "is not a directory") {
		t.Errorf("err=%v; want 'is not a directory'", err)
	}
}

// TestInstallWithRegistryInvalidManifestPropagates pins the
// `LoadManifest err → return err` arm (install_registry.go:84-86).
// Reached when the source dir lacks a manifest.json — LoadManifest
// surfaces the parse/open failure and InstallWithRegistry returns it
// unwrapped (callers see the underlying message).
func TestInstallWithRegistryInvalidManifestPropagates(t *testing.T) {
	t.Parallel()
	src := t.TempDir() // valid dir but no manifest.json inside
	host := plugins.NewHost(plugins.HostConfig{InstallRoot: t.TempDir()})
	_, err := host.InstallWithRegistry(context.Background(), src, plugins.InstallOptions{
		Registry: registry.New(t.TempDir()),
	})
	if err == nil {
		t.Fatal("InstallWithRegistry(no manifest) err=nil; want LoadManifest err propagation")
	}
}

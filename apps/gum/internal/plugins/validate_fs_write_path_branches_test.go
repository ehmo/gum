package plugins_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ehmo/gum/internal/plugins"
)

func TestValidateFSWritePathRejectsAbsoluteFSWriteDir(t *testing.T) {
	installRoot := t.TempDir()
	m := &plugins.Manifest{
		ManifestSchemaVersion: 1,
		PluginID:              "p",
		Shape:                 "mcp-plugin",
		DeclaredCapabilities:  plugins.Capabilities{FSWriteDir: t.TempDir()},
	}

	inside := filepath.Join(installRoot, "p", "data", "x.txt")
	if err := plugins.ValidateFSWritePath(m, installRoot, "p", inside); !errors.Is(err, plugins.ErrFSWriteOutsideSandbox) {
		t.Errorf("absolute fs_write_dir: err=%v; want ErrFSWriteOutsideSandbox", err)
	}
}

// TestValidateFSWritePathExplicitRelativeFSWriteDir pins the relative-path
// arm of the DeclaredCapabilities.FSWriteDir branch: a relative
// fs_write_dir MUST be resolved beneath <installRoot>/<pluginID>, not
// under the process CWD.
func TestValidateFSWritePathExplicitRelativeFSWriteDir(t *testing.T) {
	installRoot := t.TempDir()
	m := &plugins.Manifest{
		ManifestSchemaVersion: 1,
		PluginID:              "p",
		Shape:                 "mcp-plugin",
		DeclaredCapabilities:  plugins.Capabilities{FSWriteDir: "writable"},
	}

	inside := filepath.Join(installRoot, "p", "writable", "ok.txt")
	if err := plugins.ValidateFSWritePath(m, installRoot, "p", inside); err != nil {
		t.Errorf("inside writable: err=%v; want nil", err)
	}

	// A path under installRoot/p/data must be rejected — the manifest's
	// declared writable dir replaces the default.
	outside := filepath.Join(installRoot, "p", "data", "x.txt")
	if err := plugins.ValidateFSWritePath(m, installRoot, "p", outside); !errors.Is(err, plugins.ErrFSWriteOutsideSandbox) {
		t.Errorf("default-data path: err=%v; want ErrFSWriteOutsideSandbox", err)
	}
}

func TestValidateFSWritePathRejectsSymlinkEscape(t *testing.T) {
	installRoot := t.TempDir()
	dataDir := filepath.Join(installRoot, "p", "data")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatalf("mkdir data: %v", err)
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(dataDir, "link")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	m := &plugins.Manifest{
		ManifestSchemaVersion: 1,
		PluginID:              "p",
		Shape:                 "mcp-plugin",
	}

	attempted := filepath.Join(dataDir, "link", "escaped.txt")
	if err := plugins.ValidateFSWritePath(m, installRoot, "p", attempted); !errors.Is(err, plugins.ErrFSWriteOutsideSandbox) {
		t.Errorf("symlink escape: err=%v; want ErrFSWriteOutsideSandbox", err)
	}
}

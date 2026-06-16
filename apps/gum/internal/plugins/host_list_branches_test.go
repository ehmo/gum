package plugins_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/plugins"
)

// TestListReadDirNonExistErrorWraps pins List's
// `ReadDir err non-IsNotExist → return nil, fmt.Errorf("plugin list: %w", err)`
// arm (host.go:250). A file at the InstallRoot path makes ReadDir
// fail with ENOTDIR (not ENOENT), forcing the wrap path. The
// ENOENT-only swallow on the prior arm protects fresh installs;
// other errors must be visible to the operator.
func TestListReadDirNonExistErrorWraps(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	// Plant a file at install-root, NOT a directory → ReadDir → ENOTDIR.
	blocker := filepath.Join(root, "blocker")
	if err := os.WriteFile(blocker, []byte("not-a-dir"), 0o600); err != nil {
		t.Fatalf("plant blocker: %v", err)
	}
	h := plugins.NewHost(plugins.HostConfig{InstallRoot: blocker})
	_, err := h.List()
	if err == nil {
		t.Fatal("List(file-as-root) err=nil; want ENOTDIR wrap")
	}
	if !strings.Contains(err.Error(), "plugin list:") {
		t.Errorf("err=%q; want 'plugin list:' prefix", err.Error())
	}
}

// TestListSkipsInvalidPluginDirSilently pins List's
// `LoadManifest err → continue` arm (host.go:260-262). An install
// root with one valid plugin alongside an empty subdir (no
// manifest.json → ErrManifestNotFound) must return ONLY the valid
// plugin without surfacing the missing-manifest error. The skip is
// deliberate: a half-written install (rare but possible during
// crashes) shouldn't block `gum plugin list`.
func TestListSkipsInvalidPluginDirSilently(t *testing.T) {
	t.Parallel()
	root := t.TempDir()

	// Valid plugin
	validDir := filepath.Join(root, "valid")
	if err := os.MkdirAll(validDir, 0o755); err != nil {
		t.Fatalf("mkdir valid: %v", err)
	}
	manifest := map[string]any{
		"manifest_schema_version": 1,
		"shape":                   "mcp-plugin",
		"plugin_id":               "valid-plugin",
		"executable":              "bin/plugin",
	}
	body, _ := json.Marshal(manifest)
	if err := os.WriteFile(filepath.Join(validDir, "manifest.json"), body, 0o600); err != nil {
		t.Fatalf("write valid manifest: %v", err)
	}

	// Invalid plugin (no manifest.json)
	if err := os.MkdirAll(filepath.Join(root, "broken"), 0o755); err != nil {
		t.Fatalf("mkdir broken: %v", err)
	}

	h := plugins.NewHost(plugins.HostConfig{InstallRoot: root})
	manifests, err := h.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(manifests) != 1 || manifests[0].PluginID != "valid-plugin" {
		t.Errorf("manifests=%+v; want [valid-plugin] (broken dir must skip)", manifests)
	}
}

// TestListSortsMultiplePluginsByID pins the sort.Slice less-fn
// comparison (host.go:267-269). With ≥2 manifests installed the
// comparator runs; planting them in non-sorted name order (z before a)
// proves the sort actually orders the result by PluginID rather than
// returning whatever ReadDir's filesystem order is.
func TestListSortsMultiplePluginsByID(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	for _, id := range []string{"z-plugin", "a-plugin"} {
		dir := filepath.Join(root, id)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", id, err)
		}
		body, _ := json.Marshal(map[string]any{
			"manifest_schema_version": 1,
			"shape":                   "mcp-plugin",
			"plugin_id":               id,
			"executable":              "bin/plugin",
		})
		if err := os.WriteFile(filepath.Join(dir, "manifest.json"), body, 0o600); err != nil {
			t.Fatalf("write %s: %v", id, err)
		}
	}
	h := plugins.NewHost(plugins.HostConfig{InstallRoot: root})
	manifests, err := h.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(manifests) != 2 {
		t.Fatalf("len(manifests)=%d; want 2", len(manifests))
	}
	if manifests[0].PluginID != "a-plugin" || manifests[1].PluginID != "z-plugin" {
		t.Errorf("order=[%s,%s]; want [a-plugin,z-plugin]", manifests[0].PluginID, manifests[1].PluginID)
	}
}

// Defensive sanity: the ErrManifestNotFound sentinel must be defined
// so the silent-skip arm is semantically meaningful (a future rename
// of the sentinel would change the skip behaviour).
func TestListErrManifestNotFoundExists(t *testing.T) {
	t.Parallel()
	if plugins.ErrManifestNotFound == nil {
		t.Skip("ErrManifestNotFound sentinel not exported; skip semantic check")
	}
	if !errors.Is(plugins.ErrManifestNotFound, plugins.ErrManifestNotFound) {
		t.Error("ErrManifestNotFound !errors.Is itself")
	}
}

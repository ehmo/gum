// Spec §8.7 step 2: the install transaction reads the
// existing three files and MERGES the new plugin's rows. InstallWithRegistry
// appended instead, so an upgrade left two rows per plugin in plugins.lock and
// plugin-state.json and two variants per advertised tool in the catalog.
//
// RecordedDigestResolver returns the FIRST matching lock row, so after an
// upgrade the old executable_sha256 won. Host.trustedDigest then compared that
// stale digest against the freshly written sidecar, disagreed, and failed every
// spawn with ErrExecutableUntrusted until the plugin hit permanent quarantine.

package plugins_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/ehmo/gum/internal/plugins"
	"github.com/ehmo/gum/internal/plugins/registry"
)

func TestReinstallMergesRowsAndKeepsNewDigest(t *testing.T) {
	installRoot := t.TempDir()
	profileDir := t.TempDir()
	reg := registry.New(profileDir)
	host := plugins.NewHost(plugins.HostConfig{InstallRoot: installRoot})
	ctx := context.Background()

	src := filepath.Join(testdataDir(), "namespaced-plugin")
	v1 := copyPluginSource(t, src, nil)
	if _, err := host.InstallWithRegistry(ctx, v1, plugins.InstallOptions{Registry: reg}); err != nil {
		t.Fatalf("install v0.1.0: %v", err)
	}

	// Upgrade: a new version whose executable bytes differ, so the recorded
	// digest must change too.
	v2 := copyPluginSource(t, src, map[string]string{`"version": "0.1.0"`: `"version": "0.2.0"`})
	execPath := filepath.Join(v2, "executable")
	body, err := os.ReadFile(execPath)
	if err != nil {
		t.Fatalf("read fixture executable: %v", err)
	}
	if err := os.WriteFile(execPath, append(body, []byte("\n# v0.2.0\n")...), 0o755); err != nil {
		t.Fatalf("rewrite fixture executable: %v", err)
	}
	id, err := host.InstallWithRegistry(ctx, v2, plugins.InstallOptions{Registry: reg})
	if err != nil {
		t.Fatalf("install v0.2.0: %v", err)
	}

	files, err := reg.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := len(files.Lock.Plugins); got != 1 {
		t.Errorf("plugins.lock rows = %d after upgrade; want 1 merged row", got)
	}
	if got := len(files.State.Plugins); got != 1 {
		t.Errorf("plugin-state rows = %d after upgrade; want 1 merged row", got)
	}
	// The fixture advertises one tool, so the catalog holds one variant.
	if got := len(files.Catalog.Variants); got != 1 {
		t.Errorf("plugin-catalog variants = %d after upgrade; want 1", got)
	}

	// The surviving lock row must carry the new version, not the old one.
	row, ok := files.Lock.Plugins[0].(map[string]any)
	if !ok {
		t.Fatalf("lock row is %T; want a JSON object", files.Lock.Plugins[0])
	}
	if v, _ := row["version"].(string); v != "0.2.0" {
		t.Errorf("lock row version = %q after upgrade; want 0.2.0", v)
	}

	// The digest the resolver hands Host.trustedDigest must match the sidecar
	// the upgrade wrote; a disagreement is ErrExecutableUntrusted on every
	// spawn.
	resolver := plugins.RecordedDigestResolver(reg)
	recorded, err := resolver(id)
	if err != nil {
		t.Fatalf("RecordedDigestResolver: %v", err)
	}
	installed := sha256File(t, filepath.Join(installRoot, id, "executable"))
	if recorded != installed {
		t.Errorf("recorded digest %q does not match the installed executable %q;\n"+
			"every spawn fails with ErrExecutableUntrusted until permanent quarantine", recorded, installed)
	}
}

// sha256File hashes path independently of the production helper, so the test
// checks the recorded digest against the bytes on disk rather than against
// another copy of the same code.
func sha256File(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

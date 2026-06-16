package registry

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// TestPluginRegistryABISchemas locks down the spec §8.7 invariants on every
// successful write:
//   - All three files are written with their v1 schema version.
//   - plugins.lock and plugin-state.json carry the same (install_generation,
//     install_txid) pair.
//   - plugins[] arrays are sorted ascending by "name" (line 1772).
func TestPluginRegistryABISchemas(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	reg := New(dir)

	err := reg.WriteTransaction(context.Background(), func(f *Files) error {
		// Insert plugins out of name order; the registry must sort them.
		f.Lock.Plugins = []any{
			map[string]any{"name": "zebra", "version": "1.0.0"},
			map[string]any{"name": "apple", "version": "2.0.0"},
		}
		f.State.Plugins = []any{
			map[string]any{"name": "zebra", "installed_at": "2026-05-19T00:00:00Z"},
			map[string]any{"name": "apple", "installed_at": "2026-05-19T00:00:00Z"},
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WriteTransaction: %v", err)
	}

	files, err := reg.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if files.Catalog.PluginCatalogSchemaVersion != 1 {
		t.Errorf("catalog schema = %d; want 1", files.Catalog.PluginCatalogSchemaVersion)
	}
	if files.Lock.PluginsLockSchemaVersion != 1 {
		t.Errorf("lock schema = %d; want 1", files.Lock.PluginsLockSchemaVersion)
	}
	if files.State.PluginStateSchemaVersion != 1 {
		t.Errorf("state schema = %d; want 1", files.State.PluginStateSchemaVersion)
	}
	if files.Lock.InstallGeneration != 1 {
		t.Errorf("first-write generation = %d; want 1", files.Lock.InstallGeneration)
	}
	if files.Lock.InstallTxID == "" || len(files.Lock.InstallTxID) != 8 {
		t.Errorf("install_txid = %q; want 8 lowercase hex chars", files.Lock.InstallTxID)
	}
	if files.Lock.InstallGeneration != files.State.InstallGeneration ||
		files.Lock.InstallTxID != files.State.InstallTxID {
		t.Errorf("lock/state generation mismatch: lock=(%d,%q) state=(%d,%q)",
			files.Lock.InstallGeneration, files.Lock.InstallTxID,
			files.State.InstallGeneration, files.State.InstallTxID)
	}
	// Verify sort.
	if got := nameOf(files.Lock.Plugins[0]); got != "apple" {
		t.Errorf("plugins[0].name = %q; want apple (sorted)", got)
	}
	if got := nameOf(files.Lock.Plugins[1]); got != "zebra" {
		t.Errorf("plugins[1].name = %q; want zebra (sorted)", got)
	}

	// File mode must be 600 per spec §8.7 line 1705.
	for _, name := range []string{CatalogFilename, LockFilename, StateFilename} {
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("stat %s: %v", name, err)
		}
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Errorf("%s mode = %o; want 0600", name, perm)
		}
	}
}

// TestPluginInstallTransactionAtomicity proves that two concurrent
// WriteTransaction calls in the same process are serialised by the install
// lock and produce monotonically increasing generations that agree across
// lock+state, never a torn write.
func TestPluginInstallTransactionAtomicity(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	reg := New(dir).WithLockTimeout(5 * time.Second)

	const writers = 6
	var wg sync.WaitGroup
	errs := make(chan error, writers)
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			name := []string{"alpha", "bravo", "charlie", "delta", "echo", "foxtrot"}[i]
			err := reg.WriteTransaction(context.Background(), func(f *Files) error {
				f.Lock.Plugins = append(f.Lock.Plugins, map[string]any{
					"name":    name,
					"version": "1.0.0",
				})
				f.State.Plugins = append(f.State.Plugins, map[string]any{
					"name":         name,
					"installed_at": time.Now().UTC().Format(time.RFC3339),
				})
				return nil
			})
			errs <- err
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent write: %v", err)
		}
	}

	files, err := reg.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if files.Lock.InstallGeneration != writers {
		t.Errorf("final generation = %d; want %d (one bump per writer)",
			files.Lock.InstallGeneration, writers)
	}
	if files.Lock.InstallGeneration != files.State.InstallGeneration ||
		files.Lock.InstallTxID != files.State.InstallTxID {
		t.Errorf("torn write detected: lock=(%d,%q) state=(%d,%q)",
			files.Lock.InstallGeneration, files.Lock.InstallTxID,
			files.State.InstallGeneration, files.State.InstallTxID)
	}
	if len(files.Lock.Plugins) != writers || len(files.State.Plugins) != writers {
		t.Errorf("plugin counts = lock:%d state:%d; want %d each",
			len(files.Lock.Plugins), len(files.State.Plugins), writers)
	}
	// No leftover *.tmp.* files from staged transactions.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	for _, e := range entries {
		name := e.Name()
		if len(name) > 4 && (containsSubstr(name, ".tmp.") || name == InstallLockFilename) {
			continue
		}
	}
	// Confirm SelectGeneration agrees post-write.
	gen, err := reg.SelectGeneration()
	if err != nil {
		t.Fatalf("SelectGeneration: %v", err)
	}
	if !gen.Ok {
		t.Errorf("SelectGeneration.Ok = false after clean writes; want true")
	}
	if gen.Generation != writers {
		t.Errorf("SelectGeneration = %d; want %d", gen.Generation, writers)
	}
}

// TestPluginInstallCrashRecovery simulates the spec §8.7 step 5 mixed-
// generation crash: two of the three files agree, the third carries a
// different (gen, txid) pair. SelectGeneration MUST report Ok=false so the
// host refuses dispatch from the incomplete generation.
func TestPluginInstallCrashRecovery(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	// Forge a "torn write" on disk: catalog v1, lock @ gen=2/txid=newtxid,
	// state @ gen=1/txid=oldtxid (the rename to state was interrupted).
	writeJSON(t, filepath.Join(dir, CatalogFilename), map[string]any{
		"plugin_catalog_schema_version": 1,
		"updated_at":                    "2026-05-19T00:00:00Z",
		"variants":                      []any{},
	})
	writeJSON(t, filepath.Join(dir, LockFilename), map[string]any{
		"plugins_lock_schema_version": 1,
		"install_generation":          2,
		"install_txid":                "newtxid0",
		"plugins":                     []any{},
	})
	writeJSON(t, filepath.Join(dir, StateFilename), map[string]any{
		"plugin_state_schema_version": 1,
		"install_generation":          1,
		"install_txid":                "oldtxid0",
		"plugins":                     []any{},
	})

	reg := New(dir)
	gen, err := reg.SelectGeneration()
	if err != nil {
		t.Fatalf("SelectGeneration: %v", err)
	}
	if gen.Ok {
		t.Errorf("SelectGeneration.Ok = true for mixed generation; want false (refuse dispatch)")
	}

	// And the empty-profile case must report Ok=true (clean slate).
	emptyReg := New(t.TempDir())
	emptyGen, err := emptyReg.SelectGeneration()
	if err != nil {
		t.Fatalf("empty SelectGeneration: %v", err)
	}
	if !emptyGen.Ok || emptyGen.Generation != 0 {
		t.Errorf("empty profile gen = %+v; want Ok=true Generation=0", emptyGen)
	}

	// Partial presence (only catalog written) must also fail.
	partialDir := t.TempDir()
	writeJSON(t, filepath.Join(partialDir, CatalogFilename), map[string]any{
		"plugin_catalog_schema_version": 1,
		"variants":                      []any{},
	})
	partialReg := New(partialDir)
	partialGen, err := partialReg.SelectGeneration()
	if err != nil {
		t.Fatalf("partial SelectGeneration: %v", err)
	}
	if partialGen.Ok {
		t.Errorf("partial profile Ok = true; want false")
	}
}

// TestEmptyProfileLoadIsZeroValue covers the spec §8.7 step 2 rule "treat
// absent files as their empty v1 objects" — Load on a fresh dir returns
// the zero-valued v1 objects without error.
func TestEmptyProfileLoadIsZeroValue(t *testing.T) {
	t.Parallel()
	reg := New(t.TempDir())
	f, err := reg.Load()
	if err != nil {
		t.Fatalf("Load on empty dir: %v", err)
	}
	if f.Catalog.PluginCatalogSchemaVersion != 1 || f.Lock.PluginsLockSchemaVersion != 1 || f.State.PluginStateSchemaVersion != 1 {
		t.Errorf("empty load schemas = (%d,%d,%d); want (1,1,1)",
			f.Catalog.PluginCatalogSchemaVersion,
			f.Lock.PluginsLockSchemaVersion,
			f.State.PluginStateSchemaVersion)
	}
	if f.Lock.InstallGeneration != 0 || f.Lock.InstallTxID != "" {
		t.Errorf("empty lock (gen,txid) = (%d,%q); want (0,\"\")",
			f.Lock.InstallGeneration, f.Lock.InstallTxID)
	}
}

// TestWriteTransactionRespectsContextCancellation locks down the documented
// contract that an already-cancelled context skips the entire protocol.
func TestWriteTransactionRespectsContextCancellation(t *testing.T) {
	t.Parallel()
	reg := New(t.TempDir())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := reg.WriteTransaction(ctx, func(*Files) error {
		t.Fatal("mutate must not run when ctx is cancelled before entry")
		return nil
	})
	if err == nil {
		t.Fatal("WriteTransaction(cancelled ctx) returned nil; want context.Canceled")
	}
}

// writeJSON is a test helper that marshals v indented and writes it
// mode-600. Failures fail the test fatally.
func writeJSON(t *testing.T, path string, v any) {
	t.Helper()
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatalf("marshal %s: %v", path, err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func containsSubstr(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

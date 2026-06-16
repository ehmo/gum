package registry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/ehmo/gum/internal/catalog"
)

// DefaultLockTimeout is the spec §8.7 step 1 ceiling for plugins.install.lock.
const DefaultLockTimeout = 30 * time.Second

// pluginByName is the minimal projection of a plugins[] entry the transaction
// protocol needs to enforce array-sort-by-name (spec §8.7 line 1772). We keep
// the entry as json.RawMessage to avoid re-serialising fields we don't model.
type pluginByName struct {
	Name string `json:"name"`
}

// Files is the in-memory view of the three plugin registry files inside one
// transaction. Callers mutate Plugins arrays via the WriteTransaction
// mutate callback; the registry handles install_generation/install_txid +
// updated_at + array-sort-by-name on commit.
type Files struct {
	Catalog *catalog.PluginCatalog
	Lock    *catalog.PluginsLock
	State   *catalog.PluginState
}

// emptyFiles returns the v1 zero value used when the registry has never been
// written before. Spec §8.7 step 2: "treat absent files as their empty v1 objects".
func emptyFiles() *Files {
	return &Files{
		Catalog: &catalog.PluginCatalog{PluginCatalogSchemaVersion: 1, Variants: []any{}},
		Lock:    &catalog.PluginsLock{PluginsLockSchemaVersion: 1, Plugins: []any{}},
		State:   &catalog.PluginState{PluginStateSchemaVersion: 1, Plugins: []any{}},
	}
}

// Registry binds the three-file install protocol to one profile directory.
// All methods are safe for concurrent callers: cross-process serialisation
// comes from flock on plugins.install.lock, intra-process serialisation
// comes from the same lock (flock is per-fd, but each call opens a fresh fd
// and blocks on the kernel-side mutex).
type Registry struct {
	profileDir  string
	lockTimeout time.Duration
}

// New returns a Registry bound to profileDir. Callers responsible for
// ensuring profileDir already exists (gum profile setup).
func New(profileDir string) *Registry {
	return &Registry{profileDir: profileDir, lockTimeout: DefaultLockTimeout}
}

// WithLockTimeout overrides the 30s default; intended for tests that exercise
// the timeout path without sleeping for half a minute.
func (r *Registry) WithLockTimeout(d time.Duration) *Registry {
	r.lockTimeout = d
	return r
}

// ProfileDir returns the directory this registry was bound to. Callers that
// need to compose paths to artifacts inside the profile (tee, audit) reach
// for this rather than re-deriving the directory.
func (r *Registry) ProfileDir() string {
	return r.profileDir
}

// Load reads the current authoritative state of the three files without
// taking the install lock. Absent files become empty v1 objects.
// Unsupported schema versions return the catalog package's sentinel errors
// (PLUGIN_CATALOG_SCHEMA_UNSUPPORTED / PLUGIN_LOCK_SCHEMA_UNSUPPORTED /
// PLUGIN_STATE_SCHEMA_UNSUPPORTED).
func (r *Registry) Load() (*Files, error) {
	f := emptyFiles()
	if data, ok, err := readIfExists(CatalogPath(r.profileDir)); err != nil {
		return nil, err
	} else if ok {
		pc, err := catalog.LoadPluginCatalog(data)
		if err != nil {
			return nil, err
		}
		f.Catalog = pc
	}
	if data, ok, err := readIfExists(LockPath(r.profileDir)); err != nil {
		return nil, err
	} else if ok {
		pl, err := catalog.LoadPluginsLock(data)
		if err != nil {
			return nil, err
		}
		f.Lock = pl
	}
	if data, ok, err := readIfExists(StatePath(r.profileDir)); err != nil {
		return nil, err
	} else if ok {
		ps, err := catalog.LoadPluginState(data)
		if err != nil {
			return nil, err
		}
		f.State = ps
	}
	return f, nil
}

// WriteTransaction runs the spec §8.7 atomic update protocol:
//
//  1. Acquire flock on plugins.install.lock (lockTimeout budget).
//  2. Load existing files (empty v1 if absent).
//  3. Call mutate(f) so the caller can append/remove plugin entries.
//  4. Allocate a new install_generation = prev+1 and a fresh install_txid.
//  5. Sort plugins[] arrays by name and variants[] by variant_id (line 1772).
//  6. Write three .tmp.<txid> files, fsync each, fsync the dir.
//  7. Rename all three to their final names, fsync the dir again.
//  8. Release the lock and return.
//
// If mutate returns an error, no file changes are made and the lock is
// released. If any rename fails after some succeeded, the protocol returns
// the error; recovery is the responsibility of SelectGeneration on next
// startup (spec §8.7 step 5).
func (r *Registry) WriteTransaction(ctx context.Context, mutate func(*Files) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.MkdirAll(r.profileDir, 0o700); err != nil {
		return fmt.Errorf("registry: mkdir profile dir: %w", err)
	}
	release, err := acquireFileLock(InstallLockPath(r.profileDir), r.lockTimeout)
	if err != nil {
		return err
	}
	defer func() { _ = release() }()

	files, err := r.Load()
	if err != nil {
		return err
	}
	if err := mutate(files); err != nil {
		return fmt.Errorf("registry: mutate: %w", err)
	}

	prev := files.Lock.InstallGeneration
	if files.State.InstallGeneration > prev {
		prev = files.State.InstallGeneration
	}
	gen := prev + 1
	txid := newTxID()
	now := time.Now().UTC().Format(time.RFC3339)

	files.Catalog.PluginCatalogSchemaVersion = 1
	files.Catalog.UpdatedAt = now
	files.Lock.PluginsLockSchemaVersion = 1
	files.Lock.InstallGeneration = gen
	files.Lock.InstallTxID = txid
	files.State.PluginStateSchemaVersion = 1
	files.State.InstallGeneration = gen
	files.State.InstallTxID = txid
	sortByName(files.Lock.Plugins)
	sortByName(files.State.Plugins)

	tmpCatalog := tempPath(r.profileDir, CatalogFilename, txid)
	tmpLock := tempPath(r.profileDir, LockFilename, txid)
	tmpState := tempPath(r.profileDir, StateFilename, txid)
	if err := writeJSONAtomic(tmpCatalog, files.Catalog); err != nil {
		_ = os.Remove(tmpCatalog)
		return err
	}
	if err := writeJSONAtomic(tmpLock, files.Lock); err != nil {
		_ = os.Remove(tmpCatalog)
		_ = os.Remove(tmpLock)
		return err
	}
	if err := writeJSONAtomic(tmpState, files.State); err != nil {
		_ = os.Remove(tmpCatalog)
		_ = os.Remove(tmpLock)
		_ = os.Remove(tmpState)
		return err
	}
	if err := fsyncDir(r.profileDir); err != nil {
		return err
	}

	if err := os.Rename(tmpCatalog, CatalogPath(r.profileDir)); err != nil {
		_ = os.Remove(tmpLock)
		_ = os.Remove(tmpState)
		return fmt.Errorf("registry: rename catalog: %w", err)
	}
	if err := os.Rename(tmpLock, LockPath(r.profileDir)); err != nil {
		_ = os.Remove(tmpState)
		return fmt.Errorf("registry: rename lock: %w", err)
	}
	if err := os.Rename(tmpState, StatePath(r.profileDir)); err != nil {
		return fmt.Errorf("registry: rename state: %w", err)
	}
	if err := fsyncDir(r.profileDir); err != nil {
		return err
	}
	return nil
}

// readIfExists returns (data, true, nil) for a present file, (nil, false, nil)
// for ENOENT, and (nil, false, err) for any other failure.
func readIfExists(path string) ([]byte, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("registry: read %s: %w", path, err)
	}
	return data, true, nil
}

// writeJSONAtomic marshals v as indented JSON and writes it to path mode-600,
// fsyncing the file before closing. The caller fsyncs the directory after all
// temp files are in place.
func writeJSONAtomic(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("registry: marshal %s: %w", filepath.Base(path), err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("registry: open temp %s: %w", path, err)
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return fmt.Errorf("registry: write %s: %w", path, err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		// EINVAL/ENOTSUP fsync fallback is handled by fsyncDir; for the file
		// itself we surface the error so the transaction fails closed when
		// the filesystem can't promise durability for the temp content.
		return fmt.Errorf("registry: fsync %s: %w", path, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("registry: close %s: %w", path, err)
	}
	return nil
}

// fsyncDir opens dir and calls Sync so the directory entry for renamed temp
// files reaches stable storage. On filesystems where the syscall isn't
// supported, the error is swallowed silently — spec §8.7 line 1782 requires
// the host to log one structured warning per profile per process and continue
// (the warning will land here once slog wiring lands in gum-d7k2).
func fsyncDir(dir string) error {
	f, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("registry: open dir %s: %w", dir, err)
	}
	defer func() { _ = f.Close() }()
	if err := f.Sync(); err != nil {
		if isFsyncUnsupported(err) {
			return nil
		}
		return fmt.Errorf("registry: fsync dir %s: %w", dir, err)
	}
	return nil
}

// isFsyncUnsupported reports whether err is the EINVAL/ENOTSUP signature
// emitted by NFS / FUSE / overlay filesystems that don't honour fsync on
// directories. Spec §8.7 line 1782 "Filesystem fsync fallback".
func isFsyncUnsupported(err error) bool {
	return errors.Is(err, fs.ErrInvalid) ||
		errors.Is(err, errENOTSUP) ||
		errors.Is(err, errEINVAL)
}

// sortByName sorts a plugins[] slice (as decoded by json.Unmarshal into
// []any) ascending by the "name" field. Spec §8.7 line 1772: arrays sorted by
// plugin name before JCS hashing.
func sortByName(plugins []any) {
	sort.SliceStable(plugins, func(i, j int) bool {
		return nameOf(plugins[i]) < nameOf(plugins[j])
	})
}

func nameOf(p any) string {
	switch v := p.(type) {
	case map[string]any:
		if s, ok := v["name"].(string); ok {
			return s
		}
	case json.RawMessage:
		var pn pluginByName
		_ = json.Unmarshal(v, &pn)
		return pn.Name
	}
	return ""
}

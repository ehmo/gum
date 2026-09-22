package registry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"sync"
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

// variantByID is the same minimal projection for a plugin-catalog.json
// variants[] entry, which spec §8.7 line 1884 sorts by variant_id.
type variantByID struct {
	VariantID string `json:"variant_id"`
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

	// Publish-step seams. Production leaves them nil and the transaction uses
	// os.Rename, fsyncDir, and (*os.File).Sync. Only this package's own
	// error-path tests set them, because a rename failure or an
	// fsync-unsupported filesystem cannot be induced portably from a temp dir.
	renameFn   func(oldpath, newpath string) error
	syncDirFn  func(dir string) error
	syncFileFn func(f *os.File) error

	// audit receives the §8.7 fsync warning as an audit row. nil means the
	// warning goes to the host log only.
	audit AuditSink

	// logger is the §14.1 rule 2 injection point, set by WithLogger. nil
	// means slog.Default().
	logger *slog.Logger
}

// log returns the injected logger, or slog.Default() when WithLogger was not
// chained. Every diagnostic in this package goes through it, so a registry
// built with WithLogger(slog.New(slog.DiscardHandler)) emits nothing.
func (r *Registry) log() *slog.Logger {
	if r.logger != nil {
		return r.logger
	}
	return slog.Default()
}

// AuditSink is the seam that carries a registry warning into the profile audit
// log. cmd wires it to internal/auditlog; this package must not import that
// package, because §14 lets a layer call only the layer directly below it.
type AuditSink interface {
	Append(entry map[string]any)
}

// WithAuditSink attaches the audit sink and returns the registry, so a caller
// can chain it onto New.
func (r *Registry) WithAuditSink(s AuditSink) *Registry {
	r.audit = s
	return r
}

func (r *Registry) rename(oldpath, newpath string) error {
	if r.renameFn != nil {
		return r.renameFn(oldpath, newpath)
	}
	return os.Rename(oldpath, newpath)
}

func (r *Registry) syncFile() func(*os.File) error {
	if r.syncFileFn != nil {
		return r.syncFileFn
	}
	return (*os.File).Sync
}

// New returns a Registry bound to profileDir. Callers responsible for
// ensuring profileDir already exists (gum profile setup).
func New(profileDir string) *Registry {
	return &Registry{profileDir: profileDir, lockTimeout: DefaultLockTimeout}
}

// WithLogger injects the logger this registry emits its diagnostics through
// (spec §14.1 rule 2) and returns the registry, so a caller can chain it onto
// New. A nil logger leaves the registry on slog.Default().
func (r *Registry) WithLogger(l *slog.Logger) *Registry {
	r.logger = l
	return r
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

	// The highest generation on disk wins, including the catalog's. After a
	// catalog-only tear the catalog holds the newer number, and reusing it
	// would break the "monotonically increasing" rule in §8.7 step 3.
	prev := max(files.Catalog.InstallGeneration, files.Lock.InstallGeneration, files.State.InstallGeneration)
	gen := prev + 1
	txid := newTxID()
	now := time.Now().UTC().Format(time.RFC3339)

	files.Catalog.PluginCatalogSchemaVersion = 1
	files.Catalog.InstallGeneration = gen
	files.Catalog.InstallTxID = txid
	files.Catalog.UpdatedAt = now
	files.Lock.PluginsLockSchemaVersion = 1
	files.Lock.InstallGeneration = gen
	files.Lock.InstallTxID = txid
	files.State.PluginStateSchemaVersion = 1
	files.State.InstallGeneration = gen
	files.State.InstallTxID = txid
	sortByName(files.Lock.Plugins)
	sortByName(files.State.Plugins)
	sortByVariantID(files.Catalog.Variants)

	steps := []publishStep{
		{what: "catalog", final: CatalogPath(r.profileDir), tmp: tempPath(r.profileDir, CatalogFilename, txid), body: files.Catalog},
		{what: "lock", final: LockPath(r.profileDir), tmp: tempPath(r.profileDir, LockFilename, txid), body: files.Lock},
		{what: "state", final: StatePath(r.profileDir), tmp: tempPath(r.profileDir, StateFilename, txid), body: files.State},
	}

	// Snapshot the bytes of the generation currently on disk. A rename that
	// fails partway would otherwise leave one file at generation N and the
	// other two at N-1, and spec §8.7 step 5 requires the previous complete
	// generation to stay authoritative. The three files are small JSON
	// documents, so holding them in memory for the publish window is cheap.
	for i := range steps {
		prior, existed, err := readIfExists(steps[i].final)
		if err != nil {
			removeTemps(steps)
			return err
		}
		steps[i].prior, steps[i].existed = prior, existed
	}

	for i := range steps {
		if err := r.stageTemp(steps[i].tmp, steps[i].body); err != nil {
			removeTemps(steps)
			return err
		}
	}
	if err := r.syncProfileDir(); err != nil {
		removeTemps(steps)
		return err
	}

	for i := range steps {
		if err := r.rename(steps[i].tmp, steps[i].final); err != nil {
			return r.rollbackPublish(steps, i, fmt.Errorf("registry: rename %s: %w", steps[i].what, err))
		}
	}
	if err := r.syncProfileDir(); err != nil {
		return r.rollbackPublish(steps, len(steps), err)
	}
	return nil
}

// publishStep is one file's journey through the §8.7 publish: stage to tmp,
// rename over final, and, if a later step fails, restore prior.
type publishStep struct {
	what    string
	final   string
	tmp     string
	body    any
	prior   []byte
	existed bool
}

// stageTemp writes one temp file under the §8.7 fsync fallback.
func (r *Registry) stageTemp(path string, v any) error {
	return r.tolerateUnsupportedFsync(writeJSONWithSync(path, v, r.syncFile()))
}

// rollbackPublish undoes a publish that failed at step `failed`, so the
// previous complete generation stays authoritative (spec §8.7 step 5). Steps
// before `failed` already renamed, so each is restored from its snapshot, or
// deleted when the file did not exist before. Steps from `failed` on still
// have a temp file to delete.
//
// Every step is best effort. A restore that itself fails is reported next to
// the original error, because the operator then has a torn generation that
// SelectGeneration will refuse to dispatch from.
func (r *Registry) rollbackPublish(steps []publishStep, failed int, cause error) error {
	removeTemps(steps[failed:])
	var restoreErrs []error
	for i := failed - 1; i >= 0; i-- {
		if err := r.restore(steps[i]); err != nil {
			restoreErrs = append(restoreErrs, err)
		}
	}
	if len(restoreErrs) == 0 {
		return cause
	}
	return fmt.Errorf("%w; rollback incomplete: %w", cause, errors.Join(restoreErrs...))
}

// restore puts one already-renamed file back to its pre-transaction content.
// The write goes through a temp plus rename so a crash mid-rollback cannot
// leave a half-written final file.
func (r *Registry) restore(step publishStep) error {
	if !step.existed {
		if err := os.Remove(step.final); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("registry: rollback remove %s: %w", step.what, err)
		}
		return nil
	}
	tmp := step.final + ".rollback"
	if err := os.WriteFile(tmp, step.prior, 0o600); err != nil {
		return fmt.Errorf("registry: rollback stage %s: %w", step.what, err)
	}
	if err := r.rename(tmp, step.final); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("registry: rollback restore %s: %w", step.what, err)
	}
	return nil
}

// removeTemps deletes the staging files for the given steps on best effort,
// which spec §8.7 step 5 requires of every failed transaction.
func removeTemps(steps []publishStep) {
	for _, s := range steps {
		_ = os.Remove(s.tmp)
	}
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
	return writeJSONWithSync(path, v, (*os.File).Sync)
}

// writeJSONWithSync is writeJSONAtomic with an injectable file-sync call so the
// package's own tests can exercise the unsupported-fsync fallback.
func writeJSONWithSync(path string, v any, sync func(*os.File) error) error {
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
	syncErr := sync(f)
	if err := f.Close(); err != nil {
		return fmt.Errorf("registry: close %s: %w", path, err)
	}
	if syncErr == nil {
		return nil
	}
	wrapped := fmt.Errorf("registry: fsync %s: %w", path, syncErr)
	// The §8.7 fallback applies to the temp file too: on a filesystem that
	// cannot fsync, the install must not fail. The typed wrapper keeps that
	// decision at the caller and stops an EINVAL from open or write being
	// mistaken for an unsupported syscall.
	if isFsyncUnsupported(syncErr) {
		return fsyncUnsupportedError{err: wrapped}
	}
	return wrapped
}

// fsyncUnsupportedError marks an fsync failure that the filesystem does not
// support (NFS, FUSE, some overlay mounts). Spec §8.7 "Filesystem fsync
// fallback" requires the install to continue after one structured warning.
type fsyncUnsupportedError struct{ err error }

func (e fsyncUnsupportedError) Error() string { return e.err.Error() }
func (e fsyncUnsupportedError) Unwrap() error { return e.err }

// fsyncDir opens dir and calls Sync so the directory entry for renamed temp
// files reaches stable storage. A syscall the filesystem does not support
// comes back as fsyncUnsupportedError; the caller applies the §8.7 fallback
// (one structured warning per profile per process, then continue). Every other
// failure is a real error.
func fsyncDir(dir string) error {
	f, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("registry: open dir %s: %w", dir, err)
	}
	defer func() { _ = f.Close() }()
	if err := f.Sync(); err != nil {
		wrapped := fmt.Errorf("registry: fsync dir %s: %w", dir, err)
		if isFsyncUnsupported(err) {
			return fsyncUnsupportedError{err: wrapped}
		}
		return wrapped
	}
	return nil
}

// syncProfileDir fsyncs the profile directory and applies the §8.7 fallback.
func (r *Registry) syncProfileDir() error {
	sync := fsyncDir
	if r.syncDirFn != nil {
		sync = r.syncDirFn
	}
	return r.tolerateUnsupportedFsync(sync(r.profileDir))
}

// tolerateUnsupportedFsync turns an unsupported-syscall fsync failure into a
// single structured warning and a nil error, per the §8.7 fallback. Any other
// error passes through.
func (r *Registry) tolerateUnsupportedFsync(err error) error {
	var unsupported fsyncUnsupportedError
	if !errors.As(err, &unsupported) {
		return err
	}
	r.warnFsyncUnsupported(unsupported.err)
	return nil
}

// fsyncWarned records the profile directories already warned about in this
// process. Spec §8.7: detection is once per profile per process, not once per
// install transaction.
var fsyncWarned sync.Map

// warnFsyncUnsupported emits the §8.7 warning to both required sinks: the host
// log and the profile audit log, "so the loss of crash-safety guarantees is
// auditable". Both legs sit under one dedupe guard, because §8.7 scopes
// detection to once per profile per process rather than once per transaction.
func (r *Registry) warnFsyncUnsupported(err error) {
	if _, seen := fsyncWarned.LoadOrStore(r.profileDir, struct{}{}); seen {
		return
	}
	const suggestion = "Install to a local filesystem for atomic write guarantees."
	errno := errnoName(err)
	r.log().Warn("fsync_not_supported",
		"event", "fsync_not_supported",
		"path", r.profileDir,
		"syscall_errno", errno,
		"suggestion", suggestion)
	if r.audit == nil {
		return
	}
	r.audit.Append(map[string]any{
		"event_type":    "fsync_not_supported",
		"path":          r.profileDir,
		"syscall_errno": errno,
		"suggestion":    suggestion,
		"emitted_at":    time.Now().UTC().Format(time.RFC3339),
	})
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

// sortByVariantID sorts a plugin-catalog.json variants[] slice ascending by
// "variant_id". Spec §8.7 line 1884: variants sorted by variant_id before JCS
// hashing, so two profiles that installed the same plugins in a different
// order produce byte-identical catalogs.
func sortByVariantID(variants []any) {
	sort.SliceStable(variants, func(i, j int) bool {
		return variantIDOf(variants[i]) < variantIDOf(variants[j])
	})
}

func variantIDOf(v any) string {
	switch t := v.(type) {
	case map[string]any:
		if s, ok := t["variant_id"].(string); ok {
			return s
		}
	case json.RawMessage:
		var vi variantByID
		_ = json.Unmarshal(t, &vi)
		return vi.VariantID
	}
	return ""
}

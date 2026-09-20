package registry

// gum-t2x3: the §8.7 publish step leaked temp files when the directory fsync
// failed, and a rename that failed partway left the catalog published at
// generation N with the lock and state still at N-1. Spec §8.7 step 5 requires
// the previous complete generation to stay authoritative, temp files to be
// deleted on best effort, and an unsupported fsync not to fail the install.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// tempsLeft lists the transaction staging files still present in dir.
func tempsLeft(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read profile dir: %v", err)
	}
	var out []string
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp.") || strings.Contains(e.Name(), ".rollback.") {
			out = append(out, e.Name())
		}
	}
	return out
}

// addPlugin is the smallest mutate that changes all three files.
func addPlugin(name string) func(*Files) error {
	return func(f *Files) error {
		f.Lock.Plugins = append(f.Lock.Plugins, map[string]any{"name": name})
		f.State.Plugins = append(f.State.Plugins, map[string]any{"name": name})
		f.Catalog.Variants = append(f.Catalog.Variants, map[string]any{"variant_id": name + ".v1"})
		return nil
	}
}

// failFor returns a rename that fails only when publishing to finalPath, which
// is how a real partial publish behaves: one target is unwritable, the rest
// (including the rollback's restores) still work.
func failFor(finalPath string, sentinel error) func(string, string) error {
	return func(oldpath, newpath string) error {
		if newpath == finalPath {
			return sentinel
		}
		return os.Rename(oldpath, newpath)
	}
}

func TestWriteTransactionRemovesTempsWhenDirSyncFails(t *testing.T) {
	dir := t.TempDir()
	boom := errors.New("dir-sync-boom")
	r := New(dir)
	r.syncDirFn = func(string) error { return boom }

	err := r.WriteTransaction(context.Background(), addPlugin("acme"))
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v; want wraps dir-sync-boom", err)
	}
	if left := tempsLeft(t, dir); len(left) != 0 {
		t.Errorf("temp files left behind: %v", left)
	}
}

func TestWriteTransactionRollsBackTornPublish(t *testing.T) {
	dir := t.TempDir()
	r := New(dir)
	if err := r.WriteTransaction(context.Background(), addPlugin("acme")); err != nil {
		t.Fatalf("first transaction: %v", err)
	}
	before := readAll(t, dir)

	boom := errors.New("rename-boom")
	torn := New(dir)
	torn.renameFn = failFor(LockPath(dir), boom) // catalog publishes, lock fails

	err := torn.WriteTransaction(context.Background(), addPlugin("beta"))
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v; want wraps rename-boom", err)
	}

	gen, err := r.SelectGeneration()
	if err != nil {
		t.Fatalf("SelectGeneration: %v", err)
	}
	if !gen.Ok {
		t.Errorf("SelectGeneration.Ok = false after rollback; want the previous generation authoritative")
	}
	if gen.Generation != 1 {
		t.Errorf("generation = %d; want 1", gen.Generation)
	}
	if after := readAll(t, dir); !bytes.Equal(before, after) {
		t.Errorf("registry bytes changed after a rolled-back transaction")
	}
	if left := tempsLeft(t, dir); len(left) != 0 {
		t.Errorf("temp files left behind: %v", left)
	}
}

func TestWriteTransactionRollbackRemovesFirstEverPublish(t *testing.T) {
	dir := t.TempDir()
	boom := errors.New("rename-boom")
	r := New(dir)
	r.renameFn = failFor(LockPath(dir), boom)

	if err := r.WriteTransaction(context.Background(), addPlugin("acme")); !errors.Is(err, boom) {
		t.Fatalf("err = %v; want wraps rename-boom", err)
	}
	if _, err := os.Stat(CatalogPath(dir)); !os.IsNotExist(err) {
		t.Errorf("catalog exists after rollback of the first publish; stat err = %v", err)
	}
	if left := tempsLeft(t, dir); len(left) != 0 {
		t.Errorf("temp files left behind: %v", left)
	}
}

// Spec §8.7 "Filesystem fsync fallback": the install MUST NOT fail on a
// filesystem that cannot fsync, and the host logs one warning per profile per
// process.
func TestWriteTransactionSurvivesUnsupportedFsync(t *testing.T) {
	dir := t.TempDir()
	logs := &bytes.Buffer{}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(logs, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	r := New(dir)
	r.syncDirFn = func(string) error {
		return fsyncUnsupportedError{err: syscall.ENOTSUP}
	}
	r.syncFileFn = func(*os.File) error { return syscall.EINVAL }

	if err := r.WriteTransaction(context.Background(), addPlugin("acme")); err != nil {
		t.Fatalf("transaction failed on an fsync-unsupported filesystem: %v", err)
	}
	gen, err := r.SelectGeneration()
	if err != nil || !gen.Ok || gen.Generation != 1 {
		t.Fatalf("SelectGeneration = %+v, err=%v; want a complete generation 1", gen, err)
	}

	var warned int
	for _, line := range strings.Split(strings.TrimSpace(logs.String()), "\n") {
		var rec map[string]any
		if json.Unmarshal([]byte(line), &rec) != nil {
			continue
		}
		if rec["event"] == "fsync_not_supported" {
			warned++
			if rec["path"] != dir {
				t.Errorf("warning path = %v; want %s", rec["path"], dir)
			}
			if rec["syscall_errno"] == "unknown" || rec["syscall_errno"] == nil {
				t.Errorf("warning syscall_errno = %v; want a named errno", rec["syscall_errno"])
			}
		}
	}
	if warned != 1 {
		t.Errorf("fsync_not_supported warnings = %d; want exactly 1 per profile per process", warned)
	}

	// A second transaction in the same process must not warn again.
	logs.Reset()
	if err := r.WriteTransaction(context.Background(), addPlugin("beta")); err != nil {
		t.Fatalf("second transaction: %v", err)
	}
	if strings.Contains(logs.String(), "fsync_not_supported") {
		t.Errorf("warning repeated for the same profile: %s", logs.String())
	}
}

// A rollback that cannot restore a file must say so: the operator is left with
// a torn generation that SelectGeneration refuses to dispatch from.
func TestWriteTransactionReportsIncompleteRollback(t *testing.T) {
	dir := t.TempDir()
	if err := New(dir).WriteTransaction(context.Background(), addPlugin("acme")); err != nil {
		t.Fatalf("first transaction: %v", err)
	}

	boom := errors.New("rename-boom")
	r := New(dir)
	// Every rename after the catalog fails, so the restore fails too.
	calls := 0
	r.renameFn = func(oldpath, newpath string) error {
		calls++
		if calls > 1 {
			return boom
		}
		return os.Rename(oldpath, newpath)
	}

	err := r.WriteTransaction(context.Background(), addPlugin("beta"))
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v; want wraps rename-boom", err)
	}
	if !strings.Contains(err.Error(), "rollback incomplete") {
		t.Errorf("err = %v; want it to name the incomplete rollback", err)
	}
	if left := tempsLeft(t, dir); len(left) != 0 {
		t.Errorf("temp files left behind: %v", left)
	}
	// The catalog kept the new generation's content while the lock kept the
	// old one. SelectGeneration cannot see this tear, because the v1
	// plugin-catalog.json shape carries no install_generation (recover.go:45,
	// and the spec's own §8.7 example) — tracked separately. The error string
	// is what tells the operator, which is why the "rollback incomplete" note
	// above is load-bearing.
	catalogBytes, rerr := os.ReadFile(CatalogPath(dir))
	if rerr != nil {
		t.Fatalf("read catalog: %v", rerr)
	}
	if !strings.Contains(string(catalogBytes), "beta") {
		t.Errorf("catalog was restored after all; this test no longer covers the incomplete-rollback path")
	}
	lockBytes, rerr := os.ReadFile(LockPath(dir))
	if rerr != nil {
		t.Fatalf("read lock: %v", rerr)
	}
	if strings.Contains(string(lockBytes), "beta") {
		t.Errorf("lock published despite a failed rename")
	}
}

// readAll concatenates the three final registry files so a test can assert
// that a rolled-back transaction left every byte untouched.
func readAll(t *testing.T, dir string) []byte {
	t.Helper()
	var out []byte
	for _, p := range []string{CatalogPath(dir), LockPath(dir), StatePath(dir)} {
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("read %s: %v", filepath.Base(p), err)
		}
		out = append(out, data...)
	}
	return out
}

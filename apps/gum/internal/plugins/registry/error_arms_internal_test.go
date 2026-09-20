//go:build !windows

package registry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"syscall"
	"testing"
)

// errnoName feeds the §8.7 fsync_not_supported warning, so an operator reading
// the audit row sees the syscall that refused. Every branch has to name
// something: the three known errnos, the raw errno text for anything else, and
// "unknown" when the error carries no errno at all.
func TestErrnoNameNamesEveryErrno(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"no_errno", errors.New("plain"), "unknown"},
		{"nil", nil, "unknown"},
		{"einval", syscall.EINVAL, "EINVAL"},
		{"enotsup", syscall.ENOTSUP, "ENOTSUP"},
		{"enosys", syscall.ENOSYS, "ENOSYS"},
		{"wrapped", fmt.Errorf("fsync: %w", syscall.ENOTSUP), "ENOTSUP"},
		{"other_errno", syscall.EPERM, syscall.EPERM.Error()},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := errnoName(tc.err); got != tc.want {
				t.Errorf("errnoName(%v) = %q, want %q", tc.err, got, tc.want)
			}
		})
	}
}

// The typed wrapper has to stay transparent: tolerateUnsupportedFsync matches
// it with errors.As, and the caller still reaches the underlying errno with
// errors.Is.
func TestFsyncUnsupportedErrorStaysTransparent(t *testing.T) {
	cause := fmt.Errorf("registry: fsync dir /x: %w", syscall.ENOTSUP)
	wrapper := fsyncUnsupportedError{err: cause}

	if wrapper.Error() != cause.Error() {
		t.Errorf("Error() = %q, want %q", wrapper.Error(), cause.Error())
	}
	if !errors.Is(wrapper, syscall.ENOTSUP) {
		t.Error("errors.Is did not reach the errno through Unwrap")
	}
	var target fsyncUnsupportedError
	if !errors.As(error(wrapper), &target) {
		t.Error("errors.As did not match the wrapper type")
	}
}

// A character device accepts the open and refuses the fsync, which is the one
// portable way to drive fsyncDir's failure arm. Which arm it takes depends on
// the errno the platform reports, so the test asserts the classification the
// code itself would make rather than hardcoding one.
func TestFsyncDirSurfacesASyncFailure(t *testing.T) {
	err := fsyncDir("/dev/null")
	if err == nil {
		t.Fatal("fsyncDir(/dev/null) = nil, want the sync failure")
	}
	if !strings.Contains(err.Error(), "fsync dir /dev/null") {
		t.Errorf("err = %v, want one naming the directory", err)
	}

	var unsupported fsyncUnsupportedError
	if errors.As(err, &unsupported) != isFsyncUnsupported(errors.Unwrap(err)) {
		t.Errorf("err = %#v; the typed wrapper disagrees with isFsyncUnsupported", err)
	}
}

// Catalog variants arrive either as decoded maps (a freshly mutated file) or as
// raw JSON (a file round-tripped through Load). Sorting has to key both, and
// anything it cannot key sorts as the empty id rather than panicking.
func TestVariantIDOfReadsBothEncodings(t *testing.T) {
	cases := []struct {
		name string
		in   any
		want string
	}{
		{"map", map[string]any{"variant_id": "a.v1"}, "a.v1"},
		{"map_without_id", map[string]any{"name": "a"}, ""},
		{"map_with_non_string_id", map[string]any{"variant_id": 7}, ""},
		{"raw", json.RawMessage(`{"variant_id":"b.v1"}`), "b.v1"},
		{"raw_without_id", json.RawMessage(`{"name":"b"}`), ""},
		{"raw_invalid", json.RawMessage(`not json`), ""},
		{"unknown_kind", 42, ""},
		{"nil", nil, ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := variantIDOf(tc.in); got != tc.want {
				t.Errorf("variantIDOf(%v) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// An fsync failure the filesystem does support is a real error. Staging must
// abort on it and leave no temp file behind, because §8.7 step 5 only tolerates
// the unsupported-syscall case.
func TestWriteTransactionAbortsOnARealStagingFsyncFailure(t *testing.T) {
	dir := t.TempDir()
	r := New(dir)
	r.syncFileFn = func(*os.File) error { return syscall.EIO }

	err := r.WriteTransaction(context.Background(), addPlugin("alpha"))
	if !errors.Is(err, syscall.EIO) {
		t.Fatalf("WriteTransaction err = %v, want the EIO fsync failure", err)
	}
	if left := tempsLeft(t, dir); len(left) > 0 {
		t.Errorf("temps left behind: %v", left)
	}
	for _, path := range []string{CatalogPath(dir), LockPath(dir), StatePath(dir)} {
		if _, statErr := os.Stat(path); !errors.Is(statErr, os.ErrNotExist) {
			t.Errorf("%s exists; an aborted transaction must publish nothing", path)
		}
	}
}

// The publish fsyncs the directory twice: once with the temps in place and
// once after the renames. A failure on the second call comes after every file
// is already published, so the rollback has to unwind all three.
func TestWriteTransactionRollsBackWhenTheFinalDirSyncFails(t *testing.T) {
	dir := t.TempDir()
	boom := errors.New("dir sync refused")
	r := New(dir)
	calls := 0
	r.syncDirFn = func(string) error {
		calls++
		if calls >= 2 {
			return boom
		}
		return nil
	}

	err := r.WriteTransaction(context.Background(), addPlugin("alpha"))
	if !errors.Is(err, boom) {
		t.Fatalf("WriteTransaction err = %v, want the second dir sync failure", err)
	}
	if calls != 2 {
		t.Errorf("syncDir calls = %d, want 2", calls)
	}
	for _, path := range []string{CatalogPath(dir), LockPath(dir), StatePath(dir)} {
		if _, statErr := os.Stat(path); !errors.Is(statErr, os.ErrNotExist) {
			t.Errorf("%s survived; nothing existed before, so rollback must remove it", path)
		}
	}
	if left := tempsLeft(t, dir); len(left) > 0 {
		t.Errorf("temps left behind: %v", left)
	}
}

// Before publishing, the transaction snapshots the generation already on disk
// so a partial rename can be undone. A snapshot it cannot read is fatal: going
// ahead would publish with no way back.
func TestWriteTransactionRefusesAnUnreadablePriorGeneration(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses permission bits")
	}
	dir := t.TempDir()
	r := New(dir)
	if err := r.WriteTransaction(context.Background(), addPlugin("alpha")); err != nil {
		t.Fatalf("seed transaction: %v", err)
	}

	// The chmod runs inside mutate, after Load has read the file and before
	// the snapshot reads it again. That is the only window where the two
	// disagree.
	locked := CatalogPath(dir)
	t.Cleanup(func() { _ = os.Chmod(locked, 0o600) })
	err := r.WriteTransaction(context.Background(), func(f *Files) error {
		if err := addPlugin("beta")(f); err != nil {
			return err
		}
		return os.Chmod(locked, 0o000)
	})
	if err == nil || !strings.Contains(err.Error(), "registry: read ") {
		t.Fatalf("WriteTransaction err = %v, want the snapshot read failure", err)
	}
	if left := tempsLeft(t, dir); len(left) > 0 {
		t.Errorf("temps left behind: %v", left)
	}
}

// A rollback that cannot finish is worse than the failure that triggered it:
// the profile is left with a torn generation. Both restore arms therefore
// report their own failure alongside the original cause.
func TestRollbackReportsItsOwnFailures(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses permission bits")
	}

	// lockAtState makes the profile directory unwritable at the moment the
	// third rename fails, so the rollback of the first two cannot write.
	lockAtState := func(t *testing.T, dir string, boom error) func(string, string) error {
		t.Helper()
		t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
		return func(oldpath, newpath string) error {
			if newpath != StatePath(dir) {
				return os.Rename(oldpath, newpath)
			}
			if err := os.Chmod(dir, 0o500); err != nil {
				t.Fatalf("chmod %s: %v", dir, err)
			}
			return boom
		}
	}

	t.Run("remove", func(t *testing.T) {
		dir := t.TempDir()
		boom := errors.New("rename refused")
		r := New(dir)
		r.renameFn = lockAtState(t, dir, boom)

		err := r.WriteTransaction(context.Background(), addPlugin("alpha"))
		if !errors.Is(err, boom) {
			t.Fatalf("WriteTransaction err = %v, want the rename failure", err)
		}
		if !strings.Contains(err.Error(), "rollback remove") {
			t.Errorf("err = %v, want one naming the failed rollback remove", err)
		}
	})

	t.Run("stage", func(t *testing.T) {
		dir := t.TempDir()
		seed := New(dir)
		if err := seed.WriteTransaction(context.Background(), addPlugin("alpha")); err != nil {
			t.Fatalf("seed transaction: %v", err)
		}

		boom := errors.New("rename refused")
		r := New(dir)
		r.renameFn = lockAtState(t, dir, boom)

		err := r.WriteTransaction(context.Background(), addPlugin("beta"))
		if !errors.Is(err, boom) {
			t.Fatalf("WriteTransaction err = %v, want the rename failure", err)
		}
		if !strings.Contains(err.Error(), "rollback stage") {
			t.Errorf("err = %v, want one naming the failed rollback stage", err)
		}
	})
}

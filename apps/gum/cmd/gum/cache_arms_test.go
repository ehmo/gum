package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/cache"
)

// runCacheArms executes one `gum cache ...` invocation through the root
// command so the persistent --profile flag is in scope.
func runCacheArms(t *testing.T, out, errOut *bytes.Buffer, args ...string) error {
	t.Helper()
	root := newRootCmd()
	root.SetOut(out)
	root.SetErr(errOut)
	root.SetIn(strings.NewReader(""))
	root.SetArgs(append([]string{"cache"}, args...))
	return root.Execute()
}

// TestCacheMigrateSurfacesACorruptWAL pins the non-ambiguity error arm. A file
// that is not a database must fail the command; --force is the documented
// recovery and the plain run must not take it silently.
func TestCacheMigrateSurfacesACorruptWAL(t *testing.T) {
	root := withTempCacheRootCLI(t)
	dir := filepath.Join(root, "gum", "default")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "http-wal.db"), []byte("not sqlite"), 0o600); err != nil {
		t.Fatalf("write corrupt wal: %v", err)
	}

	var out, errOut bytes.Buffer
	err := runCacheArms(t, &out, &errOut, "migrate")
	if err == nil {
		t.Fatalf("want a corrupt-wal error, got nil; stdout=%q", out.String())
	}
	if strings.Contains(out.String(), "RSYNC_AMBIGUITY") {
		t.Errorf("corrupt wal reported as an rsync ambiguity:\n%s", out.String())
	}
}

// TestCacheMigrateForcePrintsItsWarning pins the warnings loop. Discarding a
// corrupt file is data loss, so the run has to say so on stderr while stdout
// stays a clean envelope.
func TestCacheMigrateForcePrintsItsWarning(t *testing.T) {
	root := withTempCacheRootCLI(t)
	dir := filepath.Join(root, "gum", "default")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "http-wal.db"), []byte("not sqlite"), 0o600); err != nil {
		t.Fatalf("write corrupt wal: %v", err)
	}

	var out, errOut bytes.Buffer
	if err := runCacheArms(t, &out, &errOut, "migrate", "--force"); err != nil {
		t.Fatalf("gum cache migrate --force: %v\nstdout=%s\nstderr=%s", err, out.String(), errOut.String())
	}
	if !strings.Contains(errOut.String(), "warning: discarded a corrupt http-wal.db") {
		t.Errorf("stderr missing the discard warning:\n%s", errOut.String())
	}
	if strings.Contains(out.String(), "warning:") {
		t.Errorf("warning leaked into the stdout envelope:\n%s", out.String())
	}
}

// TestCacheMigrateAmbiguityReportsAWriteFailure pins the arm inside the
// RSYNC_AMBIGUITY branch: if the report cannot be written, the write error is
// what the caller gets, not a bare ambiguity.
func TestCacheMigrateAmbiguityReportsAWriteFailure(t *testing.T) {
	root := withTempCacheRootCLI(t)
	dir := filepath.Join(root, "gum", "default")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	// A valid wal with no sentinel plus a leftover http.db is the mid-rsync
	// shape the ambiguity guard exists for.
	walPath := filepath.Join(dir, "http-wal.db")
	s, err := cache.OpenSQLiteWAL(cache.SQLiteConfig{Path: walPath})
	if err != nil {
		t.Fatalf("open wal: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close wal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "http.db"), []byte("bolt-ish"), 0o600); err != nil {
		t.Fatalf("write bolt: %v", err)
	}

	cmdRoot := newRootCmd()
	cmdRoot.SetOut(failWriter{})
	var errOut bytes.Buffer
	cmdRoot.SetErr(&errOut)
	cmdRoot.SetIn(strings.NewReader(""))
	cmdRoot.SetArgs([]string{"cache", "migrate"})
	if err := cmdRoot.Execute(); err == nil {
		t.Error("want the report write failure, got nil")
	}
}

// TestCacheClearBakSurfacesARemoveFailure pins the remove arm: a backup gum
// cannot delete must fail the command rather than report removed_bak true.
func TestCacheClearBakSurfacesARemoveFailure(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores the write bit")
	}
	root := withTempCacheRootCLI(t)
	dir := filepath.Join(root, "gum", "default")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "http.db.bak"), []byte("x"), 0o600); err != nil {
		t.Fatalf("write bak: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatalf("chmod %s: %v", dir, err)
	}

	var out, errOut bytes.Buffer
	if err := runCacheArms(t, &out, &errOut, "clear", "--bak"); err == nil {
		t.Fatalf("want the remove failure, got nil; stdout=%q", out.String())
	}
}

// TestCacheClearExpiredSurfacesAnOpenFailure pins the open arm: a cache.db
// that is not a database must fail rather than report zero evictions.
func TestCacheClearExpiredSurfacesAnOpenFailure(t *testing.T) {
	root := withTempCacheRootCLI(t)
	dir := filepath.Join(root, "gum", "default")
	// A directory at cache.db makes the open fail without any lock wait.
	if err := os.MkdirAll(filepath.Join(dir, "cache.db"), 0o755); err != nil {
		t.Fatalf("mkdir cache.db: %v", err)
	}

	var out, errOut bytes.Buffer
	if err := runCacheArms(t, &out, &errOut, "clear", "--expired"); err == nil {
		t.Fatalf("want the cache open failure, got nil; stdout=%q", out.String())
	}
}

// TestCacheProfileDirRejectsABadName pins the profile guard shared by clear
// and migrate: an unparseable name must not resolve to a directory.
func TestCacheProfileDirRejectsABadName(t *testing.T) {
	withTempCacheRootCLI(t)

	var out, errOut bytes.Buffer
	if err := runCacheArms(t, &out, &errOut, "clear", "--bak", "--profile", "bad/name"); err == nil {
		t.Fatalf("want an invalid-profile error, got nil; stdout=%q", out.String())
	}
}

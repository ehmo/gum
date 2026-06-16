package auth_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.uber.org/goleak"

	"github.com/ehmo/gum/internal/auth"
)

// TestOpenBucketUsesDefaultPathFromHome pins the cfg.Path=="" branch:
// callers (notably the dispatch boot path) rely on the bucket landing
// under $HOME/.cache/gum/token-bucket.db so cross-process replay finds
// the same file.
func TestOpenBucketUsesDefaultPathFromHome(t *testing.T) {
	defer goleak.VerifyNone(t)
	home := t.TempDir()
	t.Setenv("HOME", home)

	b, err := auth.OpenBucket(auth.BucketConfig{})
	if err != nil {
		t.Fatalf("OpenBucket: %v", err)
	}
	t.Cleanup(func() { _ = b.Close() })

	want := filepath.Join(home, ".cache", "gum", "token-bucket.db")
	if _, err := os.Stat(want); err != nil {
		t.Errorf("expected default bucket db at %q; stat err=%v", want, err)
	}
}

// TestOpenBucketMkdirAllErrorWraps pins the MkdirAll branch: planting
// a regular file at the would-be parent directory location forces
// MkdirAll to fail and OpenBucket must surface a wrapped error.
func TestOpenBucketMkdirAllErrorWraps(t *testing.T) {
	defer goleak.VerifyNone(t)
	dir := t.TempDir()
	blocker := filepath.Join(dir, "bucket-blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(blocker, "nested", "token-bucket.db")
	_, err := auth.OpenBucket(auth.BucketConfig{Path: dbPath})
	if err == nil {
		t.Fatalf("expected mkdir error; got nil")
	}
	if !strings.Contains(err.Error(), "create bucket dir") {
		t.Errorf("err=%v; want 'create bucket dir' wrap", err)
	}
}

// TestOpenBucketNoHomeWrapsHomeDirErr pins OpenBucket's
// `os.UserHomeDir err → "get home dir: %w"` arm
// (persistent_bucket.go:80-83). On Unix os.UserHomeDir returns
// "$HOME is not defined" when $HOME is empty; OpenBucket MUST
// surface that as a clean wrapped error rather than panicking on
// the empty default path (which would later mkdir the empty
// directory and only fail downstream).
func TestOpenBucketNoHomeWrapsHomeDirErr(t *testing.T) {
	defer goleak.VerifyNone(t)
	t.Setenv("HOME", "")
	// On Linux os.UserHomeDir consults user.Current as a fallback
	// when HOME is empty. To keep this deterministic across CI
	// platforms we additionally point USER to empty so user.Current
	// also fails on the platforms that use it.
	t.Setenv("USER", "")

	_, err := auth.OpenBucket(auth.BucketConfig{})
	if err == nil {
		t.Fatal("OpenBucket(no $HOME) err=nil; want 'get home dir' wrap")
	}
	if !strings.Contains(err.Error(), "get home dir") {
		t.Errorf("err=%v; want 'get home dir' wrap", err)
	}
}

// TestOpenBucketCorruptFileWraps pins the bolt.Open error branch: a
// non-bbolt file at the configured path produces a wrapped 'open bbolt'
// error rather than panicking inside the bolt driver.
func TestOpenBucketCorruptFileWraps(t *testing.T) {
	defer goleak.VerifyNone(t)
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "corrupt.db")
	if err := os.WriteFile(dbPath, []byte("not a bbolt file"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := auth.OpenBucket(auth.BucketConfig{Path: dbPath})
	if err == nil {
		t.Fatalf("expected open-bbolt error; got nil")
	}
	if !strings.Contains(err.Error(), "open bbolt") {
		t.Errorf("err=%v; want 'open bbolt' wrap", err)
	}
}

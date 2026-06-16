package tee_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/output/tee"
)

// TestLoadSecretUnreadablePathReturnsCorruptError pins loadSecret's
// `err != fs.ErrNotExist → &SecretCorruptError{Reason: "unreadable: ..."}`
// arm (secret.go:113). When the secret file exists but cannot be read
// (here: planted as a directory at tee.secret's path, so os.ReadFile
// returns EISDIR), LoadSecret MUST surface a typed SecretCorruptError
// — not the bare stdlib err and not fs.ErrNotExist — so the runtime
// can route it to the TEE_SECRET_CORRUPT envelope rather than the
// auto-generate path.
func TestLoadSecretUnreadablePathReturnsCorruptError(t *testing.T) {
	dir := t.TempDir()
	// Plant a directory exactly where tee.secret is expected — os.ReadFile
	// returns EISDIR, neither a "not exist" nor a clean read.
	if err := os.Mkdir(filepath.Join(dir, "tee.secret"), 0o700); err != nil {
		t.Fatalf("plant blocker dir: %v", err)
	}

	_, err := tee.LoadSecret(dir)
	if err == nil {
		t.Fatal("LoadSecret(dir-at-secret-path)=nil err; want SecretCorruptError")
	}
	var sce *tee.SecretCorruptError
	if !errors.As(err, &sce) {
		t.Fatalf("err type=%T %v; want *SecretCorruptError", err, err)
	}
	if !strings.Contains(sce.Reason, "unreadable:") {
		t.Errorf("Reason=%q; want 'unreadable:' prefix", sce.Reason)
	}
}

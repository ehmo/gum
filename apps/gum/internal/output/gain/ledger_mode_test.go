package gain_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ehmo/gum/internal/output/gain"
)

// Spec §12.3 stores the ledger at mode 600. The entries carry args hashes and
// the auth-subject fingerprint, which are profile-local identity material, so
// a world-readable ledger leaks the shape of a user's traffic to any local
// account. The §11 audit log already uses 0o700 dirs and 0o600 files.
func TestLedgerFileIsMode600(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested")
	path := filepath.Join(dir, gain.LedgerFileName)

	l, err := gain.NewLedger(path)
	if err != nil {
		t.Fatalf("NewLedger: %v", err)
	}
	t.Cleanup(func() { _ = l.Close() })

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat ledger: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("ledger mode=%04o want 0600", got)
	}

	dirInfo, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat ledger dir: %v", err)
	}
	if got := dirInfo.Mode().Perm(); got != 0o700 {
		t.Errorf("ledger dir mode=%04o want 0700", got)
	}
}

// A ledger created by an older gum (mode 644) must be tightened on open, not
// left readable for the rest of its life.
func TestExistingLedgerIsTightenedOnOpen(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, gain.LedgerFileName)
	if err := os.WriteFile(path, []byte(""), 0o644); err != nil {
		t.Fatalf("seed ledger: %v", err)
	}

	l, err := gain.NewLedger(path)
	if err != nil {
		t.Fatalf("NewLedger: %v", err)
	}
	t.Cleanup(func() { _ = l.Close() })

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat ledger: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("pre-existing ledger left at mode=%04o want 0600", got)
	}
}

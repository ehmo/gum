package cache

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCopyBoltToSQLiteBoltOpenFailureSurfacesWrap pins the
// `bolt.Open err → return wrapped err` arm. Migrate (the public
// caller) deletes the wal file on this error so the next run lands
// on branch-2 recovery; that recovery cycle MUST start from a
// clearly-labelled wrap ("cache: open bolt for migration:") so
// operators can tell a corrupt bbolt file from a missing one
// (separate errno paths) in the migration audit log.
func TestCopyBoltToSQLiteBoltOpenFailureSurfacesWrap(t *testing.T) {
	tmp := t.TempDir()
	// Plant a directory at the bolt path → bolt.Open returns EISDIR.
	boltPath := filepath.Join(tmp, "cache.bolt")
	if err := os.Mkdir(boltPath, 0o755); err != nil {
		t.Fatalf("plant bolt path as dir: %v", err)
	}
	walPath := filepath.Join(tmp, "cache.wal")

	_, _, _, err := copyBoltToSQLite(boltPath, walPath)
	if err == nil {
		t.Fatal("copyBoltToSQLite(bolt=dir)=nil err; want bolt.Open wrap")
	}
	if !strings.Contains(err.Error(), "cache: open bolt for migration") {
		t.Errorf("err=%q; want 'cache: open bolt for migration:' wrap", err)
	}
}

// TestCopyBoltToSQLiteSQLiteOpenFailureSurfacesError pins the
// `OpenSQLiteWAL err → return err` arm. Reached when the bolt file
// is fine but the wal destination is unwritable (e.g. a directory
// already exists at that path, or the parent dir is read-only). The
// caller (Migrate) sees this distinct from a copy/Set error and the
// audit log preserves the source failure point.
func TestCopyBoltToSQLiteSQLiteOpenFailureSurfacesError(t *testing.T) {
	tmp := t.TempDir()
	// Plant a real, openable bolt DB (reuse the same seed helper the
	// migrate_test.go suite uses — same package, no export needed).
	boltPath := filepath.Join(tmp, "cache.bolt")
	seedBolt(t, boltPath, map[string]map[string]string{"b": {"k": "v"}}, nil)
	// Plant a directory at the wal path → OpenSQLiteWAL fails when it
	// tries to open the file.
	walPath := filepath.Join(tmp, "cache.wal")
	if err := os.Mkdir(walPath, 0o755); err != nil {
		t.Fatalf("plant wal path as dir: %v", err)
	}

	_, _, _, err := copyBoltToSQLite(boltPath, walPath)
	if err == nil {
		t.Fatal("copyBoltToSQLite(wal=dir)=nil err; want OpenSQLiteWAL surface")
	}
}

package cache

import (
	"path/filepath"
	"strings"
	"testing"

	bolt "go.etcd.io/bbolt"
)

// TestMigrateDeeplyNestedSubBucketIncrementsCounter pins copyBoltToSQLite's
// `sv == nil → subBuckets++; return nil` arm (migrate.go:203-206). When a
// sub-bucket contains its OWN sub-bucket (depth ≥ 3 from the tx root: tx →
// top → mid → leaf-bucket), level-1 iteration encounters a nil-value key
// representing the further-nested bucket; copyBoltToSQLite intentionally
// does NOT recurse a second time but MUST still count the bucket so the
// warning surfaced by Migrate accurately reflects depth — operators
// reading the warning will go looking for those keys and need to know
// they were skipped rather than silently lost.
//
// We seed top/mid/leaf with leaf itself being a sub-bucket of mid. The
// migrated subBuckets total ends up at 2 (mid as a level-1 bucket inside
// top, leaf as a level-2 bucket inside mid).
func TestMigrateDeeplyNestedSubBucketIncrementsCounter(t *testing.T) {
	dir := t.TempDir()
	boltPath := filepath.Join(dir, HTTPCacheBoltFile)

	db, err := bolt.Open(boltPath, 0o600, nil)
	if err != nil {
		t.Fatalf("bolt.Open: %v", err)
	}
	if err := db.Update(func(tx *bolt.Tx) error {
		top, err := tx.CreateBucket([]byte("top"))
		if err != nil {
			return err
		}
		mid, err := top.CreateBucket([]byte("mid"))
		if err != nil {
			return err
		}
		// Leaf is a bucket inside mid → mid.ForEach yields (leaf, nil).
		_, err = mid.CreateBucket([]byte("leaf"))
		return err
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close seed db: %v", err)
	}

	res, err := Migrate(MigrateOptions{CacheDir: dir})
	if err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	// top.ForEach sees one nil-value entry (mid) → subBuckets=1.
	// mid.ForEach sees one nil-value entry (leaf) → subBuckets=2.
	if res.SubBucketsFound < 2 {
		t.Errorf("SubBucketsFound=%d; want >=2 (mid + leaf)", res.SubBucketsFound)
	}
	if len(res.Warnings) == 0 {
		t.Fatal("Warnings empty; want sub-bucket warning")
	}
	// The warning message embeds the count; sanity check that.
	joined := strings.Join(res.Warnings, "|")
	if !strings.Contains(joined, "sub-buckets") {
		t.Errorf("Warnings=%q; want substring 'sub-buckets'", joined)
	}
}

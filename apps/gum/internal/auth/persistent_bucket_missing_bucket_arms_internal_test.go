package auth

import (
	"errors"
	"math"
	"path/filepath"
	"strings"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"
	"go.uber.org/goleak"
)

// dropTokenBucket deletes the bbolt bucket OpenBucket created, leaving the
// handle open. Every read and write path has a "bucket missing" arm for the
// case where another writer on the shared db file removed it; this is the
// only way to reach those arms without a second process.
func dropTokenBucket(t *testing.T, b *PersistentBucket) {
	t.Helper()
	if err := b.db.Update(func(tx *bolt.Tx) error { return tx.DeleteBucket(tokenBucket) }); err != nil {
		t.Fatalf("DeleteBucket: %v", err)
	}
}

func openTestBucket(t *testing.T) *PersistentBucket {
	t.Helper()
	b, err := OpenBucket(BucketConfig{
		Path: filepath.Join(t.TempDir(), "bucket.db"),
		Now:  func() time.Time { return time.Unix(1700000000, 0) },
	})
	if err != nil {
		t.Fatalf("OpenBucket: %v", err)
	}
	t.Cleanup(func() { _ = b.Close() })
	return b
}

// TestOpenBucketClosesHandleWhenBucketCreationFails covers the create-bucket
// failure arm. bbolt rejects an empty bucket name, which stands in for any
// Update failure (a full disk, a revoked write) that OpenBucket must answer
// by closing the handle it just opened instead of leaking it.
func TestOpenBucketClosesHandleWhenBucketCreationFails(t *testing.T) {
	defer goleak.VerifyNone(t)
	saved := tokenBucket
	tokenBucket = []byte("")
	t.Cleanup(func() { tokenBucket = saved })

	b, err := OpenBucket(BucketConfig{Path: filepath.Join(t.TempDir(), "bucket.db")})
	if err == nil {
		_ = b.Close()
		t.Fatal("OpenBucket = nil err; want the bucket creation failure surfaced")
	}
	if !strings.Contains(err.Error(), "create token bucket") {
		t.Errorf("err = %v; want it to name the bucket creation step", err)
	}
}

func TestTakeSurfacesPersistFailureWhenBucketVanishes(t *testing.T) {
	defer goleak.VerifyNone(t)
	b := openTestBucket(t)
	dropTokenBucket(t, b)

	err := b.Take("op.a", "creds-1", 1)
	if err == nil || errors.Is(err, ErrRateLimited) {
		t.Fatalf("Take = %v; want the persist failure, not nil and not ErrRateLimited", err)
	}
	if !strings.Contains(err.Error(), "persist bucket state") {
		t.Errorf("err = %v; want it to name the persist step", err)
	}
}

// TestUpdateToleratesMissingBucket pins the other half of the contract:
// Update reports nothing, so a vanished bucket must not panic it.
func TestUpdateToleratesMissingBucket(t *testing.T) {
	defer goleak.VerifyNone(t)
	b := openTestBucket(t)
	dropTokenBucket(t, b)

	b.Update("op.a", "creds-1", time.Second)
}

func TestPersistStateRejectsUnserialisableTokens(t *testing.T) {
	defer goleak.VerifyNone(t)
	b := openTestBucket(t)

	err := b.persistState("op.a|creds-1", bucketState{Tokens: math.NaN(), Capacity: 10})
	if err == nil || !strings.Contains(err.Error(), "marshal bucket state") {
		t.Fatalf("persistState(NaN tokens) = %v; want the marshal failure named", err)
	}
}

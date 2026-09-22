package cache

import (
	"strings"
	"testing"

	bolt "go.etcd.io/bbolt"
)

// A row whose record does not decode reaches pred with a nil payload, so a
// pred that accepts nil reaps it and a pred that inspects the payload leaves
// it alone. Without this the only way to drop a corrupt row would be to
// delete the whole cache file.
func TestDeleteWhereSeesCorruptRowsAsNil(t *testing.T) {
	c := openTestCache(t)
	if err := c.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(cacheBucket).Put([]byte("corrupt"), []byte("not a record"))
	}); err != nil {
		t.Fatalf("seed corrupt row: %v", err)
	}
	if err := c.Set("good", []byte("payload"), 0); err != nil {
		t.Fatalf("set: %v", err)
	}

	kept, err := c.DeleteWhere(func(_ string, payload []byte) bool { return payload != nil })
	if err != nil {
		t.Fatalf("DeleteWhere: %v", err)
	}
	if kept != 1 {
		t.Fatalf("deleted %d; want only the decodable row", kept)
	}

	reaped, err := c.DeleteWhere(func(_ string, payload []byte) bool { return payload == nil })
	if err != nil {
		t.Fatalf("DeleteWhere corrupt: %v", err)
	}
	if reaped != 1 {
		t.Errorf("deleted %d corrupt rows; want 1", reaped)
	}
}

// A deleted key must leave the hot tier too, or a later Get answers from
// memory with an entry the file no longer holds.
func TestDeleteWhereDropsTheHotEntry(t *testing.T) {
	c := openTestCache(t)
	if err := c.Set("k", []byte("v"), 0); err != nil {
		t.Fatalf("set: %v", err)
	}
	if _, ok := c.Get("k"); !ok {
		t.Fatal("seed did not land")
	}
	if n, err := c.DeleteWhere(func(string, []byte) bool { return true }); err != nil || n != 1 {
		t.Fatalf("DeleteWhere = %d, %v; want 1, nil", n, err)
	}
	if _, ok := c.Get("k"); ok {
		t.Error("a deleted key still answers from the hot tier")
	}
}

// A cache file whose bucket is gone reads as empty rather than panicking, and
// the delete half reports the missing bucket rather than claiming a deletion.
func TestDeleteWhereAndForEachOnAMissingBucket(t *testing.T) {
	c := openTestCache(t)
	if err := c.Set("k", []byte("v"), 0); err != nil {
		t.Fatalf("set: %v", err)
	}
	dropBucket(t, c)

	if err := c.ForEachPayload(func(string, []byte) {
		t.Error("walked an entry in a cache with no bucket")
	}); err != nil {
		t.Errorf("ForEachPayload = %v; want nil", err)
	}
	if n, err := c.DeleteWhere(func(string, []byte) bool { return true }); err != nil || n != 0 {
		t.Errorf("DeleteWhere = %d, %v; want 0, nil", n, err)
	}
}

// A closed database fails both halves with a wrapped error instead of a
// partial count the caller would print as a result.
func TestDeleteWhereAndForEachOnAClosedDB(t *testing.T) {
	c := openTestCache(t)
	if err := c.Set("k", []byte("v"), 0); err != nil {
		t.Fatalf("set: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	n, err := c.DeleteWhere(func(string, []byte) bool { return true })
	if err == nil {
		t.Fatal("DeleteWhere on a closed db must fail")
	}
	if n != 0 {
		t.Errorf("deleted = %d; want 0 on a failed scan", n)
	}
	if !strings.Contains(err.Error(), "cache:") {
		t.Errorf("err = %v; want a cache-prefixed error", err)
	}
	if err := c.ForEachPayload(func(string, []byte) {}); err == nil {
		t.Error("ForEachPayload on a closed db must fail")
	}
}

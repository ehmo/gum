package plugins_test

import (
	"testing"

	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/plugins"
)

// TestLookupNamespaceOwnerNilLockReturnsFalse pins
// LookupNamespaceOwner's `lock == nil → return "", false` guard
// (namespace.go:22-24). Callers MUST be able to pass a nil lock
// without panicking — the install pipeline relies on this when no
// per-profile lock has been written yet.
func TestLookupNamespaceOwnerNilLockReturnsFalse(t *testing.T) {
	t.Parallel()
	owner, found := plugins.LookupNamespaceOwner(nil, "fli")
	if found {
		t.Errorf("LookupNamespaceOwner(nil, ...) found=true; want false")
	}
	if owner != "" {
		t.Errorf("owner=%q; want \"\" on nil lock", owner)
	}
}

// TestLookupNamespaceOwnerSkipsNonMapAndPrefixMismatch pins the loop's
// two `continue` arms (namespace.go:27-28 non-map skip, 31-32 prefix
// mismatch skip). The lock's Plugins slice is []any so any non-map
// element MUST be tolerated, and rows with a different prefix MUST
// fall through to the next row instead of returning a spurious match.
func TestLookupNamespaceOwnerSkipsNonMapAndPrefixMismatch(t *testing.T) {
	t.Parallel()
	lock := &catalog.PluginsLock{
		PluginsLockSchemaVersion: 1,
		Plugins: []any{
			"not-a-map", // namespace.go:27-28 non-map skip
			map[string]any{"prefix": "other", "namespace_owner": "wrong"}, // 31-32 prefix mismatch skip
			map[string]any{"prefix": "fli", "namespace_owner": "io.example.flights"},
		},
	}
	owner, found := plugins.LookupNamespaceOwner(lock, "fli")
	if !found {
		t.Fatalf("LookupNamespaceOwner(fli) found=false; want true after skipping non-map + mismatch")
	}
	if owner != "io.example.flights" {
		t.Errorf("owner=%q; want io.example.flights", owner)
	}
}

// TestRecordNamespaceOwnerNilLockNoPanic pins RecordNamespaceOwner's
// `lock == nil → return` guard (namespace.go:45-47). The function MUST
// be safe to call with a nil lock; the caller will retry once they've
// constructed a lock.
func TestRecordNamespaceOwnerNilLockNoPanic(t *testing.T) {
	t.Parallel()
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("RecordNamespaceOwner(nil, ...) panicked: %v", r)
		}
	}()
	plugins.RecordNamespaceOwner(nil, "fli", "io.example.flights")
}

// TestRecordNamespaceOwnerSkipsNonMapRow pins the non-map skip
// (namespace.go:50-51). A non-map element in lock.Plugins MUST NOT
// stop the search; the function should continue scanning and then
// append a new row when no matching prefix exists.
func TestRecordNamespaceOwnerSkipsNonMapRow(t *testing.T) {
	t.Parallel()
	lock := &catalog.PluginsLock{
		PluginsLockSchemaVersion: 1,
		Plugins: []any{
			42, // non-map element — must be skipped, not crashed on
		},
	}
	plugins.RecordNamespaceOwner(lock, "fli", "io.example.flights")
	if len(lock.Plugins) != 2 {
		t.Fatalf("expected len=2 (non-map preserved + new row appended); got %d", len(lock.Plugins))
	}
	owner, found := plugins.LookupNamespaceOwner(lock, "fli")
	if !found || owner != "io.example.flights" {
		t.Errorf("LookupNamespaceOwner after Record: owner=%q found=%v", owner, found)
	}
}

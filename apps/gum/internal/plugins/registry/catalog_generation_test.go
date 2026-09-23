package registry

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// Bead gum-t3tl. Spec §8.7 (step 3) writes the same
// install_generation and install_txid into all three temp files, and step 4
// says "a generation is authoritative only when all three final files exist
// and carry the same install_generation and install_txid".
//
// gum 2.0.x modelled plugin-catalog.json with only schema version, updated_at
// and variants, so SelectGeneration compared lock against state and took the
// catalog on presence alone. A publish that renamed plugin-catalog.json and
// then failed before renaming plugins.lock left the catalog holding
// generation N variants while the lock and state held N-1, and the host
// dispatched from a catalog the lock does not back.

// readCatalogFields returns the raw top-level JSON of plugin-catalog.json, so
// an assertion can see a key the Go struct might silently drop.
func readCatalogFields(t *testing.T, dir string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(CatalogPath(dir))
	if err != nil {
		t.Fatalf("read catalog: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode catalog: %v", err)
	}
	return out
}

// writeStampedTrio seeds the three files, stamping the catalog only when
// catalogGen is non-zero. A zero catalogGen writes the pre-gum-t3tl shape.
func writeStampedTrio(t *testing.T, dir string, catalogGen int, catalogTxID string, lockGen int, lockTxID string) {
	t.Helper()
	cat := map[string]any{
		"plugin_catalog_schema_version": 1,
		"updated_at":                    "2026-09-19T00:00:00Z",
		"variants":                      []any{},
	}
	if catalogGen != 0 {
		cat["install_generation"] = catalogGen
		cat["install_txid"] = catalogTxID
	}
	writeJSON(t, filepath.Join(dir, CatalogFilename), cat)
	writeJSON(t, filepath.Join(dir, LockFilename), map[string]any{
		"plugins_lock_schema_version": 1,
		"install_generation":          lockGen,
		"install_txid":                lockTxID,
		"plugins":                     []any{},
	})
	writeJSON(t, filepath.Join(dir, StateFilename), map[string]any{
		"plugin_state_schema_version": 1,
		"install_generation":          lockGen,
		"install_txid":                lockTxID,
		"plugins":                     []any{},
	})
}

// TestCatalogOnlyTearIsDetected is the bead's repro: the catalog alone
// advanced to the new generation. All three files exist and the lock agrees
// with the state, so the old two-file check reported Ok=true.
func TestCatalogOnlyTearIsDetected(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeStampedTrio(t, dir, 2, "newtxid0", 1, "oldtxid0")

	gen, err := New(dir).SelectGeneration()
	if err != nil {
		t.Fatalf("SelectGeneration: %v", err)
	}
	if gen.Ok {
		t.Errorf("Ok = true for a catalog-only tear (catalog gen 2, lock/state gen 1); want false so the host refuses dispatch")
	}
}

// TestCatalogTxIDTearIsDetected covers the generation-matches-txid-does-not
// case: a retry that reused the generation number under a fresh txid.
func TestCatalogTxIDTearIsDetected(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeStampedTrio(t, dir, 3, "txid-new", 3, "txid-old")

	gen, err := New(dir).SelectGeneration()
	if err != nil {
		t.Fatalf("SelectGeneration: %v", err)
	}
	if gen.Ok {
		t.Errorf("Ok = true when the catalog txid differs from lock/state; want false")
	}
}

// TestSelectGenerationAcceptsMatchingCatalogStamp keeps the happy path honest:
// a stamp that agrees must not be read as a tear.
func TestSelectGenerationAcceptsMatchingCatalogStamp(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeStampedTrio(t, dir, 5, "same-tx", 5, "same-tx")

	gen, err := New(dir).SelectGeneration()
	if err != nil {
		t.Fatalf("SelectGeneration: %v", err)
	}
	if !gen.Ok || gen.Generation != 5 || gen.TxID != "same-tx" {
		t.Errorf("gen = %+v; want Ok=true Generation=5 TxID=same-tx", gen)
	}
}

// TestUnstampedCatalogIsAcceptedOnce covers the migration case: a profile
// written before gum-t3tl has no stamp on the catalog at all. Refusing it
// would brick the profile permanently, because the startup activation write
// that would re-stamp the files only runs after a complete generation is
// selected. An unstamped catalog is therefore accepted and adopts the
// lock/state generation; the next transaction stamps it for good.
func TestUnstampedCatalogIsAcceptedOnce(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeStampedTrio(t, dir, 0, "", 7, "legacytx")

	gen, err := New(dir).SelectGeneration()
	if err != nil {
		t.Fatalf("SelectGeneration: %v", err)
	}
	if !gen.Ok || gen.Generation != 7 || gen.TxID != "legacytx" {
		t.Errorf("gen = %+v; want Ok=true Generation=7 TxID=legacytx for a pre-stamp catalog", gen)
	}
}

// TestWriteTransactionStampsTheCatalog asserts step 3 on the file the bead
// found unstamped: after any transaction all three files carry the same pair.
func TestWriteTransactionStampsTheCatalog(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	reg := New(dir)

	if err := reg.WriteTransaction(context.Background(), func(f *Files) error {
		f.Catalog.Variants = append(f.Catalog.Variants, map[string]any{"variant_id": "plug.x.v1"})
		return nil
	}); err != nil {
		t.Fatalf("WriteTransaction: %v", err)
	}

	fields := readCatalogFields(t, dir)
	if _, ok := fields["install_generation"]; !ok {
		t.Fatalf("plugin-catalog.json has no install_generation key; got %v", fields)
	}
	if _, ok := fields["install_txid"]; !ok {
		t.Fatalf("plugin-catalog.json has no install_txid key; got %v", fields)
	}

	files, err := reg.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if files.Catalog.InstallGeneration != files.Lock.InstallGeneration ||
		files.Catalog.InstallTxID != files.Lock.InstallTxID {
		t.Errorf("catalog (%d,%q) != lock (%d,%q)",
			files.Catalog.InstallGeneration, files.Catalog.InstallTxID,
			files.Lock.InstallGeneration, files.Lock.InstallTxID)
	}
}

// TestUnstampedCatalogIsRestampedOnNextWrite closes the migration: the
// exemption applies to one legacy file, not forever.
func TestUnstampedCatalogIsRestampedOnNextWrite(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeStampedTrio(t, dir, 0, "", 7, "legacytx")

	reg := New(dir)
	if err := reg.WriteTransaction(context.Background(), func(*Files) error { return nil }); err != nil {
		t.Fatalf("WriteTransaction: %v", err)
	}

	files, err := reg.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if files.Catalog.InstallGeneration != 8 {
		t.Errorf("catalog generation = %d; want 8 (7 + 1)", files.Catalog.InstallGeneration)
	}
	gen, err := reg.SelectGeneration()
	if err != nil {
		t.Fatalf("SelectGeneration: %v", err)
	}
	if !gen.Ok || gen.Generation != 8 {
		t.Errorf("gen = %+v; want Ok=true Generation=8", gen)
	}
}

// TestGenerationCountsTheCatalogWhenAdvancing keeps install_generation
// monotonic across all three files. After a catalog-only tear the catalog
// holds the highest number; the repair transaction must advance past it
// rather than reuse it.
func TestGenerationCountsTheCatalogWhenAdvancing(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeStampedTrio(t, dir, 9, "torn-tx", 4, "old-tx")

	reg := New(dir)
	if err := reg.WriteTransaction(context.Background(), func(*Files) error { return nil }); err != nil {
		t.Fatalf("WriteTransaction: %v", err)
	}
	files, err := reg.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if files.Lock.InstallGeneration != 10 {
		t.Errorf("generation = %d; want 10 (max(9,4) + 1)", files.Lock.InstallGeneration)
	}
}

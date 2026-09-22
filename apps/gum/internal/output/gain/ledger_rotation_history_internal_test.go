package gain

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestGainStatsIncludesRotatedSegments pins the rotation-history contract:
// once Append rotates the live file, the archived segment still counts
// toward `gum gain`. Reading only the live path made every rotation drop
// the whole window it archived, which silently understates the savings
// figure spec §12.3 calls "the only evidence used for release claims".
//
// It also pins rotated-name uniqueness. Two rotations inside one wall-clock
// second produced the same <base>-<unix><ext> name, and os.Rename overwrote
// the earlier segment, so the entries were gone from disk as well.
func TestGainStatsIncludesRotatedSegments(t *testing.T) {
	prev := maxLedgerSize
	t.Cleanup(func() { maxLedgerSize = prev })
	maxLedgerSize = 1 // every append crosses the threshold and rotates

	dir := t.TempDir()
	path := filepath.Join(dir, LedgerFileName)

	l, err := NewLedger(path)
	if err != nil {
		t.Fatalf("NewLedger: %v", err)
	}

	variant := "v1"
	outProfile := "p1"
	sessions := []string{"aaaaaaaa", "bbbbbbbb", "cccccccc"}
	for _, s := range sessions {
		e := Entry{
			Session: s, OpID: "gmail.users.messages.list",
			VariantID: &variant, OutputProfile: &outProfile,
			ArgsHash: "h", AuthSubjectFingerprint: "f",
			RawTokens: 100, ShapedTokens: 10,
			CacheStatus: "miss", FieldMaskStatus: "applied",
			OpFamily: "gmail.users.messages", BaselineMethod: "fixture_replay",
		}
		if err := l.Append(e); err != nil {
			t.Fatalf("Append(%s): %v", s, err)
		}
	}
	if err := l.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Every rotated segment must survive on disk under its own name.
	names, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	var rotated int
	for _, n := range names {
		if strings.HasPrefix(n.Name(), "gain-ledger-") && strings.HasSuffix(n.Name(), ".jsonl") {
			rotated++
		}
	}
	if rotated != len(sessions) {
		t.Errorf("rotated segments on disk = %d; want %d (a same-second rename overwrote one)",
			rotated, len(sessions))
	}

	// A fresh reader must aggregate the archived segments with the live one.
	r, err := NewLedger(path)
	if err != nil {
		t.Fatalf("NewLedger (reader): %v", err)
	}
	t.Cleanup(func() { _ = r.Close() })

	got := r.Stats()
	if got.TotalCalls != int64(len(sessions)) {
		t.Fatalf("Stats().TotalCalls = %d; want %d (rotated segments were not read back)",
			got.TotalCalls, len(sessions))
	}
	if got.TotalTokensIn != int64(100*len(sessions)) {
		t.Errorf("Stats().TotalTokensIn = %d; want %d", got.TotalTokensIn, 100*len(sessions))
	}
	if got.TotalTokensSaved != int64(90*len(sessions)) {
		t.Errorf("Stats().TotalTokensSaved = %d; want %d", got.TotalTokensSaved, 90*len(sessions))
	}
}

// TestRotatedSegmentPathsIgnoresNonArchiveNeighbours pins the read path to
// the names this package writes. A neighbour whose suffix is not an integer
// is someone else's file, and counting it would change the savings figure.
func TestRotatedSegmentPathsIgnoresNonArchiveNeighbours(t *testing.T) {
	dir := t.TempDir()
	live := filepath.Join(dir, LedgerFileName)
	for _, name := range []string{
		"gain-ledger-backup.jsonl",
		"gain-ledger-2026-01-02.jsonl",
		"gain-ledger-200.jsonl",
		"gain-ledger-100.jsonl",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	got := rotatedSegmentPaths(live)
	want := []string{
		filepath.Join(dir, "gain-ledger-100.jsonl"),
		filepath.Join(dir, "gain-ledger-200.jsonl"),
	}
	if len(got) != len(want) {
		t.Fatalf("rotatedSegmentPaths = %v; want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("segment[%d] = %q; want %q", i, got[i], want[i])
		}
	}
}

// TestFreeRotatedPathExhaustsItsProbeWindow proves rotation reports a real
// error instead of overwriting an archive when every candidate name is taken.
func TestFreeRotatedPathExhaustsItsProbeWindow(t *testing.T) {
	dir := t.TempDir()
	live := filepath.Join(dir, LedgerFileName)
	base, ext := rotationBase(live)

	const ts int64 = 1767225600
	for i := 0; i < maxRotationNameProbes; i++ {
		name := fmt.Sprintf("%s-%d%s", base, ts+int64(i), ext)
		if err := os.WriteFile(name, nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := freeRotatedPath(live, ts); err == nil {
		t.Fatal("freeRotatedPath returned no error with every candidate name taken")
	}
}

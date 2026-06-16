package gain

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestAppendAutoRotatesOnSizeOverflow shrinks maxLedgerSize so a single
// Append crosses the threshold and triggers the rotation branch.
func TestAppendAutoRotatesOnSizeOverflow(t *testing.T) {
	prev := maxLedgerSize
	t.Cleanup(func() { maxLedgerSize = prev })
	maxLedgerSize = 1 // any non-empty append crosses the threshold

	dir := t.TempDir()
	path := filepath.Join(dir, "gain-ledger.jsonl")
	l, err := NewLedger(path)
	if err != nil {
		t.Fatalf("NewLedger: %v", err)
	}
	t.Cleanup(func() { _ = l.Close() })

	variant := "v1"
	profile := "p1"
	e := Entry{
		Session: "cccccccc", OpID: "x.y.z",
		VariantID: &variant, OutputProfile: &profile,
		ArgsHash: "h", AuthSubjectFingerprint: "f",
		RawTokens: 10, ShapedTokens: 1,
		CacheStatus: "miss", FieldMaskStatus: "applied",
		OpFamily: "x.y", BaselineMethod: "fixture_replay",
	}
	if err := l.Append(e); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if err := l.Append(e); err != nil {
		t.Fatalf("second Append (should trigger rotation): %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	var rotatedCount int
	for _, ent := range entries {
		if strings.HasPrefix(ent.Name(), "gain-ledger-") && strings.HasSuffix(ent.Name(), ".jsonl") {
			rotatedCount++
		}
	}
	if rotatedCount < 1 {
		t.Errorf("expected at least one rotated segment after size overflow; got entries=%v", entries)
	}
}

// TestOpFamilyEdgeCases covers the no-dot and dot-at-position-zero
// branches of opFamilyOf / lastDot.
func TestOpFamilyEdgeCases(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{in: "", want: ""},
		{in: "nodot", want: "nodot"},
		{in: ".leading", want: ".leading"},
		{in: "gmail.users.messages.list", want: "gmail.users.messages"},
		{in: "a.b", want: "a"},
	}
	for _, c := range cases {
		if got := opFamilyOf(c.in); got != c.want {
			t.Errorf("opFamilyOf(%q) = %q; want %q", c.in, got, c.want)
		}
	}
}

// TestRotateLockedCreatesNewSegmentWithHeader verifies that rotateLocked
// renames the active file to <base>-<unix>.jsonl, opens a fresh segment,
// and writes a new header so the new segment is spec §12.3 compliant
// from byte zero.
func TestRotateLockedCreatesNewSegmentWithHeader(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gain-ledger.jsonl")

	l, err := NewLedger(path)
	if err != nil {
		t.Fatalf("NewLedger: %v", err)
	}
	t.Cleanup(func() { _ = l.Close() })

	variant := "v1"
	profile := "p1"
	pre := Entry{
		Session: "aaaaaaaa", OpID: "gmail.users.messages.list", VariantID: &variant,
		OutputProfile: &profile, ArgsHash: "x", AuthSubjectFingerprint: "f",
		RequestTokens: 1, ResponseTokens: 2, RawTokens: 2, ShapedTokens: 1,
		CacheStatus: "miss", FieldMaskStatus: "applied",
		OpFamily: "gmail.users.messages", BaselineMethod: "fixture_replay",
	}
	if err := l.Append(pre); err != nil {
		t.Fatalf("pre-rotation Append: %v", err)
	}

	// Manually invoke rotation under the lock (matches rotateLocked's contract).
	l.mu.Lock()
	if err := l.rotateLocked(); err != nil {
		l.mu.Unlock()
		t.Fatalf("rotateLocked: %v", err)
	}
	l.mu.Unlock()

	post := pre
	post.Session = "bbbbbbbb"
	if err := l.Append(post); err != nil {
		t.Fatalf("post-rotation Append: %v", err)
	}

	// The rotated segment must exist next to the new file.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	var rotated, active string
	for _, e := range entries {
		switch {
		case e.Name() == "gain-ledger.jsonl":
			active = filepath.Join(dir, e.Name())
		case strings.HasPrefix(e.Name(), "gain-ledger-") && strings.HasSuffix(e.Name(), ".jsonl"):
			rotated = filepath.Join(dir, e.Name())
		}
	}
	if rotated == "" {
		t.Fatalf("rotated segment not found; entries=%v", entries)
	}
	if active == "" {
		t.Fatalf("active segment not found; entries=%v", entries)
	}

	// New segment must start with the canonical header.
	newData, err := os.ReadFile(active)
	if err != nil {
		t.Fatalf("read new segment: %v", err)
	}
	const wantHeader = `{"record_type":"header","schema_version":1,"tokenizer":"cl100k_base"}`
	if !strings.HasPrefix(string(newData), wantHeader) {
		t.Errorf("new segment does not start with canonical header\ngot prefix: %.100q",
			string(newData))
	}

	// Rotated segment must contain the pre-rotation entry.
	oldData, err := os.ReadFile(rotated)
	if err != nil {
		t.Fatalf("read rotated segment: %v", err)
	}
	if !strings.Contains(string(oldData), `"session":"aaaaaaaa"`) {
		t.Errorf("rotated segment missing pre-rotation entry\ngot: %s", oldData)
	}
	if strings.Contains(string(oldData), `"session":"bbbbbbbb"`) {
		t.Errorf("rotated segment unexpectedly contains post-rotation entry")
	}
	// Post-rotation entry lives in the new segment.
	if !strings.Contains(string(newData), `"session":"bbbbbbbb"`) {
		t.Errorf("new segment missing post-rotation entry\ngot: %s", newData)
	}
}

// TestEntryInWindow exercises the legacy-passthrough + malformed-passthrough
// branches that the public StatsBetween path cannot exercise (Append always
// stamps a timestamp on blank Entry.Timestamp).
func TestEntryInWindow(t *testing.T) {
	since := time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC)
	until := time.Date(2026, 1, 7, 0, 0, 0, 0, time.UTC)

	cases := []struct {
		name string
		e    Entry
		want bool
	}{
		{name: "blank_timestamp_passes_through", e: Entry{Timestamp: ""}, want: true},
		{name: "malformed_timestamp_passes_through", e: Entry{Timestamp: "not-a-date"}, want: true},
		{name: "in_window_rfc3339nano", e: Entry{Timestamp: "2026-01-05T00:00:00.000Z"}, want: true},
		{name: "in_window_rfc3339_fallback", e: Entry{Timestamp: "2026-01-05T00:00:00Z"}, want: true},
		{name: "before_window", e: Entry{Timestamp: "2026-01-01T00:00:00Z"}, want: false},
		{name: "after_window", e: Entry{Timestamp: "2026-01-10T00:00:00Z"}, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := entryInWindow(tc.e, since, until)
			if got != tc.want {
				t.Errorf("entryInWindow(%+v) = %v, want %v", tc.e, got, tc.want)
			}
		})
	}
}

// TestComputeStatsPercentileNearestRank pins the audit fix: percentiles use the
// nearest-rank index, so for two entries P50 is the lower value (not the max,
// which the old floor(p/100*n) formula returned).
func TestComputeStatsPercentileNearestRank(t *testing.T) {
	entries := []Entry{
		{RawTokens: 100, ShapedTokens: 90}, // saving 10
		{RawTokens: 100, ShapedTokens: 10}, // saving 90
	}
	s := computeStats(entries)
	if s.P50 != 10 {
		t.Errorf("P50 = %d; want 10 (lower of two — nearest-rank, not the max)", s.P50)
	}
	if s.P99 != 90 {
		t.Errorf("P99 = %d; want 90 (the max)", s.P99)
	}
}

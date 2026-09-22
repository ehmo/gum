package gain

import (
	"path/filepath"
	"testing"
	"time"
)

// reportEntry builds a minimal entry for the grouping tests. Only the fields
// the report layer reads are set; everything else stays at its zero value.
func reportEntry(session, opID, family string, raw, shaped int) Entry {
	return Entry{
		Session:         session,
		OpID:            opID,
		OpFamily:        family,
		RawTokens:       raw,
		ShapedTokens:    shaped,
		CacheStatus:     "miss",
		FieldMaskStatus: "applied",
	}
}

func TestOperationsGroupsAndSorts(t *testing.T) {
	entries := []Entry{
		reportEntry("aaaaaaaa", "gmail.users.messages.list", "gmail.users.messages", 100, 10),
		reportEntry("aaaaaaaa", "gmail.users.messages.list", "gmail.users.messages", 200, 20),
		reportEntry("aaaaaaaa", "drive.files.list", "drive.files", 50, 5),
	}
	// Same op_id, different cache_status: a separate row, sorted after "miss".
	hit := reportEntry("aaaaaaaa", "gmail.users.messages.list", "gmail.users.messages", 8, 8)
	hit.CacheStatus = "semantic"
	entries = append(entries, hit)

	rows := Operations(entries)
	if len(rows) != 3 {
		t.Fatalf("Operations rows = %d; want 3", len(rows))
	}
	if rows[0].OpID != "drive.files.list" {
		t.Errorf("rows[0].OpID = %q; want drive.files.list", rows[0].OpID)
	}
	if rows[1].OpID != "gmail.users.messages.list" || rows[1].CacheStatus != "miss" {
		t.Errorf("rows[1] = %q/%q; want gmail.users.messages.list/miss", rows[1].OpID, rows[1].CacheStatus)
	}
	if rows[1].Calls != 2 || rows[1].BaselineTokens != 300 || rows[1].ActualTokens != 30 {
		t.Errorf("rows[1] calls/baseline/actual = %d/%d/%d; want 2/300/30",
			rows[1].Calls, rows[1].BaselineTokens, rows[1].ActualTokens)
	}
	if rows[2].CacheStatus != "semantic" {
		t.Errorf("rows[2].CacheStatus = %q; want semantic", rows[2].CacheStatus)
	}
	if rows[1].OpFamily != "gmail.users.messages" {
		t.Errorf("rows[1].OpFamily = %q; want gmail.users.messages", rows[1].OpFamily)
	}
}

func TestOperationsSortsOnFieldMaskWhenOpAndCacheTie(t *testing.T) {
	skipped := reportEntry("aaaaaaaa", "drive.files.list", "drive.files", 10, 10)
	skipped.FieldMaskStatus = "skipped"
	rows := Operations([]Entry{
		skipped,
		reportEntry("aaaaaaaa", "drive.files.list", "drive.files", 100, 10),
	})
	if len(rows) != 2 {
		t.Fatalf("rows = %d; want 2", len(rows))
	}
	if rows[0].FieldMaskStatus != "applied" || rows[1].FieldMaskStatus != "skipped" {
		t.Errorf("field-mask order = %q,%q; want applied,skipped",
			rows[0].FieldMaskStatus, rows[1].FieldMaskStatus)
	}
}

func TestHistoryGroupsBySessionAndFamilyInLedgerOrder(t *testing.T) {
	rows := History([]Entry{
		reportEntry("bbbbbbbb", "gmail.users.messages.list", "gmail.users.messages", 100, 10),
		reportEntry("aaaaaaaa", "drive.files.list", "drive.files", 200, 100),
		reportEntry("bbbbbbbb", "gmail.users.messages.get", "gmail.users.messages", 100, 30),
	})
	if len(rows) != 2 {
		t.Fatalf("History rows = %d; want 2", len(rows))
	}
	if rows[0].Session != "bbbbbbbb" || rows[0].OpFamily != "gmail.users.messages" {
		t.Fatalf("rows[0] = %q/%q; want bbbbbbbb/gmail.users.messages", rows[0].Session, rows[0].OpFamily)
	}
	if rows[0].BaselineTokens != 200 || rows[0].ActualTokens != 40 {
		t.Errorf("rows[0] baseline/actual = %d/%d; want 200/40", rows[0].BaselineTokens, rows[0].ActualTokens)
	}
	if rows[0].SavingsPct == nil || *rows[0].SavingsPct != 80 {
		t.Errorf("rows[0].SavingsPct = %v; want 80", rows[0].SavingsPct)
	}
	if rows[1].Session != "aaaaaaaa" {
		t.Errorf("rows[1].Session = %q; want aaaaaaaa (first-appearance order)", rows[1].Session)
	}
	if rows[1].SavingsPct == nil || *rows[1].SavingsPct != 50 {
		t.Errorf("rows[1].SavingsPct = %v; want 50", rows[1].SavingsPct)
	}
}

func TestHistorySavingsPctNilWithoutBaseline(t *testing.T) {
	rows := History([]Entry{reportEntry("aaaaaaaa", "gum_parallel", "gum_parallel", 0, 300)})
	if len(rows) != 1 {
		t.Fatalf("rows = %d; want 1", len(rows))
	}
	if rows[0].SavingsPct != nil {
		t.Errorf("SavingsPct = %v; want nil for a zero baseline", *rows[0].SavingsPct)
	}
}

func TestSessionsAggregatesAndSortsFamilies(t *testing.T) {
	rows := Sessions([]Entry{
		reportEntry("bbbbbbbb", "gmail.users.messages.list", "gmail.users.messages", 100, 10),
		reportEntry("aaaaaaaa", "drive.files.list", "drive.files", 100, 50),
		reportEntry("bbbbbbbb", "calendar.events.list", "calendar.events", 100, 30),
	})
	if len(rows) != 2 {
		t.Fatalf("Sessions rows = %d; want 2", len(rows))
	}
	if rows[0].Session != "bbbbbbbb" || rows[0].Calls != 2 {
		t.Fatalf("rows[0] = %q calls=%d; want bbbbbbbb calls=2", rows[0].Session, rows[0].Calls)
	}
	want := []string{"calendar.events", "gmail.users.messages"}
	if len(rows[0].OpFamilies) != 2 || rows[0].OpFamilies[0] != want[0] || rows[0].OpFamilies[1] != want[1] {
		t.Errorf("rows[0].OpFamilies = %v; want %v", rows[0].OpFamilies, want)
	}
	if rows[0].BaselineTokens != 200 || rows[0].ActualTokens != 40 {
		t.Errorf("rows[0] baseline/actual = %d/%d; want 200/40", rows[0].BaselineTokens, rows[0].ActualTokens)
	}
	if rows[0].SavingsPct == nil || *rows[0].SavingsPct != 80 {
		t.Errorf("rows[0].SavingsPct = %v; want 80", rows[0].SavingsPct)
	}
	if rows[1].SavingsPct == nil || *rows[1].SavingsPct != 50 {
		t.Errorf("rows[1].SavingsPct = %v; want 50", rows[1].SavingsPct)
	}
}

func TestSessionsSavingsPctNilWithoutBaseline(t *testing.T) {
	rows := Sessions([]Entry{reportEntry("aaaaaaaa", "gum_parallel", "gum_parallel", 0, 300)})
	if len(rows) != 1 {
		t.Fatalf("rows = %d; want 1", len(rows))
	}
	if rows[0].SavingsPct != nil {
		t.Errorf("SavingsPct = %v; want nil for a zero baseline", *rows[0].SavingsPct)
	}
}

func TestNewReportCarriesModeIndependentTotals(t *testing.T) {
	entries := []Entry{
		reportEntry("aaaaaaaa", "gmail.users.messages.list", "gmail.users.messages", 100, 10),
		reportEntry("aaaaaaaa", "drive.files.list", "drive.files", 100, 40),
	}
	r := NewReport("session", "all", entries)
	if r.Mode != "session" || r.Window != "all" {
		t.Errorf("mode/window = %q/%q; want session/all", r.Mode, r.Window)
	}
	if r.BaselineTokens != 200 || r.ActualTokens != 50 || r.SavingsTokens != 150 {
		t.Errorf("baseline/actual/savings = %d/%d/%d; want 200/50/150",
			r.BaselineTokens, r.ActualTokens, r.SavingsTokens)
	}
	if r.SavingsPct == nil || *r.SavingsPct != 75 {
		t.Errorf("SavingsPct = %v; want 75", r.SavingsPct)
	}
	if r.Tokenizer != TokenizerName {
		t.Errorf("Tokenizer = %q; want %q", r.Tokenizer, TokenizerName)
	}
	if r.Sessions != nil || r.Operations != nil || r.History != nil {
		t.Error("NewReport attached a mode array; the caller owns that choice")
	}
}

func TestNewReportEmptyLedgerHasNilPercentages(t *testing.T) {
	r := NewReport("summary", "all", nil)
	if r.BaselineTokens != 0 || r.SavingsTokens != 0 {
		t.Errorf("baseline/savings = %d/%d; want 0/0", r.BaselineTokens, r.SavingsTokens)
	}
	if r.SavingsPct != nil {
		t.Errorf("SavingsPct = %v; want nil with no baseline", *r.SavingsPct)
	}
	if r.EndToEndSavings != nil {
		t.Errorf("EndToEndSavings = %v; want nil with no fixture-backed evidence", *r.EndToEndSavings)
	}
}

// TestFilterMatchesEachRejectBranch pins the three reasons Filter.matches
// drops an entry, so a future filter field cannot silently widen the set.
func TestFilterMatchesEachRejectBranch(t *testing.T) {
	base := reportEntry("aaaaaaaa", "drive.files.list", "drive.files", 10, 1)
	base.Timestamp = "2026-01-03T00:00:00Z"

	retry := base
	retry.IsRetry = true

	since, err := parseTestTime("2026-01-04T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name   string
		filter Filter
		entry  Entry
		want   bool
	}{
		{"zero filter admits", Filter{}, base, true},
		{"window lower bound rejects", Filter{Since: since}, base, false},
		{"session mismatch rejects", Filter{Session: "bbbbbbbb"}, base, false},
		{"session match admits", Filter{Session: "aaaaaaaa"}, base, true},
		{"exclude-retries rejects a retry", Filter{ExcludeRetries: true}, retry, false},
		{"exclude-retries admits a non-retry", Filter{ExcludeRetries: true}, base, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.filter.matches(tc.entry); got != tc.want {
				t.Errorf("matches = %v; want %v", got, tc.want)
			}
		})
	}
}

func TestLedgerStatsForAndStatsByOpForApplyTheFilter(t *testing.T) {
	dir := t.TempDir()
	l, err := NewLedger(filepath.Join(dir, LedgerFileName))
	if err != nil {
		t.Fatal(err)
	}
	retry := reportEntry("bbbbbbbb", "drive.files.list", "drive.files", 100, 90)
	retry.IsRetry = true
	for _, e := range []Entry{
		reportEntry("aaaaaaaa", "gmail.users.messages.list", "gmail.users.messages", 100, 10),
		retry,
	} {
		if err := l.Append(e); err != nil {
			t.Fatal(err)
		}
	}

	all := l.StatsFor(Filter{})
	if all.TotalCalls != 2 {
		t.Errorf("unfiltered TotalCalls = %d; want 2", all.TotalCalls)
	}

	one := l.StatsFor(Filter{Session: "aaaaaaaa"})
	if one.TotalCalls != 1 || one.TotalTokensSaved != 90 {
		t.Errorf("session-filtered calls/saved = %d/%d; want 1/90", one.TotalCalls, one.TotalTokensSaved)
	}

	noRetries := l.StatsFor(Filter{ExcludeRetries: true})
	if noRetries.TotalCalls != 1 {
		t.Errorf("exclude-retries TotalCalls = %d; want 1", noRetries.TotalCalls)
	}

	byOp := l.StatsByOpFor(Filter{Session: "aaaaaaaa"})
	if len(byOp) != 1 {
		t.Fatalf("StatsByOpFor returned %d ops; want 1", len(byOp))
	}
	if byOp["gmail.users.messages.list"].TotalCalls != 1 {
		t.Errorf("byOp calls = %d; want 1", byOp["gmail.users.messages.list"].TotalCalls)
	}

	// StatsBetween and StatsByOp are the window-only spellings of the same
	// two calls; a divergence here means one of them grew its own logic.
	if l.StatsBetween(since0(), since0()).TotalCalls != all.TotalCalls {
		t.Error("StatsBetween diverged from StatsFor with open bounds")
	}
	if len(l.StatsByOp(since0(), since0())) != len(l.StatsByOpFor(Filter{})) {
		t.Error("StatsByOp diverged from StatsByOpFor with open bounds")
	}
}

func parseTestTime(v string) (time.Time, error) { return time.Parse(time.RFC3339, v) }

func since0() time.Time { return time.Time{} }

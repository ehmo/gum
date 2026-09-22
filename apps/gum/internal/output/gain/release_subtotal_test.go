package gain

import (
	"path/filepath"
	"testing"
)

// §12.3 savings-claim accounting (bead gum-7oap). Three subsets of one ledger
// answer three different questions, and the release gate may only read the
// first: fixture-backed entries with the gum_parallel envelope charged in.

func TestSubtotalPctIsNilWithoutBaseline(t *testing.T) {
	if got := (Subtotal{}).Pct(); got != nil {
		t.Errorf("Pct() = %v; an empty subset has no percentage, and 0 would read as a measured result", *got)
	}
	if got := (Subtotal{TokensIn: -5, TokensSaved: 10}).Pct(); got != nil {
		t.Errorf("Pct() = %v; a negative baseline is not divisible", *got)
	}
}

func TestSubtotalPctCanBeNegative(t *testing.T) {
	got := Subtotal{TokensIn: 100, TokensSaved: -20}.Pct()
	if got == nil {
		t.Fatal("Pct() = nil; 100 tokens in is a baseline")
	}
	if *got != -20 {
		t.Errorf("Pct() = %v; want -20, because a batch may cost more than it saves", *got)
	}
}

// statsFixture writes one ledger holding, in order:
//   - two fixture-backed inner entries, 1000 raw -> 100 shaped each
//   - one fixture-backed gum_parallel outer entry costing 300 tokens
//   - one estimated inner entry, 500 raw -> 50 shaped
func statsFixture(t *testing.T) Stats {
	t.Helper()

	l, err := NewLedger(filepath.Join(t.TempDir(), LedgerFileName))
	if err != nil {
		t.Fatalf("NewLedger: %v", err)
	}
	defer func() { _ = l.Close() }()

	inner := func(baseline string, raw, shaped int) Entry {
		return Entry{
			Session:        "0badc0de",
			OpID:           "gmail.users.messages.list",
			OpFamily:       "gmail.users.messages",
			RawTokens:      raw,
			ShapedTokens:   shaped,
			BaselineMethod: baseline,
		}
	}

	outer := NewGumParallelOuterEntry("0badc0de", "feedface", "hash", 2, 100, 200, BaselineFixtureReplay)

	for _, e := range []Entry{
		inner(BaselineFixtureReplay, 1000, 100),
		inner(BaselineFixtureReplay, 1000, 100),
		outer,
		inner(BaselineEstimated, 500, 50),
	} {
		if err := l.Append(e); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}
	return l.Stats()
}

func TestStatsReleaseSubtotalsSplitFixtureFromEstimated(t *testing.T) {
	s := statsFixture(t)

	// Whole window, both baseline methods: 2500 raw in, 1800 + 450 saved,
	// minus the 300-token envelope.
	if s.TotalTokensIn != 2500 {
		t.Errorf("TotalTokensIn = %d; want 2500", s.TotalTokensIn)
	}
	if s.TotalTokensSaved != 1950 {
		t.Errorf("TotalTokensSaved = %d; want 1950", s.TotalTokensSaved)
	}

	// Release: fixture-backed only, envelope charged in. 2000 in, 1800 - 300.
	if s.Release.TokensIn != 2000 || s.Release.TokensSaved != 1500 {
		t.Errorf("Release = %+v; want {TokensIn:2000 TokensSaved:1500}", s.Release)
	}
	if pct := s.Release.Pct(); pct == nil || *pct != 75 {
		t.Errorf("Release.Pct() = %v; want 75", pct)
	}

	// ReleaseInner: the same calls with the envelope removed.
	if s.ReleaseInner.TokensIn != 2000 || s.ReleaseInner.TokensSaved != 1800 {
		t.Errorf("ReleaseInner = %+v; want {TokensIn:2000 TokensSaved:1800}", s.ReleaseInner)
	}
	if pct := s.ReleaseInner.Pct(); pct == nil || *pct != 90 {
		t.Errorf("ReleaseInner.Pct() = %v; want 90", pct)
	}

	if s.BatchEnvelopeTokens != 300 {
		t.Errorf("BatchEnvelopeTokens = %d; want 300, the outer entry's shaped_tokens", s.BatchEnvelopeTokens)
	}
}

func TestStatsReleaseSubtotalsEmptyWithoutFixtureEntries(t *testing.T) {
	l, err := NewLedger(filepath.Join(t.TempDir(), LedgerFileName))
	if err != nil {
		t.Fatalf("NewLedger: %v", err)
	}
	defer func() { _ = l.Close() }()

	if err := l.Append(Entry{Session: "0badc0de", OpID: "op.a", RawTokens: 900, ShapedTokens: 90, BaselineMethod: BaselineEstimated}); err != nil {
		t.Fatalf("Append: %v", err)
	}

	s := l.Stats()
	if s.Release.Pct() != nil || s.ReleaseInner.Pct() != nil {
		t.Error("an all-estimated ledger has no release-gating evidence, so both subtotals must stay empty")
	}
	if s.BatchEnvelopeTokens != 0 {
		t.Errorf("BatchEnvelopeTokens = %d; want 0", s.BatchEnvelopeTokens)
	}
	if s.AggregateSavingsPct == 0 {
		t.Error("the window figure still describes the estimated entry; only the release subtotals exclude it")
	}
}

func TestEntryIsParallelOuter(t *testing.T) {
	outer := NewGumParallelOuterEntry("0badc0de", "feedface", "hash", 1, 10, 20, BaselineEstimated)
	if !outer.IsParallelOuter() {
		t.Error("the gum_parallel sentinel is not recognised as an outer entry")
	}
	if (Entry{OpFamily: "gmail.users.messages"}).IsParallelOuter() {
		t.Error("an ordinary inner entry was treated as a batch envelope")
	}
}

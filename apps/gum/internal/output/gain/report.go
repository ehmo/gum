package gain

import (
	"sort"
	"time"
)

// Filter selects which ledger entries one `gum gain` invocation reports on.
// The zero value selects every entry.
type Filter struct {
	// Since and Until bound Entry.Timestamp inclusively. A zero bound is
	// open-ended, and an entry with no parsable timestamp always passes, so
	// a filter never silently zeroes out a legacy ledger.
	Since time.Time
	Until time.Time

	// Session is the 8-character hex session prefix to report on. Empty
	// selects every session.
	Session string

	// ExcludeRetries drops entries with is_retry=true from the reported
	// aggregate. Per spec §12.3 it changes only what is displayed; the
	// ledger file keeps every entry.
	ExcludeRetries bool
}

func (f Filter) matches(e Entry) bool {
	if !entryInWindow(e, f.Since, f.Until) {
		return false
	}
	if f.Session != "" && e.Session != f.Session {
		return false
	}
	if f.ExcludeRetries && e.IsRetry {
		return false
	}
	return true
}

// OperationRow is one `#/$defs/GainResult` `operations[]` row: the calls that
// share an op_id, cache_status and field_mask_status.
type OperationRow struct {
	OpID            string `json:"op_id"`
	OpFamily        string `json:"op_family"`
	Calls           int64  `json:"calls"`
	BaselineTokens  int64  `json:"baseline_tokens"`
	ActualTokens    int64  `json:"actual_tokens"`
	CacheStatus     string `json:"cache_status"`
	FieldMaskStatus string `json:"field_mask_status"`
}

// HistoryRow is one `#/$defs/GainResult` `history[]` row: one session's calls
// within one op_family.
type HistoryRow struct {
	Session        string   `json:"session"`
	OpFamily       string   `json:"op_family"`
	BaselineTokens int64    `json:"baseline_tokens"`
	ActualTokens   int64    `json:"actual_tokens"`
	SavingsPct     *float64 `json:"savings_pct"`
}

// Report is the `#/$defs/GainResult` success envelope shared by
// `gum gain --format=json` and `gum.gain` (spec §12.3).
//
// The mode arrays are pointers because the schema distinguishes "this mode
// reported nothing" from "this mode was not selected": the selected array is
// emitted as `[]` when empty, and the other two must be absent.
type Report struct {
	Mode                  string   `json:"mode"`
	Window                string   `json:"window"`
	BaselineTokens        int64    `json:"baseline_tokens"`
	ActualTokens          int64    `json:"actual_tokens"`
	SavingsTokens         int64    `json:"savings_tokens"`
	SavingsPct            *float64 `json:"savings_pct"`
	EndToEndSavings       *float64 `json:"end_to_end_savings"`
	PerOpShapingSavings   *float64 `json:"per_op_shaping_savings"`
	BatchEnvelopeOverhead int64    `json:"batch_envelope_overhead"`
	Tokenizer             string   `json:"tokenizer"`

	Sessions   *[]SessionRow   `json:"sessions,omitempty"`
	Operations *[]OperationRow `json:"operations,omitempty"`
	History    *[]HistoryRow   `json:"history,omitempty"`
}

// SessionRow is one `#/$defs/GainResult` `sessions[]` row.
type SessionRow struct {
	Session        string   `json:"session"`
	Calls          int64    `json:"calls"`
	BaselineTokens int64    `json:"baseline_tokens"`
	ActualTokens   int64    `json:"actual_tokens"`
	SavingsPct     *float64 `json:"savings_pct"`
	OpFamilies     []string `json:"op_families"`
}

// Select returns a copy of the ledger entries the filter admits, in ledger
// order. Ledger order is append order, which is chronological.
func (l *Ledger) Select(f Filter) []Entry {
	l.mu.Lock()
	l.ensureLoadedLocked()
	out := make([]Entry, 0, len(l.entries))
	for _, e := range l.entries {
		if f.matches(e) {
			out = append(out, e)
		}
	}
	l.mu.Unlock()
	return out
}

// Operations groups entries into `operations[]` rows by
// op_id x cache_status x field_mask_status, sorted on those three keys so
// two runs over one ledger print the same rows in the same order.
func Operations(entries []Entry) []OperationRow {
	type key struct{ opID, cache, mask string }
	index := make(map[key]*OperationRow)
	for _, e := range entries {
		k := key{opID: e.OpID, cache: e.CacheStatus, mask: e.FieldMaskStatus}
		row, ok := index[k]
		if !ok {
			row = &OperationRow{
				OpID:            e.OpID,
				OpFamily:        e.OpFamily,
				CacheStatus:     e.CacheStatus,
				FieldMaskStatus: e.FieldMaskStatus,
			}
			index[k] = row
		}
		row.Calls++
		row.BaselineTokens += int64(e.RawTokens)
		row.ActualTokens += int64(e.ShapedTokens)
	}

	rows := make([]OperationRow, 0, len(index))
	for _, row := range index {
		rows = append(rows, *row)
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].OpID != rows[j].OpID {
			return rows[i].OpID < rows[j].OpID
		}
		if rows[i].CacheStatus != rows[j].CacheStatus {
			return rows[i].CacheStatus < rows[j].CacheStatus
		}
		return rows[i].FieldMaskStatus < rows[j].FieldMaskStatus
	})
	return rows
}

// History groups entries into `history[]` rows by session x op_family, in
// the order each pair first appears in the ledger. Ledger order is append
// order, so the rows come out chronological without re-parsing timestamps
// that legacy entries may not carry.
func History(entries []Entry) []HistoryRow {
	type key struct{ session, family string }
	index := make(map[key]*HistoryRow)
	order := make([]key, 0, len(entries))
	for _, e := range entries {
		k := key{session: e.Session, family: e.OpFamily}
		row, ok := index[k]
		if !ok {
			row = &HistoryRow{Session: e.Session, OpFamily: e.OpFamily}
			index[k] = row
			order = append(order, k)
		}
		row.BaselineTokens += int64(e.RawTokens)
		row.ActualTokens += int64(e.ShapedTokens)
	}

	rows := make([]HistoryRow, 0, len(order))
	for _, k := range order {
		row := *index[k]
		row.SavingsPct = Subtotal{
			TokensIn:    row.BaselineTokens,
			TokensSaved: row.BaselineTokens - row.ActualTokens,
		}.Pct()
		rows = append(rows, row)
	}
	return rows
}

// Sessions groups entries into `sessions[]` rows, in the order each session
// first appears in the ledger. op_families is sorted so the row is stable.
func Sessions(entries []Entry) []SessionRow {
	index := make(map[string]*SessionRow)
	families := make(map[string]map[string]bool)
	order := make([]string, 0, len(entries))
	for _, e := range entries {
		row, ok := index[e.Session]
		if !ok {
			row = &SessionRow{Session: e.Session}
			index[e.Session] = row
			families[e.Session] = make(map[string]bool)
			order = append(order, e.Session)
		}
		row.Calls++
		row.BaselineTokens += int64(e.RawTokens)
		row.ActualTokens += int64(e.ShapedTokens)
		families[e.Session][e.OpFamily] = true
	}

	rows := make([]SessionRow, 0, len(order))
	for _, session := range order {
		row := *index[session]
		row.SavingsPct = Subtotal{
			TokensIn:    row.BaselineTokens,
			TokensSaved: row.BaselineTokens - row.ActualTokens,
		}.Pct()
		names := make([]string, 0, len(families[session]))
		for family := range families[session] {
			names = append(names, family)
		}
		sort.Strings(names)
		row.OpFamilies = names
		rows = append(rows, row)
	}
	return rows
}

// NewReport builds the mode-independent half of a GainResult from entries
// the caller has already filtered. The caller attaches exactly one mode
// array.
func NewReport(mode, window string, entries []Entry) Report {
	stats := computeStats(entries)
	baseline := stats.TotalTokensIn
	saved := stats.TotalTokensSaved
	return Report{
		Mode:           mode,
		Window:         window,
		BaselineTokens: baseline,
		ActualTokens:   baseline - saved,
		SavingsTokens:  saved,
		SavingsPct: Subtotal{
			TokensIn:    baseline,
			TokensSaved: saved,
		}.Pct(),
		EndToEndSavings:       stats.Release.Pct(),
		PerOpShapingSavings:   stats.ReleaseInner.Pct(),
		BatchEnvelopeOverhead: stats.BatchEnvelopeTokens,
		Tokenizer:             TokenizerName,
	}
}

// StatsFor computes Stats over the entries the filter admits.
func (l *Ledger) StatsFor(f Filter) Stats {
	return computeStats(l.Select(f))
}

// StatsByOpFor aggregates Stats per op_id over the entries the filter admits.
func (l *Ledger) StatsByOpFor(f Filter) map[string]Stats {
	byOp := make(map[string][]Entry)
	for _, e := range l.Select(f) {
		byOp[e.OpID] = append(byOp[e.OpID], e)
	}

	out := make(map[string]Stats, len(byOp))
	for opID, entries := range byOp {
		out[opID] = computeStats(entries)
	}
	return out
}

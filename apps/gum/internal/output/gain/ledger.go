// Package gain provides the gum gain ledger: an append-only JSONL file recording
// token-savings metrics for each dispatch invocation.
//
// Ledger path: ~/.local/share/gum/gain-ledger.jsonl (default; override via NewLedger).
// Rotation: the file is rotated when it exceeds 100 MB; the old file is renamed to
// gain-ledger-<unix-timestamp>.jsonl.
//
// Token counting uses cl100k_base via github.com/tiktoken-go/tokenizer v0.7.0.
//
// Ledger record shape is normative per spec §12.3:
//   - The first record in every file is a header:
//     {"record_type":"header","schema_version":1,"tokenizer":"cl100k_base"}
//   - Every subsequent record is an entry with record_type:"entry" and the
//     fields enumerated on Entry below.
//   - The gum_parallel outer sentinel entry uses fixed values
//     (op_id="gum_parallel", op_family="gum_parallel", variant_id=null,
//     output_profile=null, auth_subject_fingerprint="batch") — see
//     NewGumParallelOuterEntry.
package gain

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/tiktoken-go/tokenizer"
)

// Record type sentinels and tokenizer name pinned by spec §12.3.
const (
	RecordTypeHeader = "header"
	RecordTypeEntry  = "entry"
	TokenizerName    = "cl100k_base"
	SchemaVersion    = 1
)

// maxLedgerSize is the soft size threshold that triggers automatic rotation
// in Append. Declared as a package-level var (not const) so package-internal
// tests can shrink it to exercise the rotation branch without writing 100 MB.
var maxLedgerSize int64 = 100 * 1024 * 1024

// Header is the first JSONL record written to every gain-ledger file
// (spec §12.3 line "first JSONL record is a header"). It pins the
// tokenizer identity so historical entries remain comparable across
// tokenizer changes.
type Header struct {
	RecordType    string `json:"record_type"`
	SchemaVersion int    `json:"schema_version"`
	Tokenizer     string `json:"tokenizer"`
}

// NewHeader returns the canonical v1/cl100k_base header.
func NewHeader() Header {
	return Header{
		RecordType:    RecordTypeHeader,
		SchemaVersion: SchemaVersion,
		Tokenizer:     TokenizerName,
	}
}

// Entry is a single gain-ledger record (record_type="entry") as specified
// in §12.3. Required fields have no omitempty so zero values round-trip;
// optional fields use omitempty per the spec's "optional" annotation.
//
// VariantID and OutputProfile are *string so a gum_parallel outer entry
// can emit JSON null (spec §12.3 outer-entry contract); inner entries
// pass non-nil pointers.
type Entry struct {
	// Session is the 8-character hex prefix of the session token.
	Session string `json:"session"`

	// OpID is the catalog op_id, e.g. "gmail.users.messages.list".
	// For the parallel outer sentinel this is the literal "gum_parallel".
	OpID string `json:"op_id"`

	// VariantID is the resolved variant_id. Null for parallel outer entries.
	VariantID *string `json:"variant_id"`

	// OutputProfile is the resolved expression-profile id. Null for parallel
	// outer entries (the batch has no single profile).
	OutputProfile *string `json:"output_profile"`

	// ArgsHash is the SHA-256 hex digest of the JCS-canonical args (§10.0).
	// For the parallel outer this is the digest of the full batch args canonical.
	ArgsHash string `json:"args_hash"`

	// AuthSubjectFingerprint is the per-credential identity dimension
	// (§10.0.1). For parallel outer entries this is the literal "batch".
	AuthSubjectFingerprint string `json:"auth_subject_fingerprint"`

	// RequestTokens is the cl100k_base token count of the outgoing request.
	RequestTokens int `json:"request_tokens"`

	// ResponseTokens is the cl100k_base token count of the incoming response.
	ResponseTokens int `json:"response_tokens"`

	// RawTokens is the cl100k_base token count of the raw upstream response
	// body before any expression-pipeline shaping. For the parallel outer
	// sentinel this is always 0.
	RawTokens int `json:"raw_tokens"`

	// ShapedTokens is the cl100k_base token count of the post-shaping body.
	// For the parallel outer sentinel this equals request_tokens+response_tokens.
	ShapedTokens int `json:"shaped_tokens"`

	// CacheStatus is one of "miss", "hit", "semantic", "not_applicable".
	// Parallel outer entries use "not_applicable".
	CacheStatus string `json:"cache_status"`

	// FieldMaskStatus is one of "applied", "skipped", "not_applicable".
	// Parallel outer entries use "not_applicable".
	FieldMaskStatus string `json:"field_mask_status"`

	// ServedFromCache is true when the response was served entirely from
	// cache (no executor call). Parallel outer entries are always false.
	ServedFromCache bool `json:"served_from_cache"`

	// IsRetry is true when this call repeats a prior session+op_family+
	// args_hash within 5 minutes. Parallel outer entries are always false.
	IsRetry bool `json:"is_retry"`

	// OpFamily is op_id with the terminal method stripped, e.g.
	// "gmail.users.messages". For the parallel outer sentinel this is
	// the literal "gum_parallel".
	OpFamily string `json:"op_family"`

	// BaselineMethod is "fixture_replay" for fixture-backed entries and
	// "estimated" for live entries excluded from release-gating claims.
	BaselineMethod string `json:"baseline_method"`

	// BenchmarkFixtureID identifies the fixture (only when
	// baseline_method="fixture_replay").
	BenchmarkFixtureID string `json:"benchmark_fixture_id,omitempty"`

	// BatchID groups the outer + N inner entries from one gum_parallel call.
	BatchID string `json:"batch_id,omitempty"`

	// BatchIndex is the 0-based position within the batch (inner entries only).
	// *int so zero is meaningful while absence is JSON-omitted.
	BatchIndex *int `json:"batch_index,omitempty"`

	// ElementCount is N (parallel outer entries only).
	ElementCount *int `json:"element_count,omitempty"`

	// ErrorCode is set on failed dispatches.
	ErrorCode string `json:"error_code,omitempty"`

	// Cancelled is true when the enclosing context was cancelled after the
	// batch dispatch began.
	Cancelled bool `json:"cancelled,omitempty"`

	// Timestamp is the RFC3339-UTC wall-clock time at which Append wrote
	// this entry. omitempty so historical ledgers written before this field
	// existed parse cleanly; StatsBetween treats absent timestamps as
	// unfiltered (always included) so --since/--until never silently drops
	// old evidence.
	Timestamp string `json:"ts,omitempty"`
}

// entryAlias is a method-stripped alias used by MarshalJSON to avoid
// recursing into Entry.MarshalJSON when emitting the on-wire envelope.
type entryAlias Entry

// entryWire is the on-wire envelope: it injects record_type:"entry" as the
// first field and delegates the rest to Entry's natural marshaling.
type entryWire struct {
	RecordType string `json:"record_type"`
	entryAlias
}

// MarshalJSON injects record_type="entry" at the head of every Entry
// serialization, matching the spec §12.3 wire contract.
func (e Entry) MarshalJSON() ([]byte, error) {
	return json.Marshal(entryWire{RecordType: RecordTypeEntry, entryAlias: entryAlias(e)})
}

// NewGumParallelOuterEntry constructs the spec §12.3 normative outer
// sentinel entry for a single gum_parallel call. Callers fill batchID
// (8-char hex), argsHash (SHA-256 of the batch args canonical), n
// (element count), requestTokens / responseTokens (outer tools/call
// envelope cost), and baselineMethod ("fixture_replay" or "estimated").
//
// All sentinel constants come straight from §12.3:
//
//	op_id                  = "gum_parallel"
//	variant_id             = null
//	output_profile         = null
//	auth_subject_fingerprint = "batch"
//	op_family              = "gum_parallel"
//	raw_tokens             = 0
//	shaped_tokens          = request_tokens + response_tokens
//	cache_status           = "not_applicable"
//	field_mask_status      = "not_applicable"
//	served_from_cache      = false
//	is_retry               = false
func NewGumParallelOuterEntry(session, batchID, argsHash string, n, requestTokens, responseTokens int, baselineMethod string) Entry {
	elem := n
	return Entry{
		Session:                session,
		OpID:                   "gum_parallel",
		VariantID:              nil,
		OutputProfile:          nil,
		ArgsHash:               argsHash,
		AuthSubjectFingerprint: "batch",
		RequestTokens:          requestTokens,
		ResponseTokens:         responseTokens,
		RawTokens:              0,
		ShapedTokens:           requestTokens + responseTokens,
		CacheStatus:            "not_applicable",
		FieldMaskStatus:        "not_applicable",
		ServedFromCache:        false,
		IsRetry:                false,
		OpFamily:               "gum_parallel",
		BaselineMethod:         baselineMethod,
		BatchID:                batchID,
		ElementCount:           &elem,
	}
}

// Stats summarises gains across all entries in the ledger.
type Stats struct {
	// TotalCalls is the number of ledger entries.
	TotalCalls int64 `json:"total_calls"`

	// TotalTokensIn is the sum of RawTokens across all entries
	// (the naive-baseline denominator for aggregate savings %).
	TotalTokensIn int64 `json:"total_tokens_in"`

	// TotalTokensSaved is the sum of (RawTokens - ShapedTokens) across all entries.
	TotalTokensSaved int64 `json:"total_tokens_saved"`

	// AggregateSavingsPct is TotalTokensSaved / TotalTokensIn (0 when TotalTokensIn == 0).
	// This is the release-gate metric per spec §2 ("≥80% reduction in MCP-layer tokens"),
	// not MeanSavingsPerCall which is per-call mean of absolute deltas.
	AggregateSavingsPct float64 `json:"aggregate_savings_pct"`

	// MeanSavingsPerCall is TotalTokensSaved / TotalCalls (0 when TotalCalls == 0).
	MeanSavingsPerCall float64 `json:"mean_savings_per_call"`

	// P50, P95, P99 are the 50th, 95th, and 99th percentile per-call token savings.
	P50 int64 `json:"p50"`
	P95 int64 `json:"p95"`
	P99 int64 `json:"p99"`
}

// Ledger is an append-only gain ledger backed by a JSONL file.
type Ledger struct {
	path    string
	file    *os.File
	mu      sync.Mutex
	entries []Entry
}

// cachedCodec holds the cached tokenizer to avoid repeated initialization.
var (
	codecOnce      sync.Once
	cachedCodec    tokenizer.Codec
	cachedCodecErr error
)

func getCodec() (tokenizer.Codec, error) {
	codecOnce.Do(func() {
		cachedCodec, cachedCodecErr = tokenizer.Get(tokenizer.Cl100kBase)
	})
	return cachedCodec, cachedCodecErr
}

// MeasureTokensCl100k counts the cl100k_base tokens in data using the
// tiktoken-go/tokenizer library. Returns an error if the tokenizer cannot
// be initialised or the data cannot be encoded.
func MeasureTokensCl100k(data []byte) (int, error) {
	codec, err := getCodec()
	if err != nil {
		return 0, fmt.Errorf("gain: init tokenizer: %w", err)
	}
	ids, _, err := codec.Encode(string(data))
	if err != nil {
		return 0, fmt.Errorf("gain: tokenize: %w", err)
	}
	return len(ids), nil
}

// NewLedger opens (or creates) a gain ledger at path.
// If path is "" the default path ~/.local/share/gum/gain-ledger.jsonl is used.
// The parent directory is created with 0o755 permissions if it does not exist.
//
// A spec §12.3 header record is written as the first line of any file that
// is empty when opened (newly created or pre-existing 0-byte).
func NewLedger(path string) (*Ledger, error) {
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("gain: get home dir: %w", err)
		}
		path = filepath.Join(home, ".local", "share", "gum", "gain-ledger.jsonl")
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("gain: create ledger dir: %w", err)
	}

	var entries []Entry
	if data, err := os.Open(path); err == nil {
		if info, statErr := data.Stat(); statErr == nil && info.IsDir() {
			_ = data.Close()
		} else {
			scanner := bufio.NewScanner(data)
			for scanner.Scan() {
				line := scanner.Bytes()
				if len(line) == 0 {
					continue
				}
				// Skip header records; only entries contribute to Stats.
				var head struct {
					RecordType string `json:"record_type"`
				}
				if err := json.Unmarshal(line, &head); err != nil {
					continue
				}
				if head.RecordType != RecordTypeEntry {
					continue
				}
				var e Entry
				if err := json.Unmarshal(line, &e); err == nil {
					entries = append(entries, e)
				}
			}
			if err := scanner.Err(); err != nil {
				_ = data.Close()
				return nil, fmt.Errorf("gain: scan ledger: %w", err)
			}
			if err := data.Close(); err != nil {
				return nil, fmt.Errorf("gain: close ledger after scan: %w", err)
			}
		}
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("gain: open ledger: %w", err)
	}

	l := &Ledger{
		path:    path,
		file:    f,
		entries: entries,
	}

	if info, statErr := f.Stat(); statErr == nil && info.Size() == 0 {
		if writeErr := l.writeHeaderLocked(); writeErr != nil {
			_ = f.Close()
			return nil, writeErr
		}
	}

	return l, nil
}

// writeHeaderLocked writes the canonical header record. Caller must hold l.mu,
// or call it during construction before the Ledger is published to other goroutines.
func (l *Ledger) writeHeaderLocked() error {
	data, err := json.Marshal(NewHeader())
	if err != nil {
		return fmt.Errorf("gain: marshal header: %w", err)
	}
	data = append(data, '\n')
	if _, err := l.file.Write(data); err != nil {
		return fmt.Errorf("gain: write header: %w", err)
	}
	return nil
}

// Append writes a single Entry to the ledger as a JSON line.
// Append is safe for concurrent use from a single process.
// If the file size exceeds 100 MB after the write, Append rotates the file
// before returning.
func (l *Ledger) Append(e Entry) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if e.Timestamp == "" {
		e.Timestamp = time.Now().UTC().Format(time.RFC3339Nano)
	}

	data, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("gain: marshal entry: %w", err)
	}
	data = append(data, '\n')

	if _, err := l.file.Write(data); err != nil {
		return fmt.Errorf("gain: write entry: %w", err)
	}
	l.entries = append(l.entries, e)

	info, err := l.file.Stat()
	if err == nil && info.Size() > maxLedgerSize {
		if rotErr := l.rotateLocked(); rotErr != nil {
			slog.Warn("gain: rotate ledger failed", "path", l.path, "err", rotErr)
		}
	}

	return nil
}

// rotateLocked renames the current ledger file, opens a new empty one,
// and writes a fresh header so the new file is spec-compliant on first byte.
// Must be called with l.mu held.
func (l *Ledger) rotateLocked() error {
	// fsync before the rename so the archived file's tail is durable: Close()
	// flushes to the OS page cache but does not persist it, so a crash between
	// Close and Rename could silently drop the most recent entries.
	if err := l.file.Sync(); err != nil {
		return fmt.Errorf("sync before rotate: %w", err)
	}
	if err := l.file.Close(); err != nil {
		return fmt.Errorf("close before rotate: %w", err)
	}
	ts := time.Now().Unix()
	ext := filepath.Ext(l.path)
	base := l.path[:len(l.path)-len(ext)]
	rotated := fmt.Sprintf("%s-%d%s", base, ts, ext)
	if err := os.Rename(l.path, rotated); err != nil {
		return fmt.Errorf("rename: %w", err)
	}
	f, err := os.OpenFile(l.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open new: %w", err)
	}
	l.file = f
	return l.writeHeaderLocked()
}

// Stats computes summary statistics over all entries currently in the ledger.
func (l *Ledger) Stats() Stats {
	l.mu.Lock()
	entries := make([]Entry, len(l.entries))
	copy(entries, l.entries)
	l.mu.Unlock()

	return computeStats(entries)
}

// StatsBetween computes stats over the subset of entries whose Timestamp
// falls in [since, until]. Zero-value bounds are treated as open-ended:
// since==zero means "no lower bound", until==zero means "no upper bound".
//
// Entries without a Timestamp (older ledgers) are always included so a
// `--since` filter on a fresh process does not silently zero out
// historical evidence. Callers who need strict time-bounded counts should
// rotate the ledger first or filter the JSONL externally.
func (l *Ledger) StatsBetween(since, until time.Time) Stats {
	l.mu.Lock()
	entries := make([]Entry, 0, len(l.entries))
	for _, e := range l.entries {
		if !entryInWindow(e, since, until) {
			continue
		}
		entries = append(entries, e)
	}
	l.mu.Unlock()
	return computeStats(entries)
}

// StatsByOp aggregates stats per op_id over the subset of entries whose
// Timestamp falls in [since, until] (same open-ended semantics as
// StatsBetween). The result maps op_id → Stats for that op only. Used by
// `gum gain --by-op` (review gum-y5wb).
func (l *Ledger) StatsByOp(since, until time.Time) map[string]Stats {
	l.mu.Lock()
	byOp := make(map[string][]Entry)
	for _, e := range l.entries {
		if !entryInWindow(e, since, until) {
			continue
		}
		byOp[e.OpID] = append(byOp[e.OpID], e)
	}
	l.mu.Unlock()

	out := make(map[string]Stats, len(byOp))
	for opID, entries := range byOp {
		out[opID] = computeStats(entries)
	}
	return out
}

// entryInWindow returns true when e falls in [since, until]. Entries
// without a parsed Timestamp pass through (see StatsBetween rationale).
func entryInWindow(e Entry, since, until time.Time) bool {
	if e.Timestamp == "" {
		return true
	}
	t, err := time.Parse(time.RFC3339Nano, e.Timestamp)
	if err != nil {
		t, err = time.Parse(time.RFC3339, e.Timestamp)
		if err != nil {
			return true
		}
	}
	if !since.IsZero() && t.Before(since) {
		return false
	}
	if !until.IsZero() && t.After(until) {
		return false
	}
	return true
}

// computeStats computes stats from a slice of entries.
func computeStats(entries []Entry) Stats {
	if len(entries) == 0 {
		return Stats{}
	}

	savings := make([]int64, len(entries))
	var total, totalIn int64
	for i, e := range entries {
		s := int64(e.RawTokens - e.ShapedTokens)
		savings[i] = s
		total += s
		totalIn += int64(e.RawTokens)
	}

	sort.Slice(savings, func(i, j int) bool { return savings[i] < savings[j] })

	n := len(savings)
	// Nearest-rank percentile (0-based): ceil(p/100 * n) - 1, clamped. The old
	// floor(p/100 * n) was biased one rank high — e.g. for n=2 it returned the
	// MAX as the P50. Integer ceil avoids a math import.
	pctIdx := func(p int) int {
		idx := (p*n+99)/100 - 1
		if idx < 0 {
			idx = 0
		}
		if idx >= n {
			idx = n - 1
		}
		return idx
	}
	p50 := savings[pctIdx(50)]
	p95 := savings[pctIdx(95)]
	p99 := savings[pctIdx(99)]

	var aggPct float64
	if totalIn > 0 {
		aggPct = float64(total) / float64(totalIn)
	}

	return Stats{
		TotalCalls:          int64(n),
		TotalTokensIn:       totalIn,
		TotalTokensSaved:    total,
		AggregateSavingsPct: aggPct,
		MeanSavingsPerCall:  float64(total) / float64(n),
		P50:                 p50,
		P95:                 p95,
		P99:                 p99,
	}
}

// Close flushes any pending writes and closes the underlying file.
// Close is idempotent: subsequent calls after the first are no-ops.
func (l *Ledger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file == nil {
		return nil
	}
	err := l.file.Close()
	l.file = nil
	return err
}

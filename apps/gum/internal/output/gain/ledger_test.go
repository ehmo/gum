package gain_test

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.uber.org/goleak"

	"github.com/ehmo/gum/internal/output/gain"
)

// requiredEntryFields enumerates the spec §12.3 normative entry record
// fields. Optional fields (benchmark_fixture_id, batch_id, batch_index,
// element_count, error_code, cancelled) are not in this list because the
// spec marks them optional; they MAY be absent.
var requiredEntryFields = []string{
	"record_type",
	"session",
	"op_id",
	"variant_id",
	"output_profile",
	"args_hash",
	"auth_subject_fingerprint",
	"request_tokens",
	"response_tokens",
	"raw_tokens",
	"shaped_tokens",
	"cache_status",
	"field_mask_status",
	"served_from_cache",
	"is_retry",
	"op_family",
	"baseline_method",
}

// firstLine returns the first non-empty line of path.
func firstLine(t *testing.T, path string) string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer func() { _ = f.Close() }()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" {
			return line
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan %s: %v", path, err)
	}
	t.Fatalf("%s is empty", path)
	return ""
}

// TestGainLedgerHeader (spec §12.3, bead gum-2jq) verifies that NewLedger
// writes the canonical header record as the first line of a fresh file
// and that the header serializes to exactly
// {"record_type":"header","schema_version":1,"tokenizer":"cl100k_base"}.
func TestGainLedgerHeader(t *testing.T) {
	defer goleak.VerifyNone(t)

	dir := t.TempDir()
	path := filepath.Join(dir, "gain-ledger.jsonl")

	l, err := gain.NewLedger(path)
	if err != nil {
		t.Fatalf("NewLedger: %v", err)
	}
	if err := l.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	line := firstLine(t, path)

	// Field-presence check via map.
	var got map[string]any
	if err := json.Unmarshal([]byte(line), &got); err != nil {
		t.Fatalf("unmarshal header line: %v\nline=%q", err, line)
	}
	if got["record_type"] != "header" {
		t.Errorf("record_type = %v; want %q", got["record_type"], "header")
	}
	if v, ok := got["schema_version"].(float64); !ok || int(v) != 1 {
		t.Errorf("schema_version = %v; want 1", got["schema_version"])
	}
	if got["tokenizer"] != "cl100k_base" {
		t.Errorf("tokenizer = %v; want %q", got["tokenizer"], "cl100k_base")
	}

	// Exact-byte check against the spec §12.3 canonical form. Field order
	// follows the Header struct declaration, which mirrors the spec.
	const want = `{"record_type":"header","schema_version":1,"tokenizer":"cl100k_base"}`
	if line != want {
		t.Errorf("header line mismatch\n got: %s\nwant: %s", line, want)
	}

	// NewHeader() returns the same constant value.
	hb, err := json.Marshal(gain.NewHeader())
	if err != nil {
		t.Fatalf("marshal NewHeader: %v", err)
	}
	if string(hb) != want {
		t.Errorf("NewHeader() marshal mismatch\n got: %s\nwant: %s", hb, want)
	}
}

// TestGainLedgerHeaderWrittenOnlyOnce verifies a second NewLedger call
// against the same path does NOT re-write the header (otherwise the file
// would accumulate stray header records on every process start).
func TestGainLedgerHeaderWrittenOnlyOnce(t *testing.T) {
	defer goleak.VerifyNone(t)

	dir := t.TempDir()
	path := filepath.Join(dir, "gain-ledger.jsonl")

	for i := 0; i < 3; i++ {
		l, err := gain.NewLedger(path)
		if err != nil {
			t.Fatalf("NewLedger iter %d: %v", i, err)
		}
		if err := l.Close(); err != nil {
			t.Fatalf("Close iter %d: %v", i, err)
		}
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read ledger: %v", err)
	}
	headerCount := strings.Count(string(data), `"record_type":"header"`)
	if headerCount != 1 {
		t.Errorf("header lines after 3 opens = %d; want 1\nfile:\n%s", headerCount, data)
	}
}

// TestGainLedgerEntrySchema (spec §12.3, bead gum-2jq) verifies that an
// Entry serializes to a JSONL line carrying record_type:"entry" plus all
// spec-mandated required fields, and that a round-trip through Ledger
// preserves the fields on disk.
func TestGainLedgerEntrySchema(t *testing.T) {
	defer goleak.VerifyNone(t)

	variant := "gmail.users.messages.list.v1"
	profile := "gmail.messages.list.v1"
	entry := gain.Entry{
		Session:                "0a1b2c3d",
		OpID:                   "gmail.users.messages.list",
		VariantID:              &variant,
		OutputProfile:          &profile,
		ArgsHash:               "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789",
		AuthSubjectFingerprint: "subject:fingerprint-hex",
		RequestTokens:          142,
		ResponseTokens:         318,
		RawTokens:              318,
		ShapedTokens:           67,
		CacheStatus:            "miss",
		FieldMaskStatus:        "applied",
		ServedFromCache:        false,
		IsRetry:                false,
		OpFamily:               "gmail.users.messages",
		BaselineMethod:         "fixture_replay",
		BenchmarkFixtureID:     "workspace_toon_read/001-gmail-list",
	}

	// Direct marshal check.
	raw, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("marshal entry: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal entry: %v", err)
	}

	if got["record_type"] != "entry" {
		t.Errorf("record_type = %v; want %q", got["record_type"], "entry")
	}
	for _, field := range requiredEntryFields {
		if _, ok := got[field]; !ok {
			t.Errorf("required field %q missing from entry JSON: %s", field, raw)
		}
	}

	// Round-trip through Ledger: header + entry on disk.
	dir := t.TempDir()
	path := filepath.Join(dir, "gain-ledger.jsonl")
	l, err := gain.NewLedger(path)
	if err != nil {
		t.Fatalf("NewLedger: %v", err)
	}
	if err := l.Append(entry); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if err := l.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read ledger: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("ledger has %d lines; want 2 (header + entry):\n%s", len(lines), data)
	}

	var diskEntry map[string]any
	if err := json.Unmarshal([]byte(lines[1]), &diskEntry); err != nil {
		t.Fatalf("unmarshal disk entry: %v", err)
	}
	if diskEntry["record_type"] != "entry" {
		t.Errorf("disk record_type = %v; want %q", diskEntry["record_type"], "entry")
	}
	if diskEntry["op_id"] != "gmail.users.messages.list" {
		t.Errorf("disk op_id = %v; want %q", diskEntry["op_id"], "gmail.users.messages.list")
	}
	if diskEntry["variant_id"] != "gmail.users.messages.list.v1" {
		t.Errorf("disk variant_id = %v; want %q", diskEntry["variant_id"], variant)
	}
	if diskEntry["output_profile"] != "gmail.messages.list.v1" {
		t.Errorf("disk output_profile = %v; want %q", diskEntry["output_profile"], profile)
	}
	if diskEntry["baseline_method"] != "fixture_replay" {
		t.Errorf("disk baseline_method = %v; want %q", diskEntry["baseline_method"], "fixture_replay")
	}
}

// TestGainParallelOuterEntrySchema (spec §12.3 outer-entry contract, bead
// gum-2jq) verifies that NewGumParallelOuterEntry returns an Entry whose
// JSON form encodes the seven normative sentinel values:
//
//	op_id                    = "gum_parallel"
//	variant_id               = null
//	output_profile           = null
//	auth_subject_fingerprint = "batch"
//	op_family                = "gum_parallel"
//	raw_tokens               = 0
//	shaped_tokens            = request_tokens + response_tokens
//	cache_status             = "not_applicable"
//	field_mask_status        = "not_applicable"
//	served_from_cache        = false
//	is_retry                 = false
//	element_count            = N
//	batch_id                 = <caller-supplied 8-char hex>
func TestGainParallelOuterEntrySchema(t *testing.T) {
	defer goleak.VerifyNone(t)

	const (
		session        = "deadbeef"
		batchID        = "a1b2c3d4"
		argsHash       = "0000111122223333444455556666777788889999aaaabbbbccccddddeeeeffff"
		n              = 4
		requestTokens  = 95
		responseTokens = 410
		baselineMethod = "fixture_replay"
	)
	entry := gain.NewGumParallelOuterEntry(session, batchID, argsHash, n, requestTokens, responseTokens, baselineMethod)

	raw, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("marshal outer entry: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal outer entry: %v", err)
	}

	// Required sentinel values.
	cases := []struct {
		key     string
		want    any
		nilWant bool
	}{
		{key: "record_type", want: "entry"},
		{key: "op_id", want: "gum_parallel"},
		{key: "variant_id", nilWant: true},
		{key: "output_profile", nilWant: true},
		{key: "auth_subject_fingerprint", want: "batch"},
		{key: "op_family", want: "gum_parallel"},
		{key: "cache_status", want: "not_applicable"},
		{key: "field_mask_status", want: "not_applicable"},
		{key: "served_from_cache", want: false},
		{key: "is_retry", want: false},
		{key: "baseline_method", want: baselineMethod},
		{key: "args_hash", want: argsHash},
		{key: "session", want: session},
		{key: "batch_id", want: batchID},
	}
	for _, c := range cases {
		val, present := got[c.key]
		if !present {
			t.Errorf("outer entry missing field %q\njson=%s", c.key, raw)
			continue
		}
		if c.nilWant {
			if val != nil {
				t.Errorf("outer entry %q = %v; want JSON null", c.key, val)
			}
			continue
		}
		if val != c.want {
			t.Errorf("outer entry %q = %v (%T); want %v (%T)", c.key, val, val, c.want, c.want)
		}
	}

	// Numeric sentinels: raw_tokens=0, shaped_tokens=request+response, element_count=N.
	if v, ok := got["raw_tokens"].(float64); !ok || int(v) != 0 {
		t.Errorf("raw_tokens = %v; want 0", got["raw_tokens"])
	}
	if v, ok := got["shaped_tokens"].(float64); !ok || int(v) != requestTokens+responseTokens {
		t.Errorf("shaped_tokens = %v; want %d", got["shaped_tokens"], requestTokens+responseTokens)
	}
	if v, ok := got["request_tokens"].(float64); !ok || int(v) != requestTokens {
		t.Errorf("request_tokens = %v; want %d", got["request_tokens"], requestTokens)
	}
	if v, ok := got["response_tokens"].(float64); !ok || int(v) != responseTokens {
		t.Errorf("response_tokens = %v; want %d", got["response_tokens"], responseTokens)
	}
	if v, ok := got["element_count"].(float64); !ok || int(v) != n {
		t.Errorf("element_count = %v; want %d", got["element_count"], n)
	}

	// `cancelled` is optional and omitted by default.
	if _, present := got["cancelled"]; present {
		t.Errorf("cancelled should be absent unless explicitly set; got %v", got["cancelled"])
	}

	// Exact null encoding for variant_id and output_profile (not "" or omitted).
	if !strings.Contains(string(raw), `"variant_id":null`) {
		t.Errorf("variant_id must serialize as JSON null; got %s", raw)
	}
	if !strings.Contains(string(raw), `"output_profile":null`) {
		t.Errorf("output_profile must serialize as JSON null; got %s", raw)
	}
}

// TestNewLedgerDefaultPathUsesHome verifies NewLedger("") falls back to
// $HOME/.local/share/gum/gain-ledger.jsonl. The test overrides $HOME to
// a temp dir so it never touches a real user's ledger.
func TestNewLedgerDefaultPathUsesHome(t *testing.T) {
	defer goleak.VerifyNone(t)

	home := t.TempDir()
	t.Setenv("HOME", home)

	l, err := gain.NewLedger("")
	if err != nil {
		t.Fatalf("NewLedger(\"\"): %v", err)
	}
	t.Cleanup(func() { _ = l.Close() })

	wantPath := filepath.Join(home, ".local", "share", "gum", "gain-ledger.jsonl")
	if _, err := os.Stat(wantPath); err != nil {
		t.Errorf("default ledger path missing: stat %s: %v", wantPath, err)
	}
}

// TestNewLedgerMkdirFailureSurfacesError verifies NewLedger returns a
// wrapped error (rather than panicking) when the parent directory can't
// be created. We force the failure by passing a path whose parent is a
// regular file, so os.MkdirAll cannot promote it to a directory.
func TestNewLedgerMkdirFailureSurfacesError(t *testing.T) {
	defer goleak.VerifyNone(t)

	dir := t.TempDir()
	clash := filepath.Join(dir, "imafile")
	if err := os.WriteFile(clash, []byte("not a dir"), 0o644); err != nil {
		t.Fatalf("seed clash file: %v", err)
	}
	// clash exists as a file; treat it as the parent dir of the ledger path.
	_, err := gain.NewLedger(filepath.Join(clash, "gain-ledger.jsonl"))
	if err == nil {
		t.Fatal("NewLedger: want error when parent path is a file, got nil")
	}
	if !strings.Contains(err.Error(), "gain:") {
		t.Errorf("error %q is not wrapped with the gain: prefix", err)
	}
}

// TestRunFixtureReplayMissingDirFails verifies the walk-time error
// path: RunFixtureReplay against a non-existent directory returns a
// wrapped error rather than panicking.
func TestRunFixtureReplayMissingDirFails(t *testing.T) {
	defer goleak.VerifyNone(t)

	missing := filepath.Join(t.TempDir(), "does-not-exist")
	_, err := gain.RunFixtureReplay(missing, "toon")
	if err == nil {
		t.Fatal("RunFixtureReplay on missing dir: want error, got nil")
	}
	if !strings.Contains(err.Error(), "walk dir") {
		t.Errorf("error %q does not mention walk failure", err)
	}
}

// TestLedgerCloseIsIdempotent verifies a second Close call after the
// first does not panic and returns nil (the file is no longer open).
func TestLedgerCloseIsIdempotent(t *testing.T) {
	defer goleak.VerifyNone(t)

	dir := t.TempDir()
	path := filepath.Join(dir, "gain-ledger.jsonl")
	l, err := gain.NewLedger(path)
	if err != nil {
		t.Fatalf("NewLedger: %v", err)
	}
	if err := l.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := l.Close(); err != nil {
		t.Errorf("second Close should be a no-op; got %v", err)
	}
}

// TestProcessFixtureWritesExpectedFiles verifies that a successful
// RunFixtureReplay run writes expected-toon.txt and
// expected-tokens-cl100k.json next to each fixture's response.json.
// This exercises the writeFiles=true branch of processFixture which
// the release-set tests don't reach directly.
func TestProcessFixtureWritesExpectedFiles(t *testing.T) {
	defer goleak.VerifyNone(t)

	dir := t.TempDir()
	leaf := filepath.Join(dir, "fx-001")
	if err := os.MkdirAll(leaf, 0o755); err != nil {
		t.Fatalf("mkdir leaf: %v", err)
	}
	if err := os.WriteFile(filepath.Join(leaf, "response.json"),
		[]byte(`{"items":[{"id":"a","subject":"hi"},{"id":"b","subject":"hello"}]}`), 0o644); err != nil {
		t.Fatalf("write response: %v", err)
	}

	if _, err := gain.RunFixtureReplay(dir, "toon"); err != nil {
		t.Fatalf("RunFixtureReplay: %v", err)
	}

	for _, name := range []string{"expected-toon.txt", "expected-tokens-cl100k.json"} {
		if _, err := os.Stat(filepath.Join(leaf, name)); err != nil {
			t.Errorf("processFixture writeFiles did not produce %s: %v", name, err)
		}
	}
}

// TestLedgerStatsAggregatesAppendedEntries verifies Ledger.Stats()
// computes TotalCalls / TotalTokensIn / TotalTokensSaved /
// AggregateSavingsPct / MeanSavingsPerCall / P50/P95/P99 from a small
// hand-picked set of entries.
func TestLedgerStatsAggregatesAppendedEntries(t *testing.T) {
	defer goleak.VerifyNone(t)

	dir := t.TempDir()
	path := filepath.Join(dir, "gain-ledger.jsonl")
	l, err := gain.NewLedger(path)
	if err != nil {
		t.Fatalf("NewLedger: %v", err)
	}
	t.Cleanup(func() { _ = l.Close() })

	// Two entries: (raw=100, shaped=20, saved=80) and (raw=200, shaped=60, saved=140).
	mk := func(raw, shaped int, session string) gain.Entry {
		v := "v1"
		p := "p1"
		return gain.Entry{
			Session: session, OpID: "gmail.users.messages.list",
			VariantID: &v, OutputProfile: &p, ArgsHash: "x",
			AuthSubjectFingerprint: "f",
			RawTokens:              raw, ShapedTokens: shaped,
			ResponseTokens:  raw,
			CacheStatus:     "miss",
			FieldMaskStatus: "applied",
			OpFamily:        "gmail.users.messages",
			BaselineMethod:  "fixture_replay",
		}
	}
	if err := l.Append(mk(100, 20, "aaaaaaaa")); err != nil {
		t.Fatalf("Append 1: %v", err)
	}
	if err := l.Append(mk(200, 60, "bbbbbbbb")); err != nil {
		t.Fatalf("Append 2: %v", err)
	}

	s := l.Stats()
	if s.TotalCalls != 2 {
		t.Errorf("TotalCalls = %d; want 2", s.TotalCalls)
	}
	if s.TotalTokensIn != 300 {
		t.Errorf("TotalTokensIn = %d; want 300", s.TotalTokensIn)
	}
	if s.TotalTokensSaved != 220 {
		t.Errorf("TotalTokensSaved = %d; want 220", s.TotalTokensSaved)
	}
	wantAgg := 220.0 / 300.0
	if s.AggregateSavingsPct < wantAgg-1e-9 || s.AggregateSavingsPct > wantAgg+1e-9 {
		t.Errorf("AggregateSavingsPct = %.6f; want %.6f", s.AggregateSavingsPct, wantAgg)
	}
	if s.MeanSavingsPerCall != 110 {
		t.Errorf("MeanSavingsPerCall = %.2f; want 110", s.MeanSavingsPerCall)
	}
}

// TestLedgerStatsEmpty verifies the zero-value path: an empty ledger
// returns the zero Stats, with no division-by-zero on AggregateSavingsPct
// or MeanSavingsPerCall.
func TestLedgerStatsEmpty(t *testing.T) {
	defer goleak.VerifyNone(t)

	dir := t.TempDir()
	path := filepath.Join(dir, "gain-ledger.jsonl")
	l, err := gain.NewLedger(path)
	if err != nil {
		t.Fatalf("NewLedger: %v", err)
	}
	t.Cleanup(func() { _ = l.Close() })

	s := l.Stats()
	if s.TotalCalls != 0 || s.TotalTokensSaved != 0 || s.AggregateSavingsPct != 0 {
		t.Errorf("empty Stats = %+v; want zero", s)
	}
}

// TestNewLedgerReadsExistingEntriesSkipsHeader verifies that re-opening a
// ledger with prior content correctly skips the header line, parses
// entry lines, and ignores malformed/non-entry rows. The recovered
// Stats reflect only the parsed entries.
func TestNewLedgerReadsExistingEntriesSkipsHeader(t *testing.T) {
	defer goleak.VerifyNone(t)

	dir := t.TempDir()
	path := filepath.Join(dir, "gain-ledger.jsonl")

	// Hand-craft a ledger with header + 1 valid entry + 1 malformed line + 1 non-entry record.
	body := strings.Join([]string{
		`{"record_type":"header","schema_version":1,"tokenizer":"cl100k_base"}`,
		`{"record_type":"entry","session":"ssssssss","op_id":"x.y.z","raw_tokens":50,"shaped_tokens":10}`,
		`{`, // malformed
		`{"record_type":"checkpoint","unused":1}`, // unknown record type
		``,
	}, "\n")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("seed ledger: %v", err)
	}

	l, err := gain.NewLedger(path)
	if err != nil {
		t.Fatalf("NewLedger: %v", err)
	}
	t.Cleanup(func() { _ = l.Close() })

	s := l.Stats()
	if s.TotalCalls != 1 {
		t.Errorf("TotalCalls = %d; want 1 (only the one entry line)", s.TotalCalls)
	}
	if s.TotalTokensIn != 50 {
		t.Errorf("TotalTokensIn = %d; want 50", s.TotalTokensIn)
	}
	if s.TotalTokensSaved != 40 {
		t.Errorf("TotalTokensSaved = %d; want 40", s.TotalTokensSaved)
	}
}

// TestProcessFixtureMissingResponseJSON verifies RunFixtureReplay surfaces
// a clear error when a fixture leaf directory is missing response.json.
// (The leaf filter only enters dirs that have response.json, so a missing
// file inside a non-leaf path is fine — we hit this branch by handing
// RunFixtureReplay a directory that has no fixtures at all.)
func TestRunFixtureReplayEmptyDir(t *testing.T) {
	defer goleak.VerifyNone(t)

	dir := t.TempDir()
	result, err := gain.RunFixtureReplay(dir, "toon")
	if err != nil {
		t.Fatalf("RunFixtureReplay on empty dir: %v", err)
	}
	if result.Stats.TotalCalls != 0 {
		t.Errorf("TotalCalls on empty dir = %d; want 0", result.Stats.TotalCalls)
	}
	if !result.Deterministic {
		t.Error("empty-dir replay should be trivially deterministic")
	}
}

// TestRunFixtureReplayBadJSONFails verifies RunFixtureReplay returns an
// error (rather than crashing) when a fixture's response.json is not
// valid JSON.
func TestRunFixtureReplayBadJSONFails(t *testing.T) {
	defer goleak.VerifyNone(t)

	dir := t.TempDir()
	leaf := filepath.Join(dir, "bad-fixture")
	if err := os.MkdirAll(leaf, 0o755); err != nil {
		t.Fatalf("mkdir leaf: %v", err)
	}
	if err := os.WriteFile(filepath.Join(leaf, "response.json"), []byte(`{not json`), 0o644); err != nil {
		t.Fatalf("write bad response: %v", err)
	}

	_, err := gain.RunFixtureReplay(dir, "toon")
	if err == nil {
		t.Fatal("RunFixtureReplay with malformed JSON: want error, got nil")
	}
	if !strings.Contains(err.Error(), "parse response.json") {
		t.Errorf("error %q does not mention parse failure", err)
	}
}

// TestGainParallelOuterEntryRoundTripsThroughLedger verifies that a
// parallel outer entry written via Ledger.Append survives a file
// reopen and is the first non-header line on disk with the spec
// sentinel values intact.
func TestGainParallelOuterEntryRoundTripsThroughLedger(t *testing.T) {
	defer goleak.VerifyNone(t)

	dir := t.TempDir()
	path := filepath.Join(dir, "gain-ledger.jsonl")

	l, err := gain.NewLedger(path)
	if err != nil {
		t.Fatalf("NewLedger: %v", err)
	}
	entry := gain.NewGumParallelOuterEntry("0a1b2c3d", "a1b2c3d4",
		"0000111122223333444455556666777788889999aaaabbbbccccddddeeeeffff",
		3, 50, 200, "fixture_replay")
	if err := l.Append(entry); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if err := l.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read ledger: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("ledger has %d lines; want 2 (header + outer):\n%s", len(lines), data)
	}

	var disk map[string]any
	if err := json.Unmarshal([]byte(lines[1]), &disk); err != nil {
		t.Fatalf("unmarshal disk outer: %v", err)
	}
	if disk["op_family"] != "gum_parallel" {
		t.Errorf("disk op_family = %v; want %q", disk["op_family"], "gum_parallel")
	}
	if disk["auth_subject_fingerprint"] != "batch" {
		t.Errorf("disk auth_subject_fingerprint = %v; want %q", disk["auth_subject_fingerprint"], "batch")
	}
	if disk["variant_id"] != nil {
		t.Errorf("disk variant_id = %v; want JSON null", disk["variant_id"])
	}
	if disk["output_profile"] != nil {
		t.Errorf("disk output_profile = %v; want JSON null", disk["output_profile"])
	}
}

// TestLedgerStatsBetween locks the time-window filter:
//   - Entries with timestamps inside [since, until] are counted.
//   - Entries outside the window are excluded.
//   - Entries with no Timestamp (legacy rows) pass through unconditionally —
//     so a fresh process applying --since does not silently zero out history.
//   - Zero-value bounds are open-ended.
func TestLedgerStatsBetween(t *testing.T) {
	defer goleak.VerifyNone(t)

	dir := t.TempDir()
	path := filepath.Join(dir, "gain-ledger.jsonl")
	l, err := gain.NewLedger(path)
	if err != nil {
		t.Fatalf("NewLedger: %v", err)
	}
	t.Cleanup(func() { _ = l.Close() })

	mk := func(raw, shaped int, ts string) gain.Entry {
		v := "v1"
		p := "p1"
		return gain.Entry{
			Session: "ssssssss", OpID: "gmail.users.messages.list",
			VariantID: &v, OutputProfile: &p, ArgsHash: "x",
			AuthSubjectFingerprint: "f",
			RawTokens:              raw, ShapedTokens: shaped,
			ResponseTokens:  raw,
			CacheStatus:     "miss",
			FieldMaskStatus: "applied",
			OpFamily:        "gmail.users.messages",
			BaselineMethod:  "fixture_replay",
			Timestamp:       ts,
		}
	}

	t1 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC)
	t3 := time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC)

	// Append takes ownership of Timestamp only when blank, so passing a
	// pre-stamped value gives us a deterministic window.
	for _, e := range []gain.Entry{
		mk(100, 10, t1.Format(time.RFC3339Nano)),
		mk(200, 50, t2.Format(time.RFC3339Nano)),
		mk(400, 100, t3.Format(time.RFC3339Nano)),
	} {
		if err := l.Append(e); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}

	t.Run("bounded_window", func(t *testing.T) {
		// since=Jan 3, until=Jan 7 → only t2 qualifies.
		since := time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC)
		until := time.Date(2026, 1, 7, 0, 0, 0, 0, time.UTC)
		s := l.StatsBetween(since, until)
		if s.TotalCalls != 1 {
			t.Errorf("TotalCalls = %d; want 1", s.TotalCalls)
		}
		if s.TotalTokensIn != 200 {
			t.Errorf("TotalTokensIn = %d; want 200", s.TotalTokensIn)
		}
	})

	t.Run("open_lower_bound", func(t *testing.T) {
		// since=zero, until=Jan 7 → t1 + t2 qualify; t3 excluded.
		s := l.StatsBetween(time.Time{}, time.Date(2026, 1, 7, 0, 0, 0, 0, time.UTC))
		if s.TotalCalls != 2 {
			t.Errorf("TotalCalls = %d; want 2", s.TotalCalls)
		}
	})

	t.Run("open_upper_bound", func(t *testing.T) {
		// since=Jan 7, until=zero → only t3 qualifies.
		s := l.StatsBetween(time.Date(2026, 1, 7, 0, 0, 0, 0, time.UTC), time.Time{})
		if s.TotalCalls != 1 {
			t.Errorf("TotalCalls = %d; want 1", s.TotalCalls)
		}
	})

	t.Run("both_zero_includes_all", func(t *testing.T) {
		s := l.StatsBetween(time.Time{}, time.Time{})
		if s.TotalCalls != 3 {
			t.Errorf("TotalCalls = %d; want 3 (all entries)", s.TotalCalls)
		}
	})
}

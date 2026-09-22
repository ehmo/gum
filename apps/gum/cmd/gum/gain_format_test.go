package main

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"strings"
	"testing"
)

// runGainRaw executes `gum gain` against the seeded ledger and returns the
// bytes it wrote, without assuming the output is JSON. The format tests need
// the raw stream: text and CSV are the point.
func runGainRaw(t *testing.T, args ...string) string {
	t.Helper()

	cmd := newGainCmd()
	cmd.SetArgs(args)
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("gain %v: %v (output %s)", args, err, out.String())
	}
	return out.String()
}

// runGainReject executes `gum gain` expecting a rejection and returns the err.
func runGainReject(t *testing.T, args ...string) error {
	t.Helper()

	cmd := newGainCmd()
	cmd.SetArgs(args)
	cmd.SilenceUsage = true
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	err := cmd.Execute()
	if err == nil {
		t.Fatalf("gain %v succeeded; want a rejection (output %s)", args, out.String())
	}
	return err
}

// TestGainDefaultsToTheTextRenderer pins spec §12.3's `--format` default. The
// no-flag path used to JSON-encode a raw Stats object, so there was no
// human-readable gain output at all.
func TestGainDefaultsToTheTextRenderer(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	seedGainLedger(t)

	out := runGainRaw(t)
	if strings.HasPrefix(strings.TrimSpace(out), "{") {
		t.Fatalf("no-flag output is still JSON:\n%s", out)
	}
	for _, want := range []string{"summary", "last-30-sessions", "SESSION", "aaaaaaaa", "bbbbbbbb", "cl100k_base estimate"} {
		if !strings.Contains(out, want) {
			t.Errorf("text output has no %q:\n%s", want, out)
		}
	}
}

// TestGainSummaryModeEmitsSessions pins acceptance criterion 3: the no-flag
// path emits the §12.3 GainResult summary envelope with sessions[], the same
// shape gum.gain returns, not a bare Stats object.
func TestGainSummaryModeEmitsSessions(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	seedGainLedger(t)

	var report map[string]any
	if err := json.Unmarshal([]byte(runGainRaw(t, "--format", "json")), &report); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if report["mode"] != "summary" {
		t.Errorf("mode = %v; want summary", report["mode"])
	}
	if report["window"] != "last-30-sessions" {
		t.Errorf("window = %v; want last-30-sessions", report["window"])
	}
	if report["tokenizer"] != "cl100k_base" {
		t.Errorf("tokenizer = %v; want cl100k_base", report["tokenizer"])
	}
	for _, absent := range []string{"operations", "history"} {
		if _, present := report[absent]; present {
			t.Errorf("summary mode emitted %s[]; the schema allows one mode array", absent)
		}
	}
	sessions := rows(t, report, "sessions")
	if len(sessions) != 2 {
		t.Fatalf("sessions rows = %d; want 2", len(sessions))
	}
	if sessions[0]["session"] != "aaaaaaaa" || sessions[1]["session"] != "bbbbbbbb" {
		t.Fatalf("sessions not in ledger order: %v", sessions)
	}
	if got := num(t, sessions[0], "calls"); got != 3 {
		t.Errorf("aaaaaaaa calls = %v; want 3", got)
	}
	if got := num(t, report, "baseline_tokens"); got != 450 {
		t.Errorf("baseline_tokens = %v; want 450", got)
	}
}

// TestGainCSVEmitsOneRowPerDetailRow pins acceptance criterion 2 across all
// three modes: the CSV body carries exactly the rows the same mode displays.
func TestGainCSVEmitsOneRowPerDetailRow(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	seedGainLedger(t)

	cases := []struct {
		name     string
		args     []string
		header   []string
		wantRows int
		firstCol string
	}{
		{
			name:     "summary",
			args:     []string{"--format", "csv"},
			header:   []string{"session", "calls", "baseline_tokens", "actual_tokens", "savings_pct", "op_families"},
			wantRows: 2,
			firstCol: "aaaaaaaa",
		},
		{
			name:     "session",
			args:     []string{"--session", "aaaaaaaa", "--format", "csv"},
			header:   []string{"op_id", "op_family", "calls", "baseline_tokens", "actual_tokens", "cache_status", "field_mask_status"},
			wantRows: 2,
			firstCol: "drive.files.list",
		},
		{
			name:     "history",
			args:     []string{"--history", "--format", "csv"},
			header:   []string{"session", "op_family", "baseline_tokens", "actual_tokens", "savings_pct"},
			wantRows: 3,
			firstCol: "aaaaaaaa",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			records, err := csv.NewReader(strings.NewReader(runGainRaw(t, tc.args...))).ReadAll()
			if err != nil {
				t.Fatalf("parse CSV: %v", err)
			}
			if len(records) != tc.wantRows+1 {
				t.Fatalf("records = %d; want %d (header + %d rows): %v", len(records), tc.wantRows+1, tc.wantRows, records)
			}
			if strings.Join(records[0], ",") != strings.Join(tc.header, ",") {
				t.Errorf("header = %v; want %v", records[0], tc.header)
			}
			if records[1][0] != tc.firstCol {
				t.Errorf("first data cell = %q; want %q", records[1][0], tc.firstCol)
			}
		})
	}
}

// TestGainTextRendersEveryMode pins that --session, --history and --by-op all
// reach a table renderer rather than falling through to JSON.
func TestGainTextRendersEveryMode(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	seedGainLedger(t)

	cases := []struct {
		name string
		args []string
		want []string
	}{
		{"session", []string{"--session", "aaaaaaaa"}, []string{"session:aaaaaaaa", "OP_ID", "drive.files.list", "CACHE"}},
		{"history", []string{"--history"}, []string{"OP_FAMILY", "gmail.users.messages", "SAVINGS%"}},
		{"by_op", []string{"--by-op"}, []string{"OP_ID", "gmail.users.messages.list", "P95"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := runGainRaw(t, tc.args...)
			if strings.HasPrefix(strings.TrimSpace(out), "{") {
				t.Fatalf("%v still emits JSON:\n%s", tc.args, out)
			}
			for _, want := range tc.want {
				if !strings.Contains(out, want) {
					t.Errorf("output has no %q:\n%s", want, out)
				}
			}
		})
	}
}

// TestGainByOpCSVCarriesEveryOp pins the --by-op CSV renderer, which is the
// only mode whose rows come from a map rather than a GainResult array.
func TestGainByOpCSVCarriesEveryOp(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	seedGainLedger(t)

	records, err := csv.NewReader(strings.NewReader(runGainRaw(t, "--by-op", "--format", "csv"))).ReadAll()
	if err != nil {
		t.Fatalf("parse CSV: %v", err)
	}
	if len(records) != 3 {
		t.Fatalf("records = %d; want 3 (header + 2 ops): %v", len(records), records)
	}
	if records[0][0] != "op_id" {
		t.Errorf("header = %v; want op_id first", records[0])
	}
	if records[1][0] != "drive.files.list" || records[2][0] != "gmail.users.messages.list" {
		t.Errorf("op rows are not sorted by op_id: %v", records[1:])
	}
}

// TestGainFormatEnumIsEnforced pins the owner's one-enum decision: text, json
// and csv are the report formats, toon belongs to --fixture-replay only, and
// --fixture-replay refuses the two formats it has no renderer for.
func TestGainFormatEnumIsEnforced(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	seedGainLedger(t)

	cases := []struct {
		name string
		args []string
		want string
	}{
		{"toon_needs_fixture_replay", []string{"--format", "toon"}, "--fixture-replay"},
		{"unknown_format", []string{"--format", "xml"}, "text|json|csv"},
		{"replay_refuses_text", []string{"--fixture-replay", "--format", "text"}, "json or toon"},
		{"replay_refuses_csv", []string{"--fixture-replay", "--format", "csv"}, "json or toon"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := runGainReject(t, tc.args...)
			if !strings.Contains(err.Error(), "CLI_ARG_INVALID") {
				t.Errorf("err = %q; want CLI_ARG_INVALID", err)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %q; want it to mention %q", err, tc.want)
			}
		})
	}
}

// TestGainFixtureReplayKeepsToonAsItsDefault pins that widening the enum did
// not move the replay's measured shaping format. Spec §1 validates the >=80%
// headline claim against the TOON-default fixture set, so a json default here
// would silently retire the number the release gate publishes.
func TestGainFixtureReplayKeepsToonAsItsDefault(t *testing.T) {
	var result struct {
		Format string
		Stats  struct {
			TotalCalls int64 `json:"total_calls"`
		}
	}
	if err := json.Unmarshal([]byte(runGainRaw(t, "--fixture-replay")), &result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if result.Format != "toon" {
		t.Errorf("replay Format = %q; want toon", result.Format)
	}
	if result.Stats.TotalCalls == 0 {
		t.Error("replay reported zero calls")
	}
}

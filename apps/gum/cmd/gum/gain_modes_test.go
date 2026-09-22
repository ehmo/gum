package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/output/gain"
	"github.com/ehmo/gum/internal/profile"
)

// seedGainLedger writes the four-entry fixture the mode tests report on and
// returns the ledger path.
func seedGainLedger(t *testing.T) string {
	t.Helper()

	path, err := gain.DefaultPath(profile.DefaultName)
	if err != nil {
		t.Fatalf("DefaultPath: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	ledger, err := gain.NewLedger(path)
	if err != nil {
		t.Fatalf("NewLedger: %v", err)
	}
	defer func() { _ = ledger.Close() }()

	variant := "v1"
	outProfile := "default"
	entries := []gain.Entry{
		{
			Session: "aaaaaaaa", OpID: "gmail.users.messages.list",
			OpFamily: "gmail.users.messages", RawTokens: 100, ShapedTokens: 10,
			Timestamp: "2026-01-02T00:00:00Z",
		},
		{
			Session: "aaaaaaaa", OpID: "gmail.users.messages.list",
			OpFamily: "gmail.users.messages", RawTokens: 100, ShapedTokens: 20,
			Timestamp: "2026-01-03T00:00:00Z",
		},
		{
			Session: "aaaaaaaa", OpID: "drive.files.list",
			OpFamily: "drive.files", RawTokens: 50, ShapedTokens: 5,
			IsRetry: true, Timestamp: "2026-01-04T00:00:00Z",
		},
		{
			Session: "bbbbbbbb", OpID: "gmail.users.messages.list",
			OpFamily: "gmail.users.messages", RawTokens: 200, ShapedTokens: 20,
			Timestamp: "2026-01-05T00:00:00Z",
		},
	}
	for i := range entries {
		entries[i].VariantID = &variant
		entries[i].OutputProfile = &outProfile
		entries[i].ArgsHash = "h"
		entries[i].AuthSubjectFingerprint = "f"
		entries[i].CacheStatus = "miss"
		entries[i].FieldMaskStatus = "applied"
		entries[i].BaselineMethod = gain.BaselineEstimated
		if err := ledger.Append(entries[i]); err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
	}
	return path
}

// runGain executes `gum gain` with args against the seeded ledger and
// decodes the JSON it prints. --format=json is appended because text is the
// spec §12.3 default; the mode assertions below are about the schema
// envelope, not the renderer.
func runGain(t *testing.T, args ...string) map[string]any {
	t.Helper()

	cmd := newGainCmd()
	cmd.SetArgs(append(append([]string{}, args...), "--format", "json"))
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("gain %v: %v (output %s)", args, err, out.String())
	}

	var got map[string]any
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("decode gain %v output: %v\n%s", args, err, out.String())
	}
	return got
}

func rows(t *testing.T, report map[string]any, key string) []map[string]any {
	t.Helper()

	raw, ok := report[key]
	if !ok {
		t.Fatalf("report has no %q array: %v", key, report)
	}
	list, ok := raw.([]any)
	if !ok {
		t.Fatalf("report %q is %T, want array", key, raw)
	}
	out := make([]map[string]any, 0, len(list))
	for _, item := range list {
		row, ok := item.(map[string]any)
		if !ok {
			t.Fatalf("report %q row is %T, want object", key, item)
		}
		out = append(out, row)
	}
	return out
}

func num(t *testing.T, row map[string]any, key string) float64 {
	t.Helper()

	v, ok := row[key].(float64)
	if !ok {
		t.Fatalf("row %v has no numeric %q", row, key)
	}
	return v
}

// TestGainSessionHistoryAndRetryFilters covers the spec §12.3 `gum gain`
// reporting modes: --session filters by the ledger session field and emits
// operations[], --history groups by session and op_family into history[],
// --since applies a UTC lower bound, and --exclude-retries drops is_retry
// entries from the reported aggregate without touching the ledger file.
func TestGainSessionHistoryAndRetryFilters(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	path := seedGainLedger(t)

	t.Run("session_mode_filters_by_session", func(t *testing.T) {
		report := runGain(t, "--session", "aaaaaaaa")
		if report["mode"] != "session" {
			t.Errorf("mode = %v; want session", report["mode"])
		}
		if report["window"] != "session:aaaaaaaa" {
			t.Errorf("window = %v; want session:aaaaaaaa", report["window"])
		}
		if _, present := report["history"]; present {
			t.Error("session mode emitted history[]; the schema allows one mode array")
		}
		if _, present := report["sessions"]; present {
			t.Error("session mode emitted sessions[]; the schema allows one mode array")
		}
		// bbbbbbbb's 200 raw tokens must not be in the totals.
		if got := num(t, report, "baseline_tokens"); got != 250 {
			t.Errorf("baseline_tokens = %v; want 250 (session aaaaaaaa only)", got)
		}

		ops := rows(t, report, "operations")
		if len(ops) != 2 {
			t.Fatalf("operations rows = %d; want 2 (one per op_id)", len(ops))
		}
		// Rows sort by op_id, so drive.files.list comes first.
		if ops[0]["op_id"] != "drive.files.list" || ops[1]["op_id"] != "gmail.users.messages.list" {
			t.Fatalf("operations not sorted by op_id: %v", ops)
		}
		gmail := ops[1]
		if got := num(t, gmail, "calls"); got != 2 {
			t.Errorf("gmail calls = %v; want 2", got)
		}
		if got := num(t, gmail, "baseline_tokens"); got != 200 {
			t.Errorf("gmail baseline_tokens = %v; want 200", got)
		}
		if got := num(t, gmail, "actual_tokens"); got != 30 {
			t.Errorf("gmail actual_tokens = %v; want 30", got)
		}
		if gmail["op_family"] != "gmail.users.messages" {
			t.Errorf("gmail op_family = %v", gmail["op_family"])
		}
		if gmail["cache_status"] != "miss" || gmail["field_mask_status"] != "applied" {
			t.Errorf("gmail row lost its grouping keys: %v", gmail)
		}
	})

	t.Run("exclude_retries_drops_only_the_display", func(t *testing.T) {
		report := runGain(t, "--session", "aaaaaaaa", "--exclude-retries")
		ops := rows(t, report, "operations")
		if len(ops) != 1 {
			t.Fatalf("operations rows = %d; want 1 (the retry row is excluded)", len(ops))
		}
		if ops[0]["op_id"] != "gmail.users.messages.list" {
			t.Errorf("surviving row = %v; want the non-retry op", ops[0])
		}
		if got := num(t, report, "baseline_tokens"); got != 200 {
			t.Errorf("baseline_tokens = %v; want 200 (250 minus the 50-token retry)", got)
		}

		// The ledger file itself must still carry the retry entry.
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read ledger: %v", err)
		}
		if !strings.Contains(string(data), `"is_retry":true`) {
			t.Error("--exclude-retries rewrote the ledger; it must change only the report")
		}
	})

	t.Run("history_mode_groups_by_session_and_family", func(t *testing.T) {
		report := runGain(t, "--history")
		if report["mode"] != "history" {
			t.Errorf("mode = %v; want history", report["mode"])
		}
		if report["window"] != "history" {
			t.Errorf("window = %v; want history", report["window"])
		}
		if _, present := report["operations"]; present {
			t.Error("history mode emitted operations[]; the schema allows one mode array")
		}

		hist := rows(t, report, "history")
		if len(hist) != 3 {
			t.Fatalf("history rows = %d; want 3 (session x op_family)", len(hist))
		}
		want := []struct{ session, family string }{
			{"aaaaaaaa", "gmail.users.messages"},
			{"aaaaaaaa", "drive.files"},
			{"bbbbbbbb", "gmail.users.messages"},
		}
		for i, w := range want {
			if hist[i]["session"] != w.session || hist[i]["op_family"] != w.family {
				t.Errorf("history[%d] = (%v, %v); want (%s, %s) in ledger order",
					i, hist[i]["session"], hist[i]["op_family"], w.session, w.family)
			}
		}
		if got := num(t, hist[0], "baseline_tokens"); got != 200 {
			t.Errorf("history[0].baseline_tokens = %v; want 200", got)
		}
		if got := num(t, hist[0], "savings_pct"); got != 85 {
			t.Errorf("history[0].savings_pct = %v; want 85", got)
		}
	})

	t.Run("since_applies_a_lower_bound", func(t *testing.T) {
		report := runGain(t, "--history", "--since", "2026-01-04T00:00:00Z")
		if report["window"] != "since:2026-01-04T00:00:00Z" {
			t.Errorf("window = %v; want the --since label", report["window"])
		}
		hist := rows(t, report, "history")
		if len(hist) != 2 {
			t.Fatalf("history rows = %d; want 2 (the two entries at or after the bound)", len(hist))
		}
		if got := num(t, report, "baseline_tokens"); got != 250 {
			t.Errorf("baseline_tokens = %v; want 250", got)
		}
	})

	t.Run("session_and_history_are_exclusive", func(t *testing.T) {
		cmd := newGainCmd()
		cmd.SetArgs([]string{"--session", "aaaaaaaa", "--history"})
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&out)
		err := cmd.Execute()
		if err == nil {
			t.Fatal("expected an error when both mode flags are passed")
		}
		if !strings.Contains(err.Error(), "--session") || !strings.Contains(err.Error(), "--history") {
			t.Errorf("err = %q; want both flag names", err)
		}
	})
}

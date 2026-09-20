package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/config"
	"github.com/ehmo/gum/internal/dispatch"
	"github.com/ehmo/gum/internal/output/gain"
)

// The gain ledger is the only evidence `gum gain` and the `gum.gain` tool
// report from, and spec §12.3 says every dispatch appends an entry to it.
// Nothing wired it: DispatcherConfig.Ledger was never set outside tests, so
// the ledger file was only ever created by the two readers and was always
// empty. These tests pin the wiring at the constructor.

func TestDefaultDispatcherWiresGainLedger(t *testing.T) {
	dataHome := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dataHome)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	disp, closer := newDefaultDispatcherWithCloser("default", false)
	if disp == nil {
		t.Fatal("dispatcher is nil")
	}
	defer func() { _ = closer() }()

	path := filepath.Join(dataHome, "gum", "default", gain.LedgerFileName)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("gain ledger not opened at %s: %v", path, err)
	}
}

func TestGainLedgerHonorsProfileDataDir(t *testing.T) {
	dataHome := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dataHome)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	_, closer := newDefaultDispatcherWithCloser("team-a", false)
	defer func() { _ = closer() }()

	path := filepath.Join(dataHome, "gum", "team-a", gain.LedgerFileName)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("profile ledger not opened at %s: %v", path, err)
	}
}

// gain.enabled=false is the spec §12.3 opt-out. It disables all usage logging
// for the profile, so no ledger file may appear.
func TestGainDisabledSkipsLedger(t *testing.T) {
	dataHome := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dataHome)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := config.Save("default", &config.Config{Values: map[string]string{"gain.enabled": "false"}}); err != nil {
		t.Fatalf("save config: %v", err)
	}

	_, closer := newDefaultDispatcherWithCloser("default", false)
	defer func() { _ = closer() }()

	path := filepath.Join(dataHome, "gum", "default", gain.LedgerFileName)
	if _, err := os.Stat(path); err == nil {
		t.Fatalf("gain.enabled=false still created %s", path)
	}
}

// TestGumGainReportsRealDispatch is the end-to-end proof: dispatch one call,
// then read the ledger back the way `gum gain` reads it. Before the wiring
// this reported total_calls=0 no matter how much traffic the profile served.
func TestGumGainReportsRealDispatch(t *testing.T) {
	dataHome := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dataHome)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("GUM_GAIN_DISABLED", "")

	disp, closer := newDefaultDispatcherWithCloser("default", false)
	// gum.code has auth_strategy=none, so it dispatches with no credentials
	// and no network.
	_, err := disp.Dispatch(context.Background(), &dispatch.Invocation{
		OpID: "gum.code",
		Args: map[string]any{"source": `1 + 1`, "language": "risor"},
	})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if cerr := closer(); cerr != nil {
		t.Fatalf("closer: %v", cerr)
	}

	path := filepath.Join(dataHome, "gum", "default", gain.LedgerFileName)
	ledger, err := gain.NewLedger(path)
	if err != nil {
		t.Fatalf("reopen ledger: %v", err)
	}
	t.Cleanup(func() { _ = ledger.Close() })

	stats := ledger.Stats()
	if stats.TotalCalls != 1 {
		t.Fatalf("Stats().TotalCalls=%d want 1; ledger at %s", stats.TotalCalls, path)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read ledger: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) != 2 {
		t.Fatalf("ledger has %d lines want 2 (header + 1 entry):\n%s", len(lines), raw)
	}
	var entry map[string]any
	if err := json.Unmarshal([]byte(lines[1]), &entry); err != nil {
		t.Fatalf("entry is not JSON: %v", err)
	}
	for _, key := range []string{
		"record_type", "session", "op_id", "op_family", "variant_id", "output_profile",
		"args_hash", "auth_subject_fingerprint", "request_tokens", "response_tokens",
		"raw_tokens", "shaped_tokens", "cache_status", "field_mask_status",
		"served_from_cache", "is_retry", "baseline_method",
	} {
		if _, ok := entry[key]; !ok {
			t.Errorf("§12.3 entry is missing required key %q: %s", key, lines[1])
		}
	}
	if entry["op_id"] != "gum.code" {
		t.Errorf("op_id=%v want gum.code", entry["op_id"])
	}
	if entry["record_type"] != "entry" {
		t.Errorf("record_type=%v want entry", entry["record_type"])
	}
}

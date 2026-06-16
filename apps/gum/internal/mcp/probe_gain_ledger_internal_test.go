package mcp

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestProbeGainLedgerNoEntriesYet pins the "ledger absent" branch:
// a fresh install with no gain-ledger.jsonl reports healthy with the
// "no entries yet" detail, NOT degraded. This is the cold-start case
// every CI machine hits on the first run.
func TestProbeGainLedgerNoEntriesYet(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	res := probeGainLedger(time.Unix(1700000000, 0).UTC(), "")
	if res.Subsystem != "gain_ledger" {
		t.Errorf("Subsystem=%q; want gain_ledger", res.Subsystem)
	}
	if res.Status != "healthy" {
		t.Errorf("Status=%q; want healthy on cold start", res.Status)
	}
	if res.Detail != "no entries yet" {
		t.Errorf("Detail=%q; want 'no entries yet'", res.Detail)
	}
}

// TestProbeGainLedgerPresentReportsHealthy pins the "ledger present"
// branch: when the gain-ledger.jsonl file exists, the probe reports
// healthy with the "ledger present" detail.
func TestProbeGainLedgerPresentReportsHealthy(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".local", "share", "gum")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "gain-ledger.jsonl"), []byte(`{}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	res := probeGainLedger(time.Unix(1700000000, 0).UTC(), "")
	if res.Status != "healthy" || res.Detail != "ledger present" {
		t.Errorf("got Status=%q Detail=%q; want healthy/'ledger present'", res.Status, res.Detail)
	}
}

// TestProbeGainLedgerDirInsteadOfFileTreatedAsEmpty pins the branch where
// the ledger path resolves to a directory: the probe treats this like an
// absent file (not a "present" ledger) and falls through to "no entries
// yet".
func TestProbeGainLedgerDirInsteadOfFileTreatedAsEmpty(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".local", "share", "gum", "gain-ledger.jsonl")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	res := probeGainLedger(time.Unix(1700000000, 0).UTC(), "")
	if res.Status != "healthy" || res.Detail != "no entries yet" {
		t.Errorf("got Status=%q Detail=%q; want healthy/'no entries yet'", res.Status, res.Detail)
	}
}

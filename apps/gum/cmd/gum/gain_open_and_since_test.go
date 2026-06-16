package main

import (
	"bytes"
	"strings"
	"testing"
)

// TestGainOpenLedgerErrorWrapsPrefix pins gain.go:54-56 — when
// NewLedger("") fails because os.UserHomeDir can't resolve any home
// (HOME/USER/LOGNAME all empty on Unix), the gain command MUST wrap
// the err with "open ledger:" so operators can grep this entry point
// distinctly from the parseGainTime / fixture-replay arms.
func TestGainOpenLedgerErrorWrapsPrefix(t *testing.T) {
	t.Setenv("HOME", "")
	t.Setenv("USER", "")
	t.Setenv("LOGNAME", "")

	root := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"gain"})

	err := root.Execute()
	if err == nil {
		t.Fatal("gain with empty HOME succeeded; want open-ledger wrap")
	}
	if !strings.Contains(err.Error(), "open ledger:") {
		t.Errorf("err=%q; want 'open ledger:' prefix", err)
	}
}

// TestGainSinceFlagDrivesStatsBetweenBranch pins gain.go:63 — the
// `enc.Encode(ledger.StatsBetween(since, until))` arm. Without a
// --since/--until value the command takes the `Stats()` branch at
// line 61; supplying --since with a valid RFC3339 timestamp routes
// to StatsBetween instead.
func TestGainSinceFlagDrivesStatsBetweenBranch(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	root := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"gain", "--since", "2024-01-01T00:00:00Z"})

	if err := root.Execute(); err != nil {
		t.Fatalf("gain --since: %v", err)
	}
	// Sanity: output must be a JSON object. We don't pin specific fields
	// because the empty-ledger StatsBetween envelope is identical-shape
	// to Stats() — coverage is what differs.
	if !strings.HasPrefix(strings.TrimSpace(out.String()), "{") {
		t.Errorf("out=%q; want JSON object", out.String())
	}
}

package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// TestNewGainCmdShape pins the cobra surface: all five flags must exist
// with the documented defaults (format=toon; the booleans default false;
// since/until empty). NoArgs is implicit (cobra accepts any unless we
// constrain — we don't, but the Use line names no positional).
func TestNewGainCmdShape(t *testing.T) {
	cmd := newGainCmd()
	if cmd.Use != "gain" {
		t.Errorf("Use=%q", cmd.Use)
	}
	for _, want := range []struct{ name, def string }{
		{"by-op", "false"},
		{"fixture-replay", "false"},
		{"format", "toon"},
		{"since", ""},
		{"until", ""},
	} {
		f := cmd.Flags().Lookup(want.name)
		if f == nil {
			t.Errorf("flag %q missing", want.name)
			continue
		}
		if f.DefValue != want.def {
			t.Errorf("flag %q default=%q; want %q", want.name, f.DefValue, want.def)
		}
	}
}

// TestNewGainCmdSinceInvalidPropagates exercises the RunE since-parsing
// branch: an invalid timestamp must surface the "--since" flag-prefixed
// error from parseGainTime, never opening the ledger.
func TestNewGainCmdSinceInvalidPropagates(t *testing.T) {
	cmd := newGainCmd()
	cmd.SetArgs([]string{"--since", "not-a-date"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for invalid --since")
	}
	if !strings.Contains(err.Error(), "--since") {
		t.Errorf("err=%q; want --since hint", err)
	}
}

// TestNewGainCmdUntilInvalidPropagates exercises the symmetric branch
// for --until. Both flags share the parser but each carries its own
// error prefix, so the user can tell which one misparsed.
func TestNewGainCmdUntilInvalidPropagates(t *testing.T) {
	cmd := newGainCmd()
	cmd.SetArgs([]string{"--until", "garbage"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for invalid --until")
	}
	if !strings.Contains(err.Error(), "--until") {
		t.Errorf("err=%q; want --until hint", err)
	}
}

// TestNewGainCmdHappyPath drives the ledger-stats branch end-to-end:
// with no flags the command must open the user's ledger (we redirect
// HOME/XDG_DATA_HOME to a tempdir so we never touch the real one) and
// print a JSON-encoded Stats object on stdout.
func TestNewGainCmdHappyPath(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())

	cmd := newGainCmd()
	cmd.SetArgs(nil)
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var stats map[string]any
	if err := json.Unmarshal(out.Bytes(), &stats); err != nil {
		t.Fatalf("Unmarshal: %v\nstdout=%s", err, out.String())
	}
	if len(stats) == 0 {
		t.Errorf("empty stats object; got %s", out.String())
	}
}

// TestNewGainCmdFixtureReplay drives the --fixture-replay branch, which
// short-circuits ledger reading and instead replays the embedded
// testdata/fixtures/gain-replay set. The output must be JSON-parseable
// and carry a non-zero number of fixture results — otherwise the gate
// would silently pass even when the replay registry was empty.
func TestNewGainCmdFixtureReplay(t *testing.T) {
	cmd := newGainCmd()
	cmd.SetArgs([]string{"--fixture-replay"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v\nout=%s", err, out.String())
	}
	var result map[string]any
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("not JSON: %v\nout=%s", err, out.String())
	}
	if len(result) == 0 {
		t.Errorf("empty replay result: %s", out.String())
	}
	_ = strings.Contains // import preserved
}

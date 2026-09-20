package main

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/output/gain"
)

// runGainArms runs `gum gain` through the root command so the --profile
// persistent flag is in scope, and returns the combined output plus the
// RunE error.
func runGainArms(t *testing.T, args ...string) (string, error) {
	t.Helper()
	root := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetIn(strings.NewReader(""))
	root.SetArgs(append([]string{"gain"}, args...))
	err := root.Execute()
	return out.String(), err
}

// TestGainRejectsABadProfile pins the resolveProfileName arm inside RunE.
// The real root rejects an unusable --profile in PersistentPreRunE, so RunE
// never sees one there; the arm is the guard for any root that skips that
// hook. A bare root carrying only the flag is what reaches it.
func TestGainRejectsABadProfile(t *testing.T) {
	isolatedHome(t)

	root := bareRootWithProfile("bad/name")
	root.PersistentFlags().Lookup("profile").Changed = true
	root.AddCommand(newGainCmd())
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetIn(strings.NewReader(""))
	root.SetArgs([]string{"gain"})

	err := root.Execute()
	if err == nil {
		t.Fatal("gain with a bad profile succeeded; want a rejection")
	}
	if !strings.Contains(err.Error(), "bad/name") {
		t.Errorf("err=%q does not name the rejected profile", err)
	}
}

// TestGainSurfacesALedgerOpenFailure pins the gain.NewLedger arm, which is
// distinct from the gain.DefaultPath arm the empty-HOME test drives. A
// directory sitting where the ledger file belongs makes the O_WRONLY open
// fail after the path resolved cleanly.
func TestGainSurfacesALedgerOpenFailure(t *testing.T) {
	dataHome := isolatedHome(t)

	ledgerDir := filepath.Join(dataHome, "gum", "default", gain.LedgerFileName)
	if err := os.MkdirAll(ledgerDir, 0o700); err != nil {
		t.Fatalf("mkdir %s: %v", ledgerDir, err)
	}

	_, err := runGainArms(t)
	if err == nil {
		t.Fatal("gain over a directory-shaped ledger succeeded; want an open failure")
	}
	if !strings.Contains(err.Error(), "open ledger:") {
		t.Errorf("err=%q; want the 'open ledger:' wrap", err)
	}
}

// TestGainByOpEncodesPerOpStats pins the --by-op branch. Without the flag
// the command takes Stats() or StatsBetween(); --by-op has to route to
// StatsByOp instead.
func TestGainByOpEncodesPerOpStats(t *testing.T) {
	dataHome := isolatedHome(t)

	path := filepath.Join(dataHome, "gum", "default", gain.LedgerFileName)
	ledger, err := gain.NewLedger(path)
	if err != nil {
		t.Fatalf("new ledger: %v", err)
	}
	if err := ledger.Append(gain.Entry{OpID: "gmail.messages.list", RawTokens: 100, ShapedTokens: 40}); err != nil {
		t.Fatalf("append: %v", err)
	}
	if err := ledger.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	out, err := runGainArms(t, "--by-op")
	if err != nil {
		t.Fatalf("gain --by-op: %v (out=%q)", err, out)
	}
	if !strings.Contains(out, "gmail.messages.list") {
		t.Errorf("out=%q does not carry the per-op key", out)
	}
}

// TestGainFixtureReplaySurfacesAReadFailure pins the fixture-replay error
// wrap. The seam is making one fixture unreadable, so the test copies the
// repository's fixture set into a private directory first and swaps the
// resolver. Chmod-ing the tracked file instead made every concurrent reader
// in internal/output/gain fail under `go test ./...`.
func TestGainFixtureReplaySurfacesAReadFailure(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores the read bit")
	}

	dir := copyFixtureReplaySet(t)
	swapFixtureReplayDir(t, dir)

	victim := filepath.Join(dir, "gmail-single-message", "response.json")
	if err := os.Chmod(victim, 0o000); err != nil {
		t.Fatalf("chmod %s: %v", victim, err)
	}
	t.Cleanup(func() { _ = os.Chmod(victim, 0o600) })

	_, err := runGainArms(t, "--fixture-replay")
	if err == nil {
		t.Fatal("fixture replay over an unreadable fixture succeeded; want an error")
	}
	if !strings.Contains(err.Error(), "fixture replay:") {
		t.Errorf("err=%q; want the 'fixture replay:' wrap", err)
	}
}

// swapFixtureReplayDir points defaultFixtureReplayDir at dir for one test.
func swapFixtureReplayDir(t *testing.T, dir string) {
	t.Helper()
	prev := defaultFixtureReplayDir
	defaultFixtureReplayDir = func() string { return dir }
	t.Cleanup(func() { defaultFixtureReplayDir = prev })
}

// copyFixtureReplaySet copies the repository's gain-replay fixtures into a
// temporary directory and returns it. The copy is writable, so a test may
// change a mode there without touching tracked state.
func copyFixtureReplaySet(t *testing.T) string {
	t.Helper()
	src := defaultFixtureReplayDir()
	dst := t.TempDir()
	err := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, b, 0o600)
	})
	if err != nil {
		t.Fatalf("copy fixture set from %s: %v", src, err)
	}
	return dst
}

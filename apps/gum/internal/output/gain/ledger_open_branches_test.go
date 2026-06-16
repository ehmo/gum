package gain_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/output/gain"
)

// TestNewLedgerHomeUnavailableReturnsError pins NewLedger's
// `os.UserHomeDir err → return err` arm (ledger.go:297-300). Reached
// when path is "" AND HOME is unset — the caller MUST see a non-nil
// error rather than silently writing to a CWD-relative path.
//
// Skipped on Windows: UserHomeDir uses USERPROFILE, not HOME.
func TestNewLedgerHomeUnavailableReturnsError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("UserHomeDir on Windows uses USERPROFILE, not HOME")
	}
	t.Setenv("HOME", "")
	t.Setenv("XDG_DATA_HOME", "")

	_, err := gain.NewLedger("") // empty path triggers UserHomeDir
	if err == nil {
		t.Fatalf("NewLedger(\"\", no HOME) err=nil; want UserHomeDir err")
	}
	if !strings.Contains(err.Error(), "home dir") {
		t.Errorf("err=%v; want 'home dir' wrap", err)
	}
}

// TestNewLedgerOpenFailureWrapsOpenLedger pins NewLedger's
// `OpenFile err → return 'open ledger' wrap` arm (ledger.go:334-337).
// Reached when the parent directory exists but the ledger path itself
// is a directory (EISDIR on OpenFile). The wrap names "open ledger" so
// operators can locate the failure.
func TestNewLedgerOpenFailureWrapsOpenLedger(t *testing.T) {
	dir := t.TempDir()
	ledgerPath := filepath.Join(dir, "ledger.jsonl")
	// Plant a directory at the ledger path — OpenFile rejects.
	if err := os.MkdirAll(ledgerPath, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	_, err := gain.NewLedger(ledgerPath)
	if err == nil {
		t.Fatalf("NewLedger(dir-as-file) err=nil; want OpenFile err")
	}
	if !strings.Contains(err.Error(), "open ledger") {
		t.Errorf("err=%v; want 'open ledger' wrap", err)
	}
}

// TestNewLedgerSkipsEmptyLinesInExistingFile pins NewLedger's
// `len(line) == 0 → continue` arm (ledger.go:313-314). A pre-existing
// ledger file may contain blank lines from interrupted writes; the
// scanner MUST skip them rather than json.Unmarshal-erroring out and
// losing the entries that follow.
func TestNewLedgerSkipsEmptyLinesInExistingFile(t *testing.T) {
	dir := t.TempDir()
	ledgerPath := filepath.Join(dir, "ledger.jsonl")
	// Plant a file with blank lines interleaved with one valid entry.
	// TotalTokensSaved = raw_tokens - shaped_tokens (per spec §12.3).
	body := "\n\n" + // empty lines at start
		`{"record_type":"entry","op_id":"test.op","raw_tokens":100,"shaped_tokens":58}` + "\n" +
		"\n" // trailing blank
	if err := os.WriteFile(ledgerPath, []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	l, err := gain.NewLedger(ledgerPath)
	if err != nil {
		t.Fatalf("NewLedger: %v", err)
	}
	t.Cleanup(func() { _ = l.Close() })

	// Sanity check: the single valid entry was loaded.
	stats := l.Stats()
	if stats.TotalTokensSaved != 42 {
		t.Errorf("TotalTokensSaved=%d; want 42 (100-58, blank lines must not interfere)", stats.TotalTokensSaved)
	}
}

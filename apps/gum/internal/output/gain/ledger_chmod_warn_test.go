package gain_test

import (
	"os"
	"testing"

	"github.com/ehmo/gum/internal/output/gain"
)

// TestNewLedgerSurvivesAChmodItCannotApply pins ledger.go:412-414 — the
// `f.Chmod err → slog.Warn` arm. NewLedger tightens the mode to 0o600 on
// every open so a pre-0.1.0 0o644 ledger stops being world-readable, but a
// chmod it cannot apply must not fail the open: refusing to record gain is
// worse than a permission gum could not narrow.
//
// /dev/null is the seam. It is openable and appendable by any account, root
// owns it, and fchmod on a file you do not own returns EPERM. The header
// write that follows lands in the void, so nothing on disk changes.
func TestNewLedgerSurvivesAChmodItCannotApply(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can chmod /dev/null, which would change a system device node")
	}

	l, err := gain.NewLedger(os.DevNull)
	if err != nil {
		t.Fatalf("NewLedger(%s) = %v; want the chmod failure to be a warning, not an error", os.DevNull, err)
	}
	defer func() { _ = l.Close() }()

	info, err := os.Stat(os.DevNull)
	if err != nil {
		t.Fatalf("stat %s: %v", os.DevNull, err)
	}
	if info.Mode().Perm() == 0o600 {
		t.Errorf("%s is now mode 0600; NewLedger must not have narrowed a device node", os.DevNull)
	}
}

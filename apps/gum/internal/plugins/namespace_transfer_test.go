// gum-bc2 acceptance: spec §5.1.3 namespace-transfer mutator. The CLI
// (cmd/gum/plugin.go transfer-namespace subcommand) builds on these pure
// functions and adds the --yes consent gate plus the audit-log emit; this
// file pins the in-memory mutation invariants so a regression surfaces
// without spinning up a registry transaction.

package plugins_test

import (
	"errors"
	"testing"
	"time"

	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/plugins"
)

const transferTimestamp = "2026-05-24T12:00:00Z"

func fixedClockOpts() plugins.TransferOptions {
	ts, _ := time.Parse(time.RFC3339, transferTimestamp)
	return plugins.TransferOptions{Now: func() time.Time { return ts }}
}

// TestTransferNamespaceUpdatesOwnerAndHistory pins the §5.1.3 happy path:
// the namespace_owner key flips to the new owner, transfer_history gets one
// row with the prior owner, and the lock row is otherwise unchanged.
func TestTransferNamespaceUpdatesOwnerAndHistory(t *testing.T) {
	t.Parallel()
	lock := &catalog.PluginsLock{PluginsLockSchemaVersion: 1}
	plugins.RecordNamespaceOwner(lock, "fli", "io.example.flights")

	oldOwner, err := plugins.TransferNamespace(lock, "fli", "io.example2.flights", fixedClockOpts())
	if err != nil {
		t.Fatalf("TransferNamespace: %v", err)
	}
	if oldOwner != "io.example.flights" {
		t.Errorf("oldOwner=%q; want io.example.flights", oldOwner)
	}
	if got := plugins.NamespaceOwnerForTest(lock, "fli"); got != "io.example2.flights" {
		t.Errorf("namespace_owner=%q; want io.example2.flights", got)
	}
	history := plugins.TransferHistoryForTest(lock, "fli")
	if len(history) != 1 {
		t.Fatalf("transfer_history len=%d; want 1", len(history))
	}
	row := history[0]
	if row["from"] != "io.example.flights" {
		t.Errorf("history.from=%v; want io.example.flights", row["from"])
	}
	if row["to"] != "io.example2.flights" {
		t.Errorf("history.to=%v; want io.example2.flights", row["to"])
	}
	if row["at"] != transferTimestamp {
		t.Errorf("history.at=%v; want %s", row["at"], transferTimestamp)
	}
}

// TestTransferNamespaceMissingPrefix asserts ErrPrefixNotInLock fires when
// the requested prefix has never been recorded.
func TestTransferNamespaceMissingPrefix(t *testing.T) {
	t.Parallel()
	lock := &catalog.PluginsLock{PluginsLockSchemaVersion: 1}
	_, err := plugins.TransferNamespace(lock, "unknown", "io.example.new", fixedClockOpts())
	if !errors.Is(err, plugins.ErrPrefixNotInLock) {
		t.Fatalf("err=%v; want ErrPrefixNotInLock", err)
	}
}

// TestTransferNamespaceEmptyNewOwnerRejected guards the CLI contract: the
// --new-owner flag is mandatory for transfer; the unbind path goes through
// --release / ReleaseNamespace which records `to=""`.
func TestTransferNamespaceEmptyNewOwnerRejected(t *testing.T) {
	t.Parallel()
	lock := &catalog.PluginsLock{PluginsLockSchemaVersion: 1}
	plugins.RecordNamespaceOwner(lock, "fli", "io.example.flights")
	_, err := plugins.TransferNamespace(lock, "fli", "", fixedClockOpts())
	if err == nil {
		t.Fatal("TransferNamespace with empty newOwner returned nil; want error pointing at --release")
	}
}

// TestReleaseNamespaceClearsBindingAndAppendsHistory covers the second
// half of §5.1.3 line 526: the namespace_owner key is removed but the row
// is preserved with a transfer_history entry recording the release.
func TestReleaseNamespaceClearsBindingAndAppendsHistory(t *testing.T) {
	t.Parallel()
	lock := &catalog.PluginsLock{PluginsLockSchemaVersion: 1}
	plugins.RecordNamespaceOwner(lock, "fli", "io.example.flights")

	oldOwner, err := plugins.ReleaseNamespace(lock, "fli", fixedClockOpts())
	if err != nil {
		t.Fatalf("ReleaseNamespace: %v", err)
	}
	if oldOwner != "io.example.flights" {
		t.Errorf("oldOwner=%q; want io.example.flights", oldOwner)
	}
	if got := plugins.NamespaceOwnerForTest(lock, "fli"); got != "" {
		t.Errorf("namespace_owner=%q after release; want empty", got)
	}
	if _, found := plugins.LookupNamespaceOwner(lock, "fli"); found {
		t.Errorf("LookupNamespaceOwner returned found=true after release; want false")
	}
	history := plugins.TransferHistoryForTest(lock, "fli")
	if len(history) != 1 {
		t.Fatalf("transfer_history len=%d after release; want 1", len(history))
	}
	if history[0]["to"] != "" {
		t.Errorf("release history.to=%v; want empty", history[0]["to"])
	}
}

// TestTransferNamespacePreservesPriorHistory ensures consecutive transfers
// accumulate history entries rather than overwriting them.
func TestTransferNamespacePreservesPriorHistory(t *testing.T) {
	t.Parallel()
	lock := &catalog.PluginsLock{PluginsLockSchemaVersion: 1}
	plugins.RecordNamespaceOwner(lock, "fli", "owner-a")
	if _, err := plugins.TransferNamespace(lock, "fli", "owner-b", fixedClockOpts()); err != nil {
		t.Fatalf("first transfer: %v", err)
	}
	if _, err := plugins.TransferNamespace(lock, "fli", "owner-c", fixedClockOpts()); err != nil {
		t.Fatalf("second transfer: %v", err)
	}
	history := plugins.TransferHistoryForTest(lock, "fli")
	if len(history) != 2 {
		t.Fatalf("transfer_history len=%d; want 2 after two transfers", len(history))
	}
	if history[0]["from"] != "owner-a" || history[0]["to"] != "owner-b" {
		t.Errorf("history[0]=%v; want from=owner-a, to=owner-b", history[0])
	}
	if history[1]["from"] != "owner-b" || history[1]["to"] != "owner-c" {
		t.Errorf("history[1]=%v; want from=owner-b, to=owner-c", history[1])
	}
}

// TestTransferNamespaceRejectsUnboundPrefix covers the edge case where a
// row exists for the prefix but no namespace_owner is set (e.g., released
// in a prior transaction). The transfer command must error rather than
// silently re-binding.
func TestTransferNamespaceRejectsUnboundPrefix(t *testing.T) {
	t.Parallel()
	lock := &catalog.PluginsLock{PluginsLockSchemaVersion: 1}
	plugins.RecordNamespaceOwner(lock, "fli", "owner-a")
	if _, err := plugins.ReleaseNamespace(lock, "fli", fixedClockOpts()); err != nil {
		t.Fatalf("release: %v", err)
	}
	_, err := plugins.TransferNamespace(lock, "fli", "owner-c", fixedClockOpts())
	if !errors.Is(err, plugins.ErrPrefixNotInLock) {
		t.Fatalf("err=%v; want ErrPrefixNotInLock for prefix with empty owner", err)
	}
}

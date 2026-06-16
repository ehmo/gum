// Spec §5.1.3 normative namespace-transfer logic. The CLI surface (`gum
// plugin transfer-namespace <prefix> --new-owner <name> --yes` and the
// `--release` variant) lives in cmd/gum/plugin.go; this file owns the pure
// in-memory mutations on plugins.lock so unit tests can pin the contract
// without spinning up a registry transaction.
//
// Semantics (spec §5.1 line 526):
//   1. The prefix MUST currently be bound to an owner in plugins.lock.
//   2. The new owner MUST be non-empty (Transfer) or omitted (Release).
//   3. The mutation updates `namespace_owner` in place; the previous owner
//      is appended to `transfer_history` as a {from, to, at} triple so the
//      audit trail stays in-band with the lock row.
//   4. Release clears the namespace_owner key entirely (and records the
//      release as a transfer_history entry with `to=""`).
//   5. The caller (CLI) is responsible for the --yes consent gate and the
//      audit-log emit.

package plugins

import (
	"errors"
	"fmt"
	"time"

	"github.com/ehmo/gum/internal/catalog"
)

// ErrPrefixNotInLock fires when transfer-namespace targets a prefix that is
// not currently bound in the active profile's plugins.lock. The CLI surfaces
// this as an exit-code error; the operator's recovery is to verify the
// prefix string or to run `gum plugin list` first.
var ErrPrefixNotInLock = errors.New("PLUGIN_PREFIX_NOT_BOUND")

// TransferOptions carries the clock and audit hook so tests can substitute
// deterministic values. Zero-valued Now defaults to time.Now; a nil
// AuditAppend leaves auditing to the caller.
type TransferOptions struct {
	Now func() time.Time
}

// TransferNamespace updates the namespace_owner row for prefix in lock and
// appends a transfer_history entry. Returns the prior owner so the CLI can
// surface it in stdout + the audit envelope. Empty newOwner is rejected;
// use ReleaseNamespace for the unbind path.
func TransferNamespace(lock *catalog.PluginsLock, prefix, newOwner string, opts TransferOptions) (string, error) {
	if newOwner == "" {
		return "", fmt.Errorf("transfer-namespace: --new-owner must be non-empty (use --release to clear the binding)")
	}
	return mutateNamespaceRow(lock, prefix, newOwner, opts.now())
}

// ReleaseNamespace clears the namespace_owner entry for prefix while
// preserving the transfer_history audit trail. Subsequent installers of any
// owner succeed without --dev-allow-namespace-conflict.
func ReleaseNamespace(lock *catalog.PluginsLock, prefix string, opts TransferOptions) (string, error) {
	return mutateNamespaceRow(lock, prefix, "", opts.now())
}

// mutateNamespaceRow is the shared engine for Transfer and Release. A new
// owner string of "" signals release semantics: the namespace_owner key is
// removed and the transfer_history row records `to=""`.
func mutateNamespaceRow(lock *catalog.PluginsLock, prefix, newOwner string, at time.Time) (string, error) {
	if lock == nil {
		return "", fmt.Errorf("%w: nil plugins.lock", ErrPrefixNotInLock)
	}
	if prefix == "" {
		return "", fmt.Errorf("%w: empty prefix", ErrPrefixNotInLock)
	}
	for i, raw := range lock.Plugins {
		row, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		got, _ := row["prefix"].(string)
		if got != prefix {
			continue
		}
		oldOwner, _ := row["namespace_owner"].(string)
		if oldOwner == "" {
			return "", fmt.Errorf("%w: prefix %q has no current namespace_owner in plugins.lock", ErrPrefixNotInLock, prefix)
		}
		historyEntry := map[string]any{
			"from": oldOwner,
			"to":   newOwner,
			"at":   at.UTC().Format(time.RFC3339),
		}
		row["transfer_history"] = appendTransferHistory(row["transfer_history"], historyEntry)
		if newOwner == "" {
			delete(row, "namespace_owner")
		} else {
			row["namespace_owner"] = newOwner
		}
		lock.Plugins[i] = row
		return oldOwner, nil
	}
	return "", fmt.Errorf("%w: prefix %q is not bound in the active profile's plugins.lock", ErrPrefixNotInLock, prefix)
}

// appendTransferHistory normalises an existing transfer_history value (nil
// or any-typed list) into a typed []any and appends entry. Non-list values
// are discarded — the alternative (panicking) would corrupt the lock row on
// recovery from a hand-edited file.
func appendTransferHistory(existing any, entry map[string]any) []any {
	if list, ok := existing.([]any); ok {
		return append(list, entry)
	}
	return []any{entry}
}

// now returns Now() or time.Now() when Now is nil.
func (o TransferOptions) now() time.Time {
	if o.Now == nil {
		return time.Now()
	}
	return o.Now()
}

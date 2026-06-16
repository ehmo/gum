package main_test

// CLI-level coverage of the spec §5.1.3 transfer-namespace subcommand. The
// pure-logic invariants live in internal/plugins/namespace_transfer_test.go;
// this file pins the wiring: flag parsing, --yes consent gate, registry
// commit, and the audit.jsonl row.

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	gummain "github.com/ehmo/gum/cmd/gum"
	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/plugins"
	"github.com/ehmo/gum/internal/plugins/registry"
)

// readTransferHistory reads the transfer_history list off the plugins.lock
// row for prefix. Tests in cmd/gum can't import plugins._test helpers, so we
// reach into the raw map shape here.
func readTransferHistory(lock *catalog.PluginsLock, prefix string) []map[string]any {
	if lock == nil {
		return nil
	}
	for _, raw := range lock.Plugins {
		row, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if got, _ := row["prefix"].(string); got != prefix {
			continue
		}
		hist, ok := row["transfer_history"].([]any)
		if !ok {
			return nil
		}
		out := make([]map[string]any, 0, len(hist))
		for _, h := range hist {
			if m, ok := h.(map[string]any); ok {
				out = append(out, m)
			}
		}
		return out
	}
	return nil
}

// seedNamespaceOwner pre-records prefix→owner in plugins.lock under
// reg.ProfileDir() so a subsequent transfer/release has something to mutate.
func seedNamespaceOwner(t *testing.T, reg *registry.Registry, prefix, owner string) {
	t.Helper()
	if err := reg.WriteTransaction(t.Context(), func(files *registry.Files) error {
		plugins.RecordNamespaceOwner(files.Lock, prefix, owner)
		return nil
	}); err != nil {
		t.Fatalf("seedNamespaceOwner: %v", err)
	}
}

// readAuditEntries returns every JSON object written to audit.jsonl under
// profileDir. Empty slice if the file is absent.
func readAuditEntries(t *testing.T, profileDir string) []map[string]any {
	t.Helper()
	path := filepath.Join(profileDir, "audit.jsonl")
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatalf("open audit.jsonl: %v", err)
	}
	defer func() { _ = f.Close() }()
	var out []map[string]any
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var row map[string]any
		if err := json.Unmarshal(line, &row); err != nil {
			t.Fatalf("audit.jsonl row %q is not valid JSON: %v", string(line), err)
		}
		out = append(out, row)
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan audit.jsonl: %v", err)
	}
	return out
}

// TestTransferNamespaceSubcommandHappyPath drives `gum plugin
// transfer-namespace fli --new-owner io.example2.flights --yes` end-to-end
// against a real registry, then asserts (a) success message names the old +
// new owner, (b) plugins.lock now resolves to the new owner, and (c) one
// audit.jsonl row carries the spec §5.1.3 transfer event.
func TestTransferNamespaceSubcommandHappyPath(t *testing.T) {
	reg := registry.New(t.TempDir())
	seedNamespaceOwner(t, reg, "fli", "io.example.flights")

	args := []string{"transfer-namespace", "fli", "--new-owner", "io.example2.flights", "--yes"}
	result, err := gummain.DispatchPluginCommandWithRegistry(args, &mockHost{}, reg.ProfileDir(), func(string) *registry.Registry { return reg })
	if err != nil {
		t.Fatalf("DispatchPluginCommandWithRegistry: %v", err)
	}
	if !strings.Contains(result, `transferred namespace "fli" from "io.example.flights" to "io.example2.flights"`) {
		t.Errorf("result = %q; want contains transfer description with both owners", result)
	}

	files, err := reg.Load()
	if err != nil {
		t.Fatalf("reg.Load: %v", err)
	}
	if got, found := plugins.LookupNamespaceOwner(files.Lock, "fli"); !found || got != "io.example2.flights" {
		t.Errorf("post-transfer LookupNamespaceOwner=%q,found=%v; want io.example2.flights,true", got, found)
	}
	history := readTransferHistory(files.Lock, "fli")
	if len(history) != 1 {
		t.Fatalf("transfer_history len=%d; want 1", len(history))
	}
	if history[0]["from"] != "io.example.flights" || history[0]["to"] != "io.example2.flights" {
		t.Errorf("transfer_history[0]=%v; want from=io.example.flights,to=io.example2.flights", history[0])
	}

	rows := readAuditEntries(t, reg.ProfileDir())
	if len(rows) != 1 {
		t.Fatalf("audit.jsonl rows=%d; want 1", len(rows))
	}
	row := rows[0]
	if row["event_type"] != "plugin_namespace_transfer" {
		t.Errorf("audit event_type=%v; want plugin_namespace_transfer", row["event_type"])
	}
	if row["prefix"] != "fli" {
		t.Errorf("audit prefix=%v; want fli", row["prefix"])
	}
	if row["old_owner"] != "io.example.flights" {
		t.Errorf("audit old_owner=%v; want io.example.flights", row["old_owner"])
	}
	if row["new_owner"] != "io.example2.flights" {
		t.Errorf("audit new_owner=%v; want io.example2.flights", row["new_owner"])
	}
	if released, _ := row["released"].(bool); released {
		t.Errorf("audit released=%v; want false on transfer", row["released"])
	}
}

// TestTransferNamespaceSubcommandRelease covers the --release variant: the
// namespace_owner key is gone after commit, transfer_history records the
// release with empty `to`, and the audit row uses the release event_type.
func TestTransferNamespaceSubcommandRelease(t *testing.T) {
	reg := registry.New(t.TempDir())
	seedNamespaceOwner(t, reg, "fli", "io.example.flights")

	args := []string{"transfer-namespace", "fli", "--release", "--yes"}
	result, err := gummain.DispatchPluginCommandWithRegistry(args, &mockHost{}, reg.ProfileDir(), func(string) *registry.Registry { return reg })
	if err != nil {
		t.Fatalf("DispatchPluginCommandWithRegistry: %v", err)
	}
	if !strings.Contains(result, `released namespace "fli" (was owned by "io.example.flights")`) {
		t.Errorf("result = %q; want contains release description with prior owner", result)
	}

	files, err := reg.Load()
	if err != nil {
		t.Fatalf("reg.Load: %v", err)
	}
	if _, found := plugins.LookupNamespaceOwner(files.Lock, "fli"); found {
		t.Errorf("post-release LookupNamespaceOwner returned found=true; want false")
	}

	rows := readAuditEntries(t, reg.ProfileDir())
	if len(rows) != 1 {
		t.Fatalf("audit.jsonl rows=%d; want 1", len(rows))
	}
	row := rows[0]
	if row["event_type"] != "plugin_namespace_release" {
		t.Errorf("audit event_type=%v; want plugin_namespace_release", row["event_type"])
	}
	if row["new_owner"] != "" {
		t.Errorf("audit new_owner=%v; want empty on release", row["new_owner"])
	}
	if released, _ := row["released"].(bool); !released {
		t.Errorf("audit released=%v; want true on release", row["released"])
	}
}

// TestTransferNamespaceSubcommandMissingYes guards the consent gate: without
// --yes the command MUST error before mutating plugins.lock or audit.jsonl.
func TestTransferNamespaceSubcommandMissingYes(t *testing.T) {
	reg := registry.New(t.TempDir())
	seedNamespaceOwner(t, reg, "fli", "io.example.flights")

	args := []string{"transfer-namespace", "fli", "--new-owner", "io.example2.flights"}
	_, err := gummain.DispatchPluginCommandWithRegistry(args, &mockHost{}, reg.ProfileDir(), func(string) *registry.Registry { return reg })
	if err == nil {
		t.Fatal("missing --yes returned nil error; want consent-required error")
	}
	if !strings.Contains(err.Error(), "--yes is required") {
		t.Errorf("err = %v; want '--yes is required' in message", err)
	}

	files, err := reg.Load()
	if err != nil {
		t.Fatalf("reg.Load: %v", err)
	}
	if got, _ := plugins.LookupNamespaceOwner(files.Lock, "fli"); got != "io.example.flights" {
		t.Errorf("plugins.lock owner=%q after rejected transfer; want unchanged io.example.flights", got)
	}
	if rows := readAuditEntries(t, reg.ProfileDir()); len(rows) != 0 {
		t.Errorf("audit.jsonl rows=%d after rejected transfer; want 0", len(rows))
	}
}

// TestTransferNamespaceSubcommandMissingPrefix asserts a missing prefix
// argument surfaces a usage error.
func TestTransferNamespaceSubcommandMissingPrefix(t *testing.T) {
	reg := registry.New(t.TempDir())
	_, err := gummain.DispatchPluginCommandWithRegistry([]string{"transfer-namespace"}, &mockHost{}, reg.ProfileDir(), func(string) *registry.Registry { return reg })
	if err == nil {
		t.Fatal("missing <prefix> returned nil error; want usage error")
	}
	if !strings.Contains(err.Error(), "missing <prefix>") {
		t.Errorf("err = %v; want 'missing <prefix>' in message", err)
	}
}

// TestTransferNamespaceSubcommandUnknownPrefix surfaces
// PLUGIN_PREFIX_NOT_BOUND when the requested prefix has never been recorded.
// The registry transaction MUST roll back without writing audit.jsonl.
func TestTransferNamespaceSubcommandUnknownPrefix(t *testing.T) {
	reg := registry.New(t.TempDir())

	args := []string{"transfer-namespace", "unknown", "--new-owner", "io.example.new", "--yes"}
	_, err := gummain.DispatchPluginCommandWithRegistry(args, &mockHost{}, reg.ProfileDir(), func(string) *registry.Registry { return reg })
	if err == nil {
		t.Fatal("unknown prefix returned nil error; want PLUGIN_PREFIX_NOT_BOUND")
	}
	if !errors.Is(err, plugins.ErrPrefixNotInLock) {
		t.Errorf("err = %v; want errors.Is(err, plugins.ErrPrefixNotInLock)", err)
	}
	if rows := readAuditEntries(t, reg.ProfileDir()); len(rows) != 0 {
		t.Errorf("audit.jsonl rows=%d after failed transfer; want 0", len(rows))
	}
}

// TestTransferNamespaceSubcommandConflictingFlags ensures --release and
// --new-owner cannot be combined.
func TestTransferNamespaceSubcommandConflictingFlags(t *testing.T) {
	reg := registry.New(t.TempDir())
	seedNamespaceOwner(t, reg, "fli", "io.example.flights")

	args := []string{"transfer-namespace", "fli", "--release", "--new-owner", "io.example2.flights", "--yes"}
	_, err := gummain.DispatchPluginCommandWithRegistry(args, &mockHost{}, reg.ProfileDir(), func(string) *registry.Registry { return reg })
	if err == nil {
		t.Fatal("--release + --new-owner returned nil error; want mutually-exclusive error")
	}
	if !strings.Contains(err.Error(), "mutually exclusive") {
		t.Errorf("err = %v; want 'mutually exclusive' in message", err)
	}
}

// TestTransferNamespaceSubcommandMissingMode requires either --new-owner or
// --release; an empty mode is rejected.
func TestTransferNamespaceSubcommandMissingMode(t *testing.T) {
	reg := registry.New(t.TempDir())
	seedNamespaceOwner(t, reg, "fli", "io.example.flights")

	args := []string{"transfer-namespace", "fli", "--yes"}
	_, err := gummain.DispatchPluginCommandWithRegistry(args, &mockHost{}, reg.ProfileDir(), func(string) *registry.Registry { return reg })
	if err == nil {
		t.Fatal("missing --new-owner/--release returned nil error; want mode error")
	}
	if !strings.Contains(err.Error(), "--new-owner") && !strings.Contains(err.Error(), "--release") {
		t.Errorf("err = %v; want mention of --new-owner or --release", err)
	}
}

// TestTransferNamespaceSubcommandUnknownFlag rejects typos so misspellings
// like --release-namespace can't silently fall through to the "missing mode"
// path.
func TestTransferNamespaceSubcommandUnknownFlag(t *testing.T) {
	reg := registry.New(t.TempDir())
	seedNamespaceOwner(t, reg, "fli", "io.example.flights")

	args := []string{"transfer-namespace", "fli", "--bogus", "--yes"}
	_, err := gummain.DispatchPluginCommandWithRegistry(args, &mockHost{}, reg.ProfileDir(), func(string) *registry.Registry { return reg })
	if err == nil {
		t.Fatal("unknown flag returned nil error; want rejection")
	}
	if !strings.Contains(err.Error(), "unknown flag") {
		t.Errorf("err = %v; want 'unknown flag' in message", err)
	}
}

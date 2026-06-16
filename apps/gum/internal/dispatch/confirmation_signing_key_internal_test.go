package dispatch

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLoadOrCreateSigningKeyPersistsAcrossProcesses is the live-sweep regression:
// the confirmation signing key MUST be stable across processes, or a destructive
// token issued by one `gum call` can never be verified by the next (the original
// bug used a process-random key). Two independent loads return the same key.
func TestLoadOrCreateSigningKeyPersistsAcrossProcesses(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)

	k1, ok := loadOrCreateSigningKey()
	if !ok {
		t.Fatal("first loadOrCreateSigningKey failed")
	}
	// File created 0600 at the expected path.
	path := filepath.Join(dir, "gum", "confirmation-signing.key")
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("key file not created: %v", err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("key file mode = %v; want 0600", fi.Mode().Perm())
	}
	// A second, independent load (simulating a separate process) returns the
	// SAME key — this is what makes issue→confirm work across CLI invocations.
	k2, ok := loadOrCreateSigningKey()
	if !ok {
		t.Fatal("second loadOrCreateSigningKey failed")
	}
	if k1 != k2 {
		t.Error("signing key differs between loads; cross-process confirm would fail")
	}
}

// TestConfirmationTokenVerifiesAcrossKeyReload proves the end-to-end fix: a token
// issued under one loaded key verifies after the in-memory key is reloaded from
// the persisted file (the cross-process scenario). Before the fix the reload
// produced a fresh random key and verification failed with reason=mismatch.
func TestConfirmationTokenVerifiesAcrossKeyReload(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)

	// "Process 1": load key, issue a token.
	resetSigningKeyForTest()
	params := ConfirmationParams{
		OpID:      "gmail.users.labels.delete",
		VariantID: "v1",
		ArgsHash:  "abc123",
		Purpose:   ConfirmationPurposeDestructive,
		TTL:       DefaultDestructiveTokenTTL,
	}
	tok, err := IssueConfirmationToken(params)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}

	// "Process 2": fresh in-memory key state, reloads the SAME persisted key.
	resetSigningKeyForTest()
	if verr := VerifyConfirmationToken(tok, params); verr != nil {
		t.Fatalf("verify after key reload failed: %v — cross-process confirm is broken", verr)
	}
}

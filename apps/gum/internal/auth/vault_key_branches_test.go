package auth

import (
	"strings"
	"testing"
)

// TestVaultKeyTruncatesLongSubject pins the `len(subj) > 16 → subj = subj[:16]`
// arm. Subject fingerprints can be arbitrarily long (e.g. an email or full
// hex-encoded digest); vaultKey MUST truncate so keychain entries don't
// exceed the platform's per-entry length cap and so the canonical key
// stays stable across fingerprint formats.
func TestVaultKeyTruncatesLongSubject(t *testing.T) {
	longSubj := "abcdefghij0123456789-extra-bytes-that-must-be-truncated"
	got := vaultKey("oauth", longSubj, []string{"https://www.googleapis.com/auth/gmail.readonly"})

	// Key shape: gum.<strategy>.<subj>.<hex>. The subject segment must be
	// exactly the first 16 chars of longSubj.
	parts := strings.Split(got, ".")
	if len(parts) < 4 {
		t.Fatalf("vaultKey shape unexpected: %q (want at least 4 dot-parts)", got)
	}
	if parts[2] != longSubj[:16] {
		t.Errorf("subject segment = %q; want %q (truncated to 16 chars)", parts[2], longSubj[:16])
	}
}

// TestVaultKeyEmptySubjectDefaults pins the `subj == "" → subj = "default"`
// arm. An anonymous resolver (no subject hint) MUST still produce a
// stable, collision-free key under the "default" subject bucket rather
// than ending up with gum.<strategy>..<hex> which would be ambiguous
// with a truncated-to-empty fingerprint.
func TestVaultKeyEmptySubjectDefaults(t *testing.T) {
	got := vaultKey("api_key", "", nil)
	parts := strings.Split(got, ".")
	if len(parts) < 4 {
		t.Fatalf("vaultKey shape unexpected: %q", got)
	}
	if parts[2] != "default" {
		t.Errorf("subject segment = %q; want \"default\"", parts[2])
	}
}

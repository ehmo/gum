// Package tee implements the §9.0 filesystem tee artifact lifecycle:
// per-profile tee.secret management, principal-scoped content-addressed
// hashing, and gzip-compressed artifact writes used by lossy expression
// profiles' recovery handles (`full_result_path`, `full_result_resource`).
//
// Five normative lifecycle points (spec §9 "tee.secret lifecycle"):
//
//  1. Generate — 32 random bytes from crypto/rand, hex-encoded (64 lowercase
//     ASCII chars), written to ~/.local/share/gum/<profile>/tee.secret at
//     mode 600 inside a mode-700 profile dir.
//  2. Corruption / missing at runtime — surface TEE_SECRET_CORRUPT. Silent
//     regeneration is prohibited.
//  3. Algorithm stability — HMAC-SHA-256 keyed by tee.secret; not versioned
//     in v0.1.0.
//  4. Reverse lookup for gum://results/{hash} — directory scan (sibling
//     package gum-uuh).
//  5. Embedding-model independence — tee.secret has its own lifecycle and is
//     not touched by catalog or HTTP-cache migrations.
package tee

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// secretHexLen is the canonical length of a tee.secret file: 64 lowercase
// hex chars representing 32 random bytes (spec §9 lifecycle point 1).
const secretHexLen = 64

// secretByteLen is the raw key length used by HMAC-SHA-256.
const secretByteLen = 32

// ErrSecretCorrupt is the sentinel returned when a tee.secret file exists
// but cannot be used: empty, wrong length, non-hex, non-lowercase, or
// unreadable. Spec §9 lifecycle point 2 mandates this is a hard failure
// (TEE_SECRET_CORRUPT) — silent regeneration is prohibited.
var ErrSecretCorrupt = errors.New("tee.secret is corrupt")

// SecretCorruptError wraps ErrSecretCorrupt with the offending path and a
// machine-readable suggestion, suitable for projecting into a
// *dispatch.StructuredError at the dispatch boundary.
type SecretCorruptError struct {
	Path   string
	Reason string
}

func (e *SecretCorruptError) Error() string {
	return fmt.Sprintf("tee.secret is corrupt at %s: %s", e.Path, e.Reason)
}

func (e *SecretCorruptError) Unwrap() error { return ErrSecretCorrupt }

// Suggestion returns the user-facing remediation string mandated by spec §9
// lifecycle point 2.
func (e *SecretCorruptError) Suggestion() string {
	return "Remove or rename the file; gum will regenerate it on next start, invalidating any previously emitted gum://results/<hash> handles."
}

// SecretPath returns the canonical absolute path of the tee.secret for the
// given profile dir. The profileDir is expected to be
// ~/.local/share/gum/<profile>/ (the caller resolves the home dir).
func SecretPath(profileDir string) string {
	return filepath.Join(profileDir, "tee.secret")
}

// LoadOrCreateSecret returns the 32-byte tee.secret key for the profile dir,
// creating it (mode 600 inside a mode-700 profileDir) when absent. If the
// file exists but is corrupt — wrong length, non-hex, non-lowercase, or
// unreadable in a way other than "not exist" — it returns *SecretCorruptError
// wrapping ErrSecretCorrupt. The runtime MUST surface this as
// TEE_SECRET_CORRUPT and refuse to process the request.
func LoadOrCreateSecret(profileDir string) ([]byte, error) {
	path := SecretPath(profileDir)
	if key, err := loadSecret(path); err == nil {
		return key, nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}

	if err := os.MkdirAll(profileDir, 0o700); err != nil {
		return nil, fmt.Errorf("tee: create profile dir %s: %w", profileDir, err)
	}
	var raw [secretByteLen]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return nil, fmt.Errorf("tee: generate secret: %w", err)
	}
	encoded := hex.EncodeToString(raw[:])
	if err := atomicWrite(profileDir, ".tee.secret.*", path, []byte(encoded), 0o600, "secret"); err != nil {
		return nil, err
	}
	return raw[:], nil
}

// LoadSecret reads an existing tee.secret. Returns fs.ErrNotExist when the
// file is absent (the caller decides whether to generate). On invalid
// content it returns *SecretCorruptError.
func LoadSecret(profileDir string) ([]byte, error) {
	return loadSecret(SecretPath(profileDir))
}

func loadSecret(path string) ([]byte, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
		return nil, &SecretCorruptError{Path: path, Reason: "unreadable: " + err.Error()}
	}
	text := strings.TrimRight(string(raw), "\n")
	if len(text) == 0 {
		return nil, &SecretCorruptError{Path: path, Reason: "empty file"}
	}
	if len(text) != secretHexLen {
		return nil, &SecretCorruptError{Path: path, Reason: fmt.Sprintf("expected %d hex chars, got %d", secretHexLen, len(text))}
	}
	if text != strings.ToLower(text) {
		return nil, &SecretCorruptError{Path: path, Reason: "uppercase hex chars present (lowercase required)"}
	}
	key, err := hex.DecodeString(text)
	if err != nil {
		return nil, &SecretCorruptError{Path: path, Reason: "non-hex characters"}
	}
	if len(key) != secretByteLen {
		return nil, &SecretCorruptError{Path: path, Reason: fmt.Sprintf("decoded %d bytes, expected %d", len(key), secretByteLen)}
	}
	return key, nil
}

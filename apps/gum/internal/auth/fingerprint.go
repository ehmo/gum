package auth

import (
	"crypto/sha256"
	"encoding/hex"
)

// DeriveSubjectFingerprint returns a stable opaque ID for a credential subject.
// The input should be material that uniquely identifies the principal across
// sessions: a refresh token (byo_oauth), the raw ADC JSON (adc), or a
// service-account client_email. The output is the first 16 hex chars of the
// SHA-256 (8 bytes of entropy — sufficient for principal-switching detection,
// short enough to embed in cache keys and audit lines without bloating them).
//
// Returns the empty string when input is empty so callers can detect
// "unknown subject" via a zero value rather than a stable hash of "".
func DeriveSubjectFingerprint(material string) string {
	if material == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(material))
	return hex.EncodeToString(sum[:8])
}

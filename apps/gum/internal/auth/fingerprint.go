package auth

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"strings"
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

// oauthSubjectFromIDToken extracts the spec §10.0.1 OAuth principal from an
// OpenID Connect id_token: the lower-case account email when the token carries
// one, otherwise the `sub` claim. It returns "" when the token is absent or
// unreadable.
//
// The payload is read without signature verification because the id_token came
// straight from the token endpoint over TLS in the same response as the access
// token. It is used only to name a local cache partition, never to authorize.
func oauthSubjectFromIDToken(idToken string) string {
	parts := strings.Split(strings.TrimSpace(idToken), ".")
	if len(parts) != 3 {
		return ""
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return ""
	}
	var claims struct {
		Sub   string `json:"sub"`
		Email string `json:"email"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return ""
	}
	if email := strings.ToLower(strings.TrimSpace(claims.Email)); email != "" {
		return email
	}
	return strings.TrimSpace(claims.Sub)
}

// byoSubjectFingerprint derives the byo_oauth fingerprint from the account
// principal. It falls back to the refresh token for grants stored before
// gum-mc67, whose principal is unknown: that fingerprint moves on every
// re-login, but an empty one would merge every account into one cache
// partition, which is the worse failure.
func byoSubjectFingerprint(subject, refreshToken string) string {
	if subject != "" {
		return DeriveSubjectFingerprint("byo_oauth:" + subject)
	}
	return DeriveSubjectFingerprint("byo_oauth:" + refreshToken)
}

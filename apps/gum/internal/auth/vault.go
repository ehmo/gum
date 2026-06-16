package auth

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
)

// CredentialVault is the typed wrapper around a KeyringBackend that the
// gum_oauth flow (gum-xth) and byo_oauth flow both use to persist refresh
// tokens by subject fingerprint. Keeping the keychain key shape in one place
// — rather than letting every strategy roll its own — means the wire-level
// invariant (spec §7: "never plaintext, always per-subject") is enforced by
// a single small surface that the security audit can grep for.
type CredentialVault struct {
	kb KeyringBackend
}

// NewCredentialVault wraps kb. Pass NewOSKeyring() in production, or an
// in-memory backend in tests.
func NewCredentialVault(kb KeyringBackend) *CredentialVault {
	return &CredentialVault{kb: kb}
}

// NewDefaultCredentialVault returns a vault backed by the OS keychain.
func NewDefaultCredentialVault() *CredentialVault {
	return &CredentialVault{kb: NewOSKeyring()}
}

// vaultKey computes the canonical keyring key for (strategy, subject, scopes).
// The shape is `gum.<strategy>.<subject-fp-prefix>.<scope-hash>` so the audit
// can scope refresh-token entries per principal and per scope set — credential
// switching never replays the prior subject's token (spec §10.0.1).
func vaultKey(strategy, subjectFingerprint string, scopes []string) string {
	subj := subjectFingerprint
	if len(subj) > 16 {
		subj = subj[:16]
	}
	if subj == "" {
		subj = "default"
	}
	return fmt.Sprintf("gum.%s.%s.%s", strategy, subj, scopeHashHex(scopes))
}

func scopeHashHex(scopes []string) string {
	sorted := append([]string{}, scopes...)
	sort.Strings(sorted)
	sum := sha256.Sum256([]byte(strings.Join(sorted, " ")))
	return hex.EncodeToString(sum[:8])
}

// StoreRefreshToken persists refreshToken under the canonical key for the
// (strategy, subject, scopes) tuple. Returns AUTH_KEYCHAIN_UNAVAILABLE
// (wrapped *AuthError) when the OS backend is missing.
func (v *CredentialVault) StoreRefreshToken(strategy, subjectFingerprint string, scopes []string, refreshToken string) error {
	return v.kb.Set(vaultKey(strategy, subjectFingerprint, scopes), refreshToken)
}

// LookupRefreshToken returns the persisted refresh token for the tuple, or
// ("", nil) if absent. Errors are AUTH_KEYCHAIN_UNAVAILABLE.
func (v *CredentialVault) LookupRefreshToken(strategy, subjectFingerprint string, scopes []string) (string, error) {
	return v.kb.Get(vaultKey(strategy, subjectFingerprint, scopes))
}

// DeleteRefreshToken removes the persisted refresh token for the tuple.
// Absent keys are not an error.
func (v *CredentialVault) DeleteRefreshToken(strategy, subjectFingerprint string, scopes []string) error {
	return v.kb.Delete(vaultKey(strategy, subjectFingerprint, scopes))
}

// gumOAuthVaultIndexKey is the keyring key under which GumOAuth tracks all
// vault entries it has written. The value is a newline-joined list of vault
// keys (each of the form gum.gum_oauth.<subj>.<hash>). RevokeAllGumOAuth reads
// and clears this list so Logout can purge every gum_oauth token without
// requiring prefix-scan support from the OS keychain backend (the zalando
// go-keyring backend exposes only Get/Set/Delete — no enumeration).
const gumOAuthVaultIndexKey = "gum.gum_oauth._index"

func gumOAuthSubjectKey(scopes []string) string {
	return "gum.gum_oauth._subject." + scopeHashHex(scopes)
}

// StoreGumOAuthSubject records the currently selected gum_oauth subject for a
// scope set. Resolve has only the requested scopes before it refreshes a token,
// so Login writes this pointer after deriving the subject from the ID token.
func (v *CredentialVault) StoreGumOAuthSubject(scopes []string, subjectFingerprint string) error {
	return v.kb.Set(gumOAuthSubjectKey(scopes), subjectFingerprint)
}

// LookupGumOAuthSubject returns the subject fingerprint most recently selected
// by gum_oauth Login for this exact scope set.
func (v *CredentialVault) LookupGumOAuthSubject(scopes []string) (string, error) {
	return v.kb.Get(gumOAuthSubjectKey(scopes))
}

// TrackGumOAuthKey appends key to the gum_oauth index entry so RevokeAllGumOAuth
// can later find and delete it. The key may be either a refresh-token entry or
// an auxiliary gum_oauth metadata pointer. Duplicates are skipped to keep the
// index small.
// This is a read-modify-write over the keyring; callers accept best-effort
// semantics (a crash between StoreRefreshToken and TrackGumOAuthKey leaves a
// token Logout will not find, but Google refresh tokens decay on their own).
func (v *CredentialVault) TrackGumOAuthKey(key string) error {
	existing, err := v.kb.Get(gumOAuthVaultIndexKey)
	if err != nil {
		return err
	}
	for _, entry := range splitIndex(existing) {
		if entry == key {
			return nil
		}
	}
	return v.kb.Set(gumOAuthVaultIndexKey, joinIndex(append(splitIndex(existing), key)))
}

// RevokeAllGumOAuth reads the gum_oauth index and deletes every vault key listed
// there, then deletes the index itself. Absent keys are silently skipped.
// Returns the first keyring error encountered but continues deleting the rest so
// one missing key never blocks the others.
func (v *CredentialVault) RevokeAllGumOAuth() error {
	raw, err := v.kb.Get(gumOAuthVaultIndexKey)
	if err != nil {
		return err
	}
	var firstErr error
	for _, key := range splitIndex(raw) {
		if delErr := v.kb.Delete(key); delErr != nil && firstErr == nil {
			firstErr = delErr
		}
	}
	if delErr := v.kb.Delete(gumOAuthVaultIndexKey); delErr != nil && firstErr == nil {
		firstErr = delErr
	}
	return firstErr
}

// splitIndex splits a newline-joined index value into vault keys, dropping the
// empty strings that result from trailing newlines or an empty value.
func splitIndex(raw string) []string {
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, "\n")
	out := parts[:0]
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// joinIndex joins vault keys back to the newline-separated index string.
func joinIndex(keys []string) string {
	return strings.Join(keys, "\n")
}

// NewDefaultByoOAuth wires ByoOAuth with the OS keyring backend. Used by the
// CLI to construct a production-ready BYO resolver once the user has
// provided ClientID + ClientSecret + Scopes via `gum auth use-oauth-client`.
func NewDefaultByoOAuth(cfg ByoOAuthConfig) *ByoOAuth {
	return NewByoOAuth(cfg, NewOSKeyring())
}

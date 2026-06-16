package auth

import "strings"

// OAuthRemediation maps a Google OAuth/token-endpoint error body to a
// concrete next step the operator can run. v0.1.0 covers the two cases that
// dominate field reports:
//
//   - invalid_grant   — refresh token revoked, expired, or never seen by the
//     endpoint. Almost always fixed by re-running the
//     application-default login flow.
//   - invalid_rapt    — Reauth-Proof Token expired (a Google second-factor
//     step for sensitive scopes). Fixed by re-authing.
//
// Returns an empty string for bodies that don't match a known pattern so
// callers can keep their existing remediation text. Body is matched
// case-insensitively because Google occasionally varies casing in error
// payloads from different token endpoints (oauth2.googleapis.com vs
// sts.googleapis.com).
func OAuthRemediation(body string) string {
	lower := strings.ToLower(body)
	switch {
	case strings.Contains(lower, "client_secret is missing"):
		// Google "Desktop app" OAuth clients are issued a client_secret and the
		// token endpoint REQUIRES it even with PKCE (Google does not implement
		// true secret-less public clients for Desktop). The operator registered
		// the client without a secret, so the exchange omitted it.
		return "This Google OAuth client requires its client_secret (Desktop-app clients have one even with PKCE). Re-register it with the secret from the Google Cloud console (or your downloaded client JSON):\n  gum auth use-oauth-client --client-id <id> --secret-stdin\nthen run `gum login` again."
	case strings.Contains(lower, "invalid_rapt"):
		return "Google reauth-proof token expired; re-run `gcloud auth application-default login` (or `gum auth login`) and retry"
	case strings.Contains(lower, "invalid_grant"):
		return "Refresh token revoked or expired; re-run `gcloud auth application-default login` (or `gum auth login`) and retry"
	}
	return ""
}

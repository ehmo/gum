package auth

import (
	"context"
	"strings"
)

// logout.go implements `gum logout`: clearing the OAuth credentials for a
// profile so the next `gum login` starts clean. The primary use is switching
// Google accounts — paired with the login flow's prompt=select_account
// (byooauth_login.go), an operator can drop the current grant and re-authorize
// as a different Google account.
//
// Scope of clearing (two non-obvious effects):
//   - Revoke is best-effort server-side AND local: it POSTs the refresh token to
//     Google's revocation endpoint (so a captured copy stops working) and then
//     deletes the local grant; a remote failure still clears local state.
//   - Grants are profile-scoped (gum-2fu0): the grant key includes the profile,
//     so Logout clears only the named profile's grant — even when the same OAuth
//     client is reused under several profiles. The lone exception is the
//     "default" profile, which keeps the legacy unprefixed key for backward
//     compatibility; clearing it does not touch other profiles.

// LogoutResult reports what Logout cleared so the CLI can give accurate
// feedback (and so it can say "nothing to do" instead of a misleading success).
type LogoutResult struct {
	// ClientID is the OAuth client whose grant was targeted (a registered BYO
	// client). Empty when no BYO client is configured.
	ClientID string
	// UsingManaged is kept for old JSON consumers. v1 does not target a
	// built-in managed OAuth fallback, so this value is always false.
	UsingManaged bool
	// GrantCleared is true when a stored refresh-token grant was present and
	// removed. False means there was nothing to revoke (already logged out).
	GrantCleared bool
	// ClientForgotten is true when a registered BYO client entry was removed.
	// Only ever true when forgetClient is set AND a BYO client was registered.
	ClientForgotten bool
	// ForgetClientSkipped is true when forgetClient was requested but there was
	// no registered BYO client to remove. It lets the CLI explain why
	// client_forgotten is false instead of silently dropping the flag.
	ForgetClientSkipped bool
	// GumOAuthVaultCleared is true when the gum_oauth credential vault was
	// non-empty and was purged by RevokeAllGumOAuth. False when the vault index
	// was absent (gum_oauth was never used on this machine).
	GumOAuthVaultCleared bool
}

// Logout clears the stored OAuth credentials for profile. It revokes (deletes)
// the refresh-token grant for the registered BYO client. When forgetClient is
// true it also removes that BYO client entry. Clearing an absent grant or client
// is not an error: Logout is idempotent so re-running it, or running it when
// already logged out, is safe.
func Logout(ctx context.Context, kb KeyringBackend, profile string, forgetClient bool) (LogoutResult, error) {
	var res LogoutResult

	client, registered, err := LoadByoClient(kb, profile)
	if err != nil {
		return res, err
	}
	if forgetClient && !registered {
		// No registered BYO client to remove — record the skip so the caller
		// can explain it rather than silently ignoring --forget-client.
		res.ForgetClientSkipped = true
	}
	switch {
	case registered:
		res.ClientID = client.ClientID
	default:
		return res, nil
	}

	b := NewByoOAuth(ByoOAuthConfig{ClientID: client.ClientID, Profile: profile}, kb)
	// Determine grant presence from the raw entry rather than a parsed grant:
	// a corrupt/unparseable value still represents stored state that Revoke
	// removes, so it must count as cleared (loadGrant would report it absent).
	raw, getErr := kb.Get(b.keyringKey())
	if getErr != nil {
		return res, getErr
	}
	res.GrantCleared = strings.TrimSpace(raw) != ""
	if err := b.Revoke(ctx); err != nil {
		return res, err
	}

	// Purge any gum_oauth refresh tokens tracked by the vault index. gum_oauth
	// is not used by the public v1 login path, but old vault state should not
	// survive an explicit logout.
	gumOAuth := &GumOAuth{Vault: NewCredentialVault(kb)}
	idxRaw, idxErr := kb.Get(gumOAuthVaultIndexKey)
	if idxErr != nil {
		return res, idxErr
	}
	if idxRaw != "" {
		if err := gumOAuth.Revoke(); err != nil {
			return res, err
		}
		res.GumOAuthVaultCleared = true
	} else {
		// Index absent: best-effort cleanup of any orphaned keys written before
		// the index existed; do not claim the vault was cleared.
		_ = gumOAuth.Revoke()
	}

	if forgetClient && registered {
		if err := DeleteByoClient(kb, profile); err != nil {
			return res, err
		}
		res.ClientForgotten = true
	}

	return res, nil
}

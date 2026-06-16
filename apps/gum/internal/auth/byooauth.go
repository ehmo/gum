package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/ehmo/gum/internal/httputil"
)

// KeyringBackend abstracts the system keychain so tests can inject an in-memory
// store without touching the real OS keyring (via go-keyring).
type KeyringBackend interface {
	// Get retrieves a stored secret by key. Returns ("", nil) when absent.
	Get(key string) (string, error)
	// Set stores a secret under key.
	Set(key, value string) error
	// Delete removes a stored secret. Returns nil if the key did not exist.
	Delete(key string) error
}

// DefaultRevokeEndpoint is Google's OAuth 2.0 token-revocation endpoint, used by
// Revoke when ByoOAuthConfig.RevokeEndpoint is empty. It is a package var so
// tests (including in other packages) can redirect revocation to a local server
// and stay hermetic — production code never reassigns it.
var DefaultRevokeEndpoint = "https://oauth2.googleapis.com/revoke"

// ByoOAuthConfig carries the OAuth 2.0 client credentials and requested scopes
// provided by the end-user at setup time ("bring your own OAuth app").
type ByoOAuthConfig struct {
	// ClientID is the OAuth 2.0 client identifier.
	ClientID string
	// ClientSecret is the OAuth 2.0 client secret.
	ClientSecret string
	// Profile scopes the stored grant so the same OAuth client reused across
	// profiles never shares one refresh-token grant (cross-account token
	// substitution, gum-2fu0). Empty or "default" keeps the legacy unprefixed
	// key so existing single-profile installs are not invalidated. Callers MUST
	// pass the same profile for store (login) and read (resolve/logout/granted)
	// or the grant will not be found.
	Profile string
	// Scopes is the list of OAuth 2.0 scopes to request.
	Scopes []string
	// TokenEndpoint overrides the default Google token endpoint. Used in tests.
	// Defaults to "https://oauth2.googleapis.com/token" when empty.
	TokenEndpoint string
	// AuthEndpoint overrides the default Google authorization endpoint. Used in
	// tests. Defaults to defaultGumOAuthAuthURL when empty.
	AuthEndpoint string
	// RevokeEndpoint overrides the default Google token revocation endpoint.
	// Used in tests. Defaults to "https://oauth2.googleapis.com/revoke".
	RevokeEndpoint string
}

// ByoOAuth implements the byo_oauth strategy. It looks up a cached refresh token
// from the KeyringBackend, exchanges it for an access token (refreshing on expiry),
// and stores updated refresh tokens back to the keyring.
type ByoOAuth struct {
	cfg    ByoOAuthConfig
	kb     KeyringBackend
	cached *Credentials
	client *http.Client
	// BrowserOpener launches the user's browser at the authorization URL
	// during interactive Login. nil means "do not open" (tests drive the
	// loopback callback directly; the CLI wires a real opener).
	BrowserOpener func(authURL string) error
}

// NewByoOAuth constructs a ByoOAuth with the given config and keyring backend.
// Inject an in-memory KeyringBackend for unit tests.
func NewByoOAuth(cfg ByoOAuthConfig, kb KeyringBackend) *ByoOAuth {
	if cfg.TokenEndpoint == "" {
		cfg.TokenEndpoint = "https://oauth2.googleapis.com/token"
	}
	return &ByoOAuth{
		cfg:    cfg,
		kb:     kb,
		client: newDefaultAuthHTTPClient(),
	}
}

// keyringKey returns the canonical keyring key for this OAuth client. The key
// is derived from the client_id (NOT the requested scopes) so a single stored
// grant is reused across operations: a broad `gum login` and a narrow per-op
// `gum call` against the same client resolve to the same entry, and the stored
// granted-scope set (see byoGrant) decides whether re-authorization is needed
// via scopesSatisfied. Keying by scope-hash instead would strand a broad grant
// behind every narrower lookup (gum-ergy).
func (b *ByoOAuth) keyringKey() string {
	p := strings.TrimSpace(b.cfg.Profile)
	if p == "" || p == DefaultAPIKeyProfile {
		// Legacy unprefixed key: a single-profile (default) install keeps its
		// established entry, so this change does not force a re-login. A unique
		// "default" can never collide with another profile.
		sum := sha256.Sum256([]byte(b.cfg.ClientID))
		return fmt.Sprintf("gum.byo_oauth.%s", hex.EncodeToString(sum[:8]))
	}
	// Profile-scoped key (gum-2fu0): the same OAuth client registered under two
	// profiles must not share one grant, or the second login's refresh token
	// would be served to the first profile (wrong Google account).
	sum := sha256.Sum256([]byte(p + "\x00" + b.cfg.ClientID))
	return fmt.Sprintf("gum.byo_oauth.%s", hex.EncodeToString(sum[:8]))
}

// byoGrant is the JSON value stored under keyringKey: the refresh token plus
// the set of scopes the user has actually granted to this client. Recording the
// granted set lets Acquire serve any request whose scopes are a subset, instead
// of forcing a fresh consent for every distinct scope combination.
type byoGrant struct {
	RefreshToken string   `json:"refresh_token"`
	Scopes       []string `json:"scopes"`
}

// loadGrant reads the stored grant for this client. ok=false means no usable
// grant: an absent entry, a keyring read error (surfaced separately), or a
// legacy/unparseable value (treated as absent so the caller re-authorizes
// cleanly rather than stranding on corrupt state).
func (b *ByoOAuth) loadGrant() (byoGrant, bool, error) {
	raw, err := b.kb.Get(b.keyringKey())
	if err != nil {
		return byoGrant{}, false, err
	}
	if raw == "" {
		return byoGrant{}, false, nil
	}
	var g byoGrant
	if jsonErr := json.Unmarshal([]byte(raw), &g); jsonErr != nil {
		return byoGrant{}, false, nil
	}
	return g, true, nil
}

// sortedUniqueScopes merges scope lists into a sorted, de-duplicated set,
// dropping empties. Used to accumulate the granted-scope union on store.
func sortedUniqueScopes(lists ...[]string) []string {
	seen := map[string]bool{}
	var out []string
	for _, list := range lists {
		for _, s := range list {
			if s == "" || seen[s] {
				continue
			}
			seen[s] = true
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}

// scopesSatisfied reports whether every requested scope is present in the
// granted set — i.e. the stored grant is a superset of what the caller needs.
func scopesSatisfied(granted, requested []string) bool {
	have := make(map[string]bool, len(granted))
	for _, s := range granted {
		have[s] = true
	}
	for _, s := range requested {
		// Exact grant, or a broader granted scope that subsumes it. The latter
		// lets an op declaring gmail.metadata resolve against a token that holds
		// gmail.readonly instead (gum drops the poisonous gmail.metadata from the
		// login union — see PruneLoginScopes).
		if !have[s] && !scopeSubsumedBy(s, have) {
			return false
		}
	}
	return true
}

// tokenResponse is the JSON shape of a token exchange response.
type tokenResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
	Scope       string `json:"scope"`
}

// Acquire looks up the cached refresh token from the KeyringBackend, exchanges it
// for a fresh access token, stores any new refresh token, and returns the Credentials.
// Returns *AuthError with Code="NO_REFRESH_TOKEN" if no token is cached.
func (b *ByoOAuth) Acquire(ctx context.Context) (*Credentials, error) {
	// Return cached credentials if still valid (with 30s buffer).
	if b.cached != nil && time.Now().Before(b.cached.ExpiresAt.Add(-30*time.Second)) {
		return b.cached, nil
	}

	// Look up the stored grant for this client. The grant is reused only when
	// it covers every requested scope (superset match); a missing, unreadable,
	// or insufficient grant routes to re-authorization with the requested
	// scopes so `gum login` / JIT consents to exactly what is missing.
	grant, ok, err := b.loadGrant()
	if err != nil || !ok || grant.RefreshToken == "" || !scopesSatisfied(grant.Scopes, b.cfg.Scopes) {
		return nil, &AuthError{
			Code:             "NO_REFRESH_TOKEN",
			Strategy:         "byo_oauth",
			RequiredScopes:   append([]string{}, b.cfg.Scopes...),
			SetupCommand:     "gum login",
			HumanRemediation: "no stored authorization for these scopes; run `gum login` (gum also prompts to authorize on first use)",
			UserMessage:      "Authorize gum to access these scopes — run `gum login`.",
			Retryable:        true,
		}
	}
	refreshToken := grant.RefreshToken

	// Build token refresh request. A public PKCE client has no secret; per
	// RFC 6749 §2.3.1 it MUST NOT send client_secret (mirrors exchangeAuthCode).
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
		"client_id":     {b.cfg.ClientID},
	}
	if b.cfg.ClientSecret != "" {
		form.Set("client_secret", b.cfg.ClientSecret)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, b.cfg.TokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, &AuthError{
			Code:             "AUTH_REFRESH_FAILED",
			Strategy:         "byo_oauth",
			HumanRemediation: fmt.Sprintf("failed to build request: %v", err),
		}
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := b.client.Do(req)
	if err != nil {
		return nil, &AuthError{
			Code:             "AUTH_REFRESH_FAILED",
			Strategy:         "byo_oauth",
			HumanRemediation: fmt.Sprintf("HTTP request failed: %v", err),
		}
	}
	defer func() { _ = resp.Body.Close() }()

	// Token-exchange responses are tiny (~1 KiB). Cap at 1 MiB to defend
	// against a hostile or misconfigured token endpoint (gum-4d66).
	body, readErr := httputil.ReadCapped(resp.Body, 1<<20)
	if readErr != nil {
		return nil, &AuthError{
			Code:             "AUTH_REFRESH_FAILED",
			Strategy:         "byo_oauth",
			HumanRemediation: fmt.Sprintf("token endpoint response too large: %v", readErr),
		}
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		rem := OAuthRemediation(string(body))
		if rem == "" {
			rem = fmt.Sprintf("token endpoint returned HTTP %d: %s", resp.StatusCode, string(body))
		}
		return nil, &AuthError{
			Code:             "AUTH_REFRESH_FAILED",
			Strategy:         "byo_oauth",
			HumanRemediation: rem,
		}
	}

	var tr tokenResponse
	if err := json.Unmarshal(body, &tr); err != nil {
		return nil, &AuthError{
			Code:             "AUTH_REFRESH_FAILED",
			Strategy:         "byo_oauth",
			HumanRemediation: fmt.Sprintf("failed to parse token response: %v", err),
		}
	}

	creds := &Credentials{
		Token:              tr.AccessToken,
		ExpiresAt:          time.Now().Add(time.Duration(tr.ExpiresIn) * time.Second),
		Scopes:             b.cfg.Scopes,
		StrategyName:       "byo_oauth",
		SubjectFingerprint: DeriveSubjectFingerprint("byo_oauth:" + refreshToken),
	}
	b.cached = creds
	return creds, nil
}

// StoreRefreshToken persists rt under the per-client keyring entry, recording
// the granted-scope set so later subset requests resolve without re-consent.
// The stored scopes accumulate the union of any existing grant and the scopes
// just authorized — paired with Login's include_granted_scopes=true, the latest
// refresh token is valid for that union, so a second per-op authorization does
// not clobber an earlier one.
func (b *ByoOAuth) StoreRefreshToken(rt string) error {
	scopes := sortedUniqueScopes(b.cfg.Scopes)
	if existing, ok, _ := b.loadGrant(); ok {
		scopes = sortedUniqueScopes(existing.Scopes, scopes)
	}
	blob, _ := json.Marshal(byoGrant{RefreshToken: rt, Scopes: scopes})
	return b.kb.Set(b.keyringKey(), string(blob))
}

// storeLoginGrant persists the grant produced by a completed login, recording
// the scopes Google ACTUALLY granted (the token response's space-separated
// `scope`). With include_granted_scopes that value is the authoritative union
// FOR THE AUTHENTICATED ACCOUNT, so unlike StoreRefreshToken it deliberately
// does NOT fold in the previous grant's scopes: the account chooser
// (prompt=select_account) lets a user switch Google accounts without a prior
// logout, and accumulating would inflate the new account's grant with the old
// account's scopes — Acquire would then refresh for a scope the new account
// never authorized and the API would return a silent 403. When the server omits
// `scope`, it falls back to StoreRefreshToken so incremental auth never regresses.
func (b *ByoOAuth) storeLoginGrant(rt, grantedScope string) error {
	granted := sortedUniqueScopes(strings.Fields(grantedScope))
	if len(granted) == 0 {
		// Server omitted the scope field (RFC 6749: granted == requested). Store
		// exactly the requested scopes — do NOT fall back to the accumulating
		// StoreRefreshToken, which would re-fold the previous grant's scopes and
		// re-introduce the cross-account inflation this method exists to prevent
		// (account switch via prompt=select_account → Acquire serves an
		// unauthorized scope → silent 403). Losing same-account incremental
		// accumulation here only costs a harmless extra consent on the next call.
		granted = sortedUniqueScopes(b.cfg.Scopes)
	}
	blob, _ := json.Marshal(byoGrant{RefreshToken: rt, Scopes: granted})
	return b.kb.Set(b.keyringKey(), string(blob))
}

// Revoke invalidates the stored grant. It first makes a BEST-EFFORT call to
// Google's token revocation endpoint to invalidate the refresh token
// server-side, then deletes the local grant and drops the in-memory cache. The
// remote call is best-effort: a failure (offline, already-expired token, non-2xx)
// never blocks the local delete, so logout always clears local state. The
// context bounds the remote call.
func (b *ByoOAuth) Revoke(ctx context.Context) error {
	if grant, ok, _ := b.loadGrant(); ok && grant.RefreshToken != "" {
		b.revokeRemote(ctx, grant.RefreshToken)
	}
	err := b.kb.Delete(b.keyringKey())
	b.cached = nil
	return err
}

// revokeRemote best-effort POSTs the token to Google's revocation endpoint.
// All failures are swallowed: the caller's local delete is what guarantees the
// credential is gone from this machine; server-side revocation is a bonus.
func (b *ByoOAuth) revokeRemote(ctx context.Context, token string) {
	endpoint := b.cfg.RevokeEndpoint
	if endpoint == "" {
		endpoint = DefaultRevokeEndpoint
	}
	form := url.Values{"token": {token}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := b.client.Do(req)
	if err != nil {
		return
	}
	// Drain + close so the connection can be reused/closed cleanly; status is
	// intentionally ignored (a 400 "token already invalid" is an acceptable
	// outcome for revocation).
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
}

// Resolve satisfies the auth.Resolver interface by calling Acquire.
func (b *ByoOAuth) Resolve(ctx context.Context, scopes []string) (*Credentials, error) {
	return b.Acquire(ctx)
}

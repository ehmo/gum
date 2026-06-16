package auth

// gum_oauth.go implements the spec §7 managed OAuth strategy:
// installed-app PKCE + loopback redirect + CSRF state. The client_id comes
// from the embedded managed-scopes manifest (no embedded client_secret —
// spec §7 line 1220). Refresh tokens are persisted via CredentialVault and
// scoped per spec §10.0.1.
//
// v0.1.0 ships with the protocol code wired but gated by the manifest's
// active_scope_rule: until at least one scope reaches
// (active, verified, ready, passing), Resolve and Login return
// GUM_OAUTH_MANAGED_CLIENT_NOT_READY. Tests override the manifest +
// authorization endpoint to exercise the happy path.

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ehmo/gum/internal/httputil"
)

// GumOAuth is the spec §7 managed-OAuth strategy.
type GumOAuth struct {
	// Vault persists refresh tokens. Required.
	Vault *CredentialVault

	// AuthURL, when non-empty, overrides the Google authorization endpoint.
	// Default https://accounts.google.com/o/oauth2/v2/auth. Tests inject the
	// fake provider's URL here.
	AuthURL string
	// TokenURL, when non-empty, overrides the Google token endpoint.
	// Default https://oauth2.googleapis.com/token.
	TokenURL string
	// HTTPClient is used for token exchange. Default http.DefaultClient.
	HTTPClient *http.Client
	// ManifestBody, when non-nil, overrides the embedded
	// auth-managed-scopes.v1.json manifest. Tests use this to promote a
	// scope without mutating the on-disk manifest.
	ManifestBody []byte
	// BrowserOpener launches the user's browser. Tests inject a function
	// that drives the loopback callback programmatically.
	BrowserOpener func(authURL string) error
	// ClientIDOverride, when non-empty, replaces the manifest's client_id.
	// Tests set this so the fake provider can validate it.
	ClientIDOverride string
	// Now is the clock used for refresh-token freshness checks.
	// Default time.Now.
	Now func() time.Time
}

// NewGumOAuth constructs a GumOAuth wired with the default vault and
// production OAuth endpoints. The manifest gate (see canStartGumOAuth) keeps
// the strategy effectively disabled until a managed scope reaches the
// promoted state.
func NewGumOAuth() *GumOAuth {
	return &GumOAuth{Vault: NewDefaultCredentialVault()}
}

const (
	defaultGumOAuthAuthURL  = "https://accounts.google.com/o/oauth2/v2/auth"
	defaultGumOAuthTokenURL = "https://oauth2.googleapis.com/token"
	gumOAuthStrategyName    = "gum_oauth"
)

// Resolve looks up a stored refresh token for the requested scopes and
// returns a refreshed access token. When no token is stored, returns
// AUTH_LOGIN_REQUIRED pointing the operator at `gum auth login`. Both
// branches first run the manifest gate so a not-yet-promoted scope set
// surfaces GUM_OAUTH_MANAGED_CLIENT_NOT_READY before any vault access.
func (g *GumOAuth) Resolve(ctx context.Context, scopes []string) (*Credentials, error) {
	manifest, err := loadManagedScopesManifest(g.ManifestBody)
	if err != nil {
		return nil, &AuthError{
			Code:             "GUM_OAUTH_MANIFEST_INVALID",
			Strategy:         gumOAuthStrategyName,
			HumanRemediation: err.Error(),
		}
	}
	if err := canStartGumOAuth(manifest, scopes); err != nil {
		return nil, err
	}
	if g.Vault == nil {
		return nil, &AuthError{
			Code:             "AUTH_RESOLVER_NOT_CONFIGURED",
			Strategy:         gumOAuthStrategyName,
			HumanRemediation: "gum_oauth Vault is nil; wire NewGumOAuth() into the resolver",
		}
	}
	fp, err := g.Vault.LookupGumOAuthSubject(scopes)
	if err != nil {
		return nil, err
	}
	if fp == "" {
		return nil, &AuthError{
			Code:             "AUTH_LOGIN_REQUIRED",
			Strategy:         gumOAuthStrategyName,
			SetupCommand:     "gum auth login",
			HumanRemediation: "no stored gum_oauth subject for the requested scopes; run `gum auth login`",
			UserMessage:      "Run `gum auth login` to authorize these scopes.",
			Retryable:        true,
			RequiredScopes:   append([]string{}, scopes...),
		}
	}
	rt, err := g.Vault.LookupRefreshToken(gumOAuthStrategyName, fp, scopes)
	if err != nil {
		return nil, err
	}
	if rt == "" {
		return nil, &AuthError{
			Code:             "AUTH_LOGIN_REQUIRED",
			Strategy:         gumOAuthStrategyName,
			SetupCommand:     "gum auth login",
			HumanRemediation: "no stored gum_oauth refresh token for the requested scopes; run `gum auth login`",
			UserMessage:      "Run `gum auth login` to authorize these scopes.",
			Retryable:        true,
			RequiredScopes:   append([]string{}, scopes...),
		}
	}
	tok, err := g.exchangeRefresh(ctx, manifest, rt)
	if err != nil {
		return nil, err
	}
	return &Credentials{
		Token:              tok.AccessToken,
		ExpiresAt:          g.now().Add(time.Duration(tok.ExpiresIn) * time.Second),
		Scopes:             append([]string{}, scopes...),
		StrategyName:       gumOAuthStrategyName,
		SubjectFingerprint: fp,
	}, nil
}

// Login runs the installed-app PKCE + loopback + CSRF state flow end-to-end:
//  1. Bind a loopback listener on 127.0.0.1:0.
//  2. Generate PKCE verifier+challenge and a 32-byte random state.
//  3. Build the authorization URL and open it via BrowserOpener.
//  4. Wait for the callback, validate the state and absence of error,
//     then exchange the code for tokens.
//  5. Persist the refresh token in the vault.
//
// The manifest gate runs first: scopes that aren't promoted return
// GUM_OAUTH_MANAGED_CLIENT_NOT_READY without touching the network.
func (g *GumOAuth) Login(ctx context.Context, scopes []string) (*Credentials, error) {
	manifest, err := loadManagedScopesManifest(g.ManifestBody)
	if err != nil {
		return nil, &AuthError{
			Code:             "GUM_OAUTH_MANIFEST_INVALID",
			Strategy:         gumOAuthStrategyName,
			HumanRemediation: err.Error(),
		}
	}
	if err := canStartGumOAuth(manifest, scopes); err != nil {
		return nil, err
	}
	if g.Vault == nil {
		return nil, &AuthError{
			Code:             "AUTH_RESOLVER_NOT_CONFIGURED",
			Strategy:         gumOAuthStrategyName,
			HumanRemediation: "gum_oauth Vault is nil; wire NewGumOAuth() into the resolver",
		}
	}
	clientID := g.clientID(manifest)
	if clientID == "" {
		return nil, &AuthError{
			Code:             "GUM_OAUTH_CLIENT_ID_MISSING",
			Strategy:         gumOAuthStrategyName,
			HumanRemediation: "managed-scope manifest does not declare client_id; gum_oauth cannot start the PKCE flow",
		}
	}

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("gum_oauth: bind loopback: %w", err)
	}
	defer func() { _ = lis.Close() }()
	redirectURI := "http://" + lis.Addr().String() + "/oauth/callback"

	state, err := randomURLToken(32)
	if err != nil {
		return nil, err
	}
	verifier, err := randomURLToken(64)
	if err != nil {
		return nil, err
	}
	challenge := pkceS256(verifier)

	authQ := url.Values{
		"client_id":             {clientID},
		"redirect_uri":          {redirectURI},
		"response_type":         {"code"},
		"scope":                 {strings.Join(scopesWithOpenID(scopes), " ")},
		"state":                 {state},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
		"access_type":           {"offline"},
		// Spec §7 + manifest active_scope_rule: scope upgrades force a
		// full re-consent rather than incremental authorization.
		"prompt": {scopeExpansionPrompt(manifest)},
	}
	authURL := g.authURL() + "?" + authQ.Encode()

	cb := newCallbackServer(lis, redirectURI, state, gumOAuthStrategyName)
	go cb.serve()
	defer cb.shutdown()

	opener := g.BrowserOpener
	if opener == nil {
		opener = func(string) error { return nil }
	}
	if err := opener(authURL); err != nil {
		return nil, fmt.Errorf("gum_oauth: open browser: %w", err)
	}

	res, err := cb.await(ctx, 5*time.Minute)
	if err != nil {
		return nil, err
	}

	tok, err := g.exchangeCode(ctx, manifest, res.code, verifier, redirectURI)
	if err != nil {
		return nil, err
	}
	if tok.RefreshToken == "" {
		return nil, &AuthError{
			Code:             "GUM_OAUTH_NO_REFRESH_TOKEN",
			Strategy:         gumOAuthStrategyName,
			HumanRemediation: "token endpoint did not return a refresh_token; ensure prompt=consent and access_type=offline",
		}
	}
	fp, err := managedSubjectFingerprintFromIDToken(tok.IDToken)
	if err != nil {
		return nil, err
	}
	vaultK := vaultKey(gumOAuthStrategyName, fp, scopes)
	if err := g.Vault.StoreRefreshToken(gumOAuthStrategyName, fp, scopes, tok.RefreshToken); err != nil {
		return nil, err
	}
	subjectK := gumOAuthSubjectKey(scopes)
	if err := g.Vault.StoreGumOAuthSubject(scopes, fp); err != nil {
		return nil, err
	}
	// Track the key in the index so RevokeAllGumOAuth (called at logout) can
	// find and purge this entry. Surface a tracking failure — it signals a
	// keychain problem the operator should see.
	if err := g.Vault.TrackGumOAuthKey(vaultK); err != nil {
		return nil, err
	}
	if err := g.Vault.TrackGumOAuthKey(subjectK); err != nil {
		return nil, err
	}
	return &Credentials{
		Token:              tok.AccessToken,
		ExpiresAt:          g.now().Add(time.Duration(tok.ExpiresIn) * time.Second),
		Scopes:             append([]string{}, scopes...),
		StrategyName:       gumOAuthStrategyName,
		SubjectFingerprint: fp,
	}, nil
}

// authURL returns the authorization endpoint, honoring the test override.
func (g *GumOAuth) authURL() string {
	if g.AuthURL != "" {
		return g.AuthURL
	}
	return defaultGumOAuthAuthURL
}

// tokenURL returns the token-exchange endpoint, honoring the test override.
func (g *GumOAuth) tokenURL() string {
	if g.TokenURL != "" {
		return g.TokenURL
	}
	return defaultGumOAuthTokenURL
}

func (g *GumOAuth) httpClient() *http.Client {
	if g.HTTPClient != nil {
		return g.HTTPClient
	}
	return newDefaultAuthHTTPClient()
}

func (g *GumOAuth) now() time.Time {
	if g.Now != nil {
		return g.Now()
	}
	return time.Now()
}

func (g *GumOAuth) clientID(m *managedScopesManifest) string {
	if g.ClientIDOverride != "" {
		return g.ClientIDOverride
	}
	return m.ClientPolicy.ClientID
}

// gumOAuthTokenResponse models the subset of the Google token endpoint response
// the gum_oauth flow consumes. (byooauth.go declares its own tokenResponse
// without refresh_token / error fields; we keep them disjoint to avoid coupling
// the two strategies.)
type gumOAuthTokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	Scope        string `json:"scope"`
	TokenType    string `json:"token_type"`
	IDToken      string `json:"id_token"`
	Error        string `json:"error"`
	ErrorDesc    string `json:"error_description"`
}

func (g *GumOAuth) exchangeCode(ctx context.Context, m *managedScopesManifest, code, verifier, redirectURI string) (*gumOAuthTokenResponse, error) {
	body := url.Values{
		"client_id":     {g.clientID(m)},
		"code":          {code},
		"code_verifier": {verifier},
		"grant_type":    {"authorization_code"},
		"redirect_uri":  {redirectURI},
	}
	return g.postToken(ctx, body)
}

func (g *GumOAuth) exchangeRefresh(ctx context.Context, m *managedScopesManifest, refreshToken string) (*gumOAuthTokenResponse, error) {
	body := url.Values{
		"client_id":     {g.clientID(m)},
		"refresh_token": {refreshToken},
		"grant_type":    {"refresh_token"},
	}
	return g.postToken(ctx, body)
}

func (g *GumOAuth) postToken(ctx context.Context, body url.Values) (*gumOAuthTokenResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.tokenURL(), strings.NewReader(body.Encode()))
	if err != nil {
		return nil, fmt.Errorf("gum_oauth: token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := g.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("gum_oauth: token POST: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	// Cap the response body (mirrors byooauth.go gum-4d66): a misconfigured or
	// hostile token endpoint must not be able to stream an unbounded body into
	// the JSON decoder and exhaust memory.
	raw, readErr := httputil.ReadCapped(resp.Body, 1<<20)
	if readErr != nil {
		return nil, fmt.Errorf("gum_oauth: read token response (%d): %w", resp.StatusCode, readErr)
	}
	var tr gumOAuthTokenResponse
	if err := json.Unmarshal(raw, &tr); err != nil {
		return nil, fmt.Errorf("gum_oauth: decode token response (%d): %w", resp.StatusCode, err)
	}
	if tr.Error != "" || resp.StatusCode >= 400 {
		return nil, &AuthError{
			Code:             "GUM_OAUTH_TOKEN_EXCHANGE_FAILED",
			Strategy:         gumOAuthStrategyName,
			HumanRemediation: fmt.Sprintf("token endpoint returned %d %s: %s", resp.StatusCode, tr.Error, tr.ErrorDesc),
		}
	}
	return &tr, nil
}

// scopeExpansionPrompt maps the manifest's scope_expansion_mode to the
// authorization endpoint's prompt parameter. full_reconsent → consent, the
// only mode v0.1 supports.
func scopeExpansionPrompt(m *managedScopesManifest) string {
	switch m.ClientPolicy.ScopeExpansionMode {
	case "incremental":
		return ""
	default:
		// full_reconsent (and any unknown value) defaults to consent so
		// scope upgrades trigger the full re-consent screen per the
		// active_scope_rule.
		return "consent"
	}
}

func scopesWithOpenID(scopes []string) []string {
	out := append([]string{}, scopes...)
	for _, s := range out {
		if s == "openid" {
			sort.Strings(out)
			return out
		}
	}
	out = append(out, "openid")
	sort.Strings(out)
	return out
}

func managedSubjectFingerprintFromIDToken(idToken string) (string, error) {
	sub, err := idTokenSubject(idToken)
	if err != nil {
		return "", err
	}
	return managedSubjectFingerprintFromSub(sub), nil
}

func managedSubjectFingerprintFromSub(sub string) string {
	h := sha256.Sum256([]byte("gum_oauth:" + sub))
	return base64.RawURLEncoding.EncodeToString(h[:8])
}

func idTokenSubject(idToken string) (string, error) {
	if strings.TrimSpace(idToken) == "" {
		return "", &AuthError{
			Code:             "GUM_OAUTH_ID_TOKEN_MISSING",
			Strategy:         gumOAuthStrategyName,
			HumanRemediation: "token endpoint did not return an id_token; gum_oauth requires OpenID Connect subject identity to partition stored refresh tokens",
		}
	}
	parts := strings.Split(idToken, ".")
	if len(parts) != 3 {
		return "", invalidIDTokenError("expected JWT with three dot-separated parts")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", invalidIDTokenError("decode payload: " + err.Error())
	}
	var claims struct {
		Sub string `json:"sub"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return "", invalidIDTokenError("decode claims: " + err.Error())
	}
	if strings.TrimSpace(claims.Sub) == "" {
		return "", invalidIDTokenError("missing sub claim")
	}
	return claims.Sub, nil
}

func invalidIDTokenError(detail string) error {
	return &AuthError{
		Code:             "GUM_OAUTH_ID_TOKEN_INVALID",
		Strategy:         gumOAuthStrategyName,
		HumanRemediation: "token endpoint returned an unusable id_token: " + detail,
	}
}

// randomURLToken returns a URL-safe random token of n bytes (pre-encoding).
func randomURLToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("gum_oauth: random read: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// pkceS256 computes the S256 PKCE code_challenge from the verifier.
func pkceS256(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// callbackServer is the loopback redirect listener that captures the
// `code` query parameter. It enforces:
//   - the request path matches /oauth/callback
//   - the `state` parameter is present and equals the expected value
//   - no `error` parameter from the provider
//
// Any failure surfaces a typed *AuthError so the caller can forward it
// verbatim in the dispatch envelope.
type callbackServer struct {
	lis         net.Listener
	srv         *http.Server
	redirectURI string
	expectState string
	// strategy labels AuthError.Strategy on callback failures. The server is
	// shared by GumOAuth.Login ("gum_oauth") and ByoOAuth.Login ("byo_oauth"),
	// so error routing/remediation keying on Strategy must see the real one.
	strategy string
	once     sync.Once
	done     chan callbackResult
}

type callbackResult struct {
	code string
	err  error
}

func newCallbackServer(lis net.Listener, redirectURI, expectState, strategy string) *callbackServer {
	cb := &callbackServer{
		lis:         lis,
		redirectURI: redirectURI,
		expectState: expectState,
		strategy:    strategy,
		done:        make(chan callbackResult, 1),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/oauth/callback", cb.handle)
	cb.srv = &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	return cb
}

func (cb *callbackServer) serve() {
	_ = cb.srv.Serve(cb.lis)
}

func (cb *callbackServer) shutdown() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = cb.srv.Shutdown(ctx)
}

// handle validates the callback query parameters and emits exactly one
// result on cb.done. Subsequent callbacks (e.g. browser refresh) are
// ignored so an attacker can't race a second callback through.
func (cb *callbackServer) handle(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	finish := func(res callbackResult) {
		cb.once.Do(func() {
			cb.done <- res
		})
	}
	if got := q.Get("state"); got != cb.expectState {
		http.Error(w, "state mismatch", http.StatusBadRequest)
		finish(callbackResult{err: &AuthError{
			Code:             "AUTH_LOOPBACK_STATE_MISMATCH",
			Strategy:         cb.strategy,
			HumanRemediation: "callback `state` parameter missing or did not match the value GUM issued; aborting to prevent CSRF",
		}})
		return
	}
	if errCode := q.Get("error"); errCode != "" {
		http.Error(w, "auth error", http.StatusBadRequest)
		finish(callbackResult{err: &AuthError{
			Code:             "GUM_OAUTH_USER_DENIED",
			Strategy:         cb.strategy,
			HumanRemediation: "user declined authorization at the consent screen: " + errCode + ": " + q.Get("error_description"),
		}})
		return
	}
	code := q.Get("code")
	if code == "" {
		http.Error(w, "missing code", http.StatusBadRequest)
		finish(callbackResult{err: &AuthError{
			Code:             "GUM_OAUTH_CALLBACK_INVALID",
			Strategy:         cb.strategy,
			HumanRemediation: "callback URL has no `code` parameter",
		}})
		return
	}
	_, _ = w.Write([]byte("Authorization complete. You can close this tab."))
	finish(callbackResult{code: code})
}

// await blocks until the callback fires or the context / timeout elapses.
func (cb *callbackServer) await(ctx context.Context, timeout time.Duration) (*callbackResult, error) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case res := <-cb.done:
		if res.err != nil {
			return nil, res.err
		}
		return &res, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-timer.C:
		return nil, &AuthError{
			Code: "GUM_OAUTH_TIMEOUT",
			// Use cb.strategy (like the other arms), not the gum_oauth constant —
			// a byo_oauth login uses this same callback server, so a hardcoded
			// "gum_oauth" mislabels a byo timeout and misroutes remediation.
			Strategy:         cb.strategy,
			HumanRemediation: "no callback received within " + strconv.Itoa(int(timeout.Seconds())) + "s; aborting",
		}
	}
}

// Revoke purges all locally-stored gum_oauth refresh tokens from the
// CredentialVault, using the tracked index (vault.go gumOAuthVaultIndexKey) to
// find every vault entry written by prior Login calls. This is a local-only
// operation: the managed PKCE client has no embedded client_secret, so
// server-side token revocation is not possible. Revoke is independent of the
// manifest gate so it is always safe to call during `gum logout`.
func (g *GumOAuth) Revoke() error {
	if g.Vault == nil {
		return nil
	}
	return g.Vault.RevokeAllGumOAuth()
}

// Compile-time check that GumOAuth satisfies Resolver. Login is the
// imperative entry point, not part of the Resolve(ctx, scopes) contract.
var _ Resolver = (*GumOAuth)(nil)

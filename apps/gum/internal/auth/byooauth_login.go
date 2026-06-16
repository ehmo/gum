package auth

// byooauth_login.go adds the interactive authorization-code half of the
// byo_oauth strategy: the installed-app PKCE + loopback + CSRF-state flow that
// obtains the FIRST refresh token. byooauth.go's Acquire/Resolve then refresh
// that token silently on every later call. Unlike gum_oauth this runs against
// a user-supplied client (ByoOAuthConfig.ClientID/Secret) with NO managed-scope
// manifest gate — it is the user's own OAuth client — and never touches gcloud.
// The loopback machinery (callbackServer, randomURLToken, pkceS256) is shared
// with gum_oauth.go in this package.

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ehmo/gum/internal/httputil"
)

// authEndpoint returns the authorization endpoint, honoring the test override.
func (b *ByoOAuth) authEndpoint() string {
	if b.cfg.AuthEndpoint != "" {
		return b.cfg.AuthEndpoint
	}
	return defaultGumOAuthAuthURL
}

// Login runs the installed-app PKCE + loopback + CSRF flow against the
// user-supplied OAuth client, persists the resulting refresh token under the
// scope-keyed vault entry Acquire/Resolve read, and returns the access token.
// It opens the browser via BrowserOpener (nil = do not open). The flow is
// gated only by the presence of a client_id and at least one scope; there is
// no managed-manifest gate because this is the operator's own client.
func (b *ByoOAuth) Login(ctx context.Context) (*Credentials, error) {
	if strings.TrimSpace(b.cfg.ClientID) == "" {
		return nil, &AuthError{
			Code:             "BYO_OAUTH_CLIENT_NOT_CONFIGURED",
			Strategy:         "byo_oauth",
			SetupCommand:     "gum auth use-oauth-client",
			HumanRemediation: "no OAuth client configured; run `gum auth use-oauth-client --client-id <id>` (create a Desktop client in the Google Cloud console)",
			UserMessage:      "Run `gum auth use-oauth-client` to register your Google OAuth client.",
		}
	}
	if len(b.cfg.Scopes) == 0 {
		return nil, &AuthError{
			Code:             "BYO_OAUTH_NO_SCOPES",
			Strategy:         "byo_oauth",
			HumanRemediation: "no scopes requested; nothing to authorize",
		}
	}

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("byo_oauth: bind loopback: %w", err)
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
		"client_id":             {b.cfg.ClientID},
		"redirect_uri":          {redirectURI},
		"response_type":         {"code"},
		"scope":                 {strings.Join(b.cfg.Scopes, " ")},
		"state":                 {state},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
		"access_type":           {"offline"},
		// Incremental authorization: the issued tokens cover the union of all
		// scopes this user has previously granted to the client, so authorizing
		// a second operation does not invalidate an earlier grant (gum-ergy).
		"include_granted_scopes": {"true"},
		// select_account shows the Google account chooser so an operator can
		// switch accounts (e.g. after `gum logout`); without it Google silently
		// reuses the single signed-in session and account switching is
		// impossible. consent forces the consent screen so Google still returns
		// a refresh_token even if this client was previously authorized.
		"prompt": {"select_account consent"},
	}
	authURL := b.authEndpoint() + "?" + authQ.Encode()

	cb := newCallbackServer(lis, redirectURI, state, "byo_oauth")
	go cb.serve()
	defer cb.shutdown()

	opener := b.BrowserOpener
	if opener == nil {
		opener = func(string) error { return nil }
	}
	if err := opener(authURL); err != nil {
		return nil, fmt.Errorf("byo_oauth: open browser: %w", err)
	}

	res, err := cb.await(ctx, 5*time.Minute)
	if err != nil {
		return nil, err
	}

	tok, err := b.exchangeAuthCode(ctx, res.code, verifier, redirectURI)
	if err != nil {
		return nil, err
	}
	if tok.RefreshToken == "" {
		return nil, &AuthError{
			Code:             "BYO_OAUTH_NO_REFRESH_TOKEN",
			Strategy:         "byo_oauth",
			HumanRemediation: "token endpoint returned no refresh_token; ensure the consent screen completed (access_type=offline, prompt=consent)",
		}
	}
	if err := b.storeLoginGrant(tok.RefreshToken, tok.Scope); err != nil {
		return nil, err
	}
	creds := &Credentials{
		Token:              tok.AccessToken,
		ExpiresAt:          time.Now().Add(time.Duration(tok.ExpiresIn) * time.Second),
		Scopes:             append([]string{}, b.cfg.Scopes...),
		StrategyName:       "byo_oauth",
		SubjectFingerprint: DeriveSubjectFingerprint("byo_oauth:" + tok.RefreshToken),
	}
	b.cached = creds
	return creds, nil
}

// exchangeAuthCode trades the authorization code for tokens at the token
// endpoint. The PKCE verifier is always sent; client_secret is included only
// when the operator configured one (public PKCE clients omit it).
func (b *ByoOAuth) exchangeAuthCode(ctx context.Context, code, verifier, redirectURI string) (*gumOAuthTokenResponse, error) {
	form := url.Values{
		"client_id":     {b.cfg.ClientID},
		"code":          {code},
		"code_verifier": {verifier},
		"grant_type":    {"authorization_code"},
		"redirect_uri":  {redirectURI},
	}
	if b.cfg.ClientSecret != "" {
		form.Set("client_secret", b.cfg.ClientSecret)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, b.cfg.TokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("byo_oauth: token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := b.client.Do(req)
	if err != nil {
		return nil, &AuthError{
			Code:             "BYO_OAUTH_TOKEN_EXCHANGE_FAILED",
			Strategy:         "byo_oauth",
			HumanRemediation: fmt.Sprintf("token POST failed: %v", err),
		}
	}
	defer func() { _ = resp.Body.Close() }()

	body, readErr := httputil.ReadCapped(resp.Body, 1<<20)
	if readErr != nil {
		return nil, &AuthError{
			Code:             "BYO_OAUTH_TOKEN_EXCHANGE_FAILED",
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
			Code:             "BYO_OAUTH_TOKEN_EXCHANGE_FAILED",
			Strategy:         "byo_oauth",
			HumanRemediation: rem,
		}
	}

	var tr gumOAuthTokenResponse
	if err := json.Unmarshal(body, &tr); err != nil {
		return nil, &AuthError{
			Code:             "BYO_OAUTH_TOKEN_EXCHANGE_FAILED",
			Strategy:         "byo_oauth",
			HumanRemediation: fmt.Sprintf("decode token response: %v", err),
		}
	}
	if tr.Error != "" {
		return nil, &AuthError{
			Code:             "BYO_OAUTH_TOKEN_EXCHANGE_FAILED",
			Strategy:         "byo_oauth",
			HumanRemediation: fmt.Sprintf("token endpoint error: %s: %s", tr.Error, tr.ErrorDesc),
		}
	}
	return &tr, nil
}

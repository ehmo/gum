package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"

	"golang.org/x/oauth2/google"
)

// EnvServiceAccountKeyVar is the env variable the v0.1.0 service_account
// resolver reads for the JSON key file path. Spec §7 line 1284 mandates
// keychain storage; until gum-0wv lands, the env var is the documented
// divergence (see docs/known-divergences.md). Distinct from
// GOOGLE_APPLICATION_CREDENTIALS so a single host machine can run both
// ADC-backed and SA-backed gum profiles without env crosstalk.
const EnvServiceAccountKeyVar = "GUM_SERVICE_ACCOUNT_KEY"

// ServiceAccountResolver resolves service_account_key credentials by
// loading a Google Cloud service account JSON key file and exchanging the
// signed JWT assertion for an OAuth2 access token via Google's token
// endpoint. Spec §7: service_account is the "long-lived key.json" flow.
//
// Construction validates the key file shape and caches the parsed JWT
// config; Resolve issues one token exchange per Resolve call (the
// underlying oauth2 token source caches until expiry, but we re-bind to
// the requested scopes each time because variants in the catalog declare
// different scope sets).
type ServiceAccountResolver struct {
	// KeyPath is the absolute or relative path to the service-account
	// JSON key file. Populated by NewServiceAccountResolver.
	KeyPath string
	// keyBytes caches the parsed JSON so Resolve does not re-read.
	keyBytes []byte
	// clientEmail is parsed from the JSON for SubjectFingerprint derivation.
	clientEmail string
	// TokenURLOverride lets tests redirect the token exchange away from
	// Google's edge to an httptest server. Empty means use the JSON's
	// token_uri (Google's default oauth2 endpoint).
	TokenURLOverride string
}

// NewServiceAccountResolver constructs a resolver from the JSON key file
// at path. The file MUST contain a `type=service_account` JSON document
// matching Google Cloud's downloaded key shape; anything else surfaces
// AUTH_SA_KEY_INVALID.
//
// The constructor reads the file synchronously so an invalid path or
// malformed JSON fails fast at wiring time instead of at first dispatch.
func NewServiceAccountResolver(path string) (*ServiceAccountResolver, error) {
	if strings.TrimSpace(path) == "" {
		return nil, &AuthError{
			Code:             "AUTH_SA_KEY_NOT_CONFIGURED",
			Strategy:         "service_account_key",
			HumanRemediation: "set the GUM_SERVICE_ACCOUNT_KEY env variable to the path of a service-account JSON key file",
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, &AuthError{
				Code:             "AUTH_SA_KEY_NOT_FOUND",
				Strategy:         "service_account_key",
				HumanRemediation: fmt.Sprintf("service-account key file %q does not exist; download a new key from Google Cloud Console > IAM > Service Accounts", path),
			}
		}
		return nil, &AuthError{
			Code:             "AUTH_SA_KEY_UNREADABLE",
			Strategy:         "service_account_key",
			HumanRemediation: fmt.Sprintf("could not read service-account key %q: %v", path, err),
		}
	}
	// Probe with empty scopes — the JSON shape check fails the same way
	// regardless of scopes, and we'll re-bind scopes per-Resolve.
	cfg, parseErr := google.JWTConfigFromJSON(data)
	if parseErr != nil {
		return nil, &AuthError{
			Code:             "AUTH_SA_KEY_INVALID",
			Strategy:         "service_account_key",
			HumanRemediation: fmt.Sprintf("service-account key %q is not a valid Google service-account JSON: %v", path, parseErr),
		}
	}
	if cfg.Email == "" {
		return nil, &AuthError{
			Code:             "AUTH_SA_KEY_INVALID",
			Strategy:         "service_account_key",
			HumanRemediation: fmt.Sprintf("service-account key %q has no client_email; download a fresh key", path),
		}
	}
	return &ServiceAccountResolver{
		KeyPath:     path,
		keyBytes:    data,
		clientEmail: cfg.Email,
	}, nil
}

// Resolve mints a Google access token for the requested scopes using the
// cached JSON key. Empty scopes are valid — the token will carry the
// service account's default scope set per Google's default behavior.
func (r *ServiceAccountResolver) Resolve(ctx context.Context, scopes []string) (*Credentials, error) {
	if r == nil || len(r.keyBytes) == 0 {
		return nil, &AuthError{
			Code:             "AUTH_SA_KEY_NOT_CONFIGURED",
			Strategy:         "service_account_key",
			HumanRemediation: "construct ServiceAccountResolver via NewServiceAccountResolver(path) before calling Resolve",
		}
	}
	cfg, err := google.JWTConfigFromJSON(r.keyBytes, scopes...)
	if err != nil {
		return nil, &AuthError{
			Code:             "AUTH_SA_KEY_INVALID",
			Strategy:         "service_account_key",
			HumanRemediation: fmt.Sprintf("service-account key reparse failed: %v", err),
		}
	}
	if r.TokenURLOverride != "" {
		cfg.TokenURL = r.TokenURLOverride
	}
	tok, err := cfg.TokenSource(ctx).Token()
	if err != nil {
		return nil, &AuthError{
			Code:             "AUTH_SA_TOKEN_EXCHANGE_FAILED",
			Strategy:         "service_account_key",
			HumanRemediation: fmt.Sprintf("service-account token exchange failed: %v", err),
		}
	}
	return &Credentials{
		Token:              tok.AccessToken,
		ExpiresAt:          tok.Expiry,
		Scopes:             append([]string{}, scopes...),
		StrategyName:       "service_account_key",
		SubjectFingerprint: serviceAccountFingerprint(r.clientEmail),
	}, nil
}

// serviceAccountFingerprint produces the spec §10.0.1 SubjectFingerprint
// from the SA client_email. Email is non-secret but already a stable
// per-principal identifier; hashing keeps the audit log shape consistent
// with the other resolvers (which all emit hex digests).
func serviceAccountFingerprint(email string) string {
	sum := sha256.Sum256([]byte(email))
	return hex.EncodeToString(sum[:])
}

// NewServiceAccountResolverFromEnv reads GUM_SERVICE_ACCOUNT_KEY and
// constructs a resolver from the path it names. Returns
// AUTH_SA_KEY_NOT_CONFIGURED when the env var is unset so the CLI can
// distinguish "operator forgot to configure" from "config is malformed".
func NewServiceAccountResolverFromEnv() (*ServiceAccountResolver, error) {
	return NewServiceAccountResolver(strings.TrimSpace(os.Getenv(EnvServiceAccountKeyVar)))
}

var _ Resolver = (*ServiceAccountResolver)(nil)

package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"golang.org/x/oauth2/google"
)

// LiveADCResolver fetches a real Bearer token from the ambient Application
// Default Credentials chain using golang.org/x/oauth2/google. Unlike
// ADCResolver, which returns stub tokens for testing the priority chain, this
// resolver returns a token that can be used to authenticate live Google API
// requests.
//
// Scopes must be the fully qualified URL form (e.g.
// "https://www.googleapis.com/auth/gmail.readonly"); ScopeURL helps construct
// these from the catalog's short scope identifiers.
type LiveADCResolver struct{}

// NewLiveADCResolver constructs a LiveADCResolver. The struct is empty today
// but the constructor is kept so callers can add knobs later without churn.
func NewLiveADCResolver() *LiveADCResolver { return &LiveADCResolver{} }

// Resolve fetches an OAuth2 access token for scopes from the ambient ADC chain.
func (r *LiveADCResolver) Resolve(ctx context.Context, scopes []string) (*Credentials, error) {
	urlScopes := normaliseScopes(scopes)
	creds, err := google.FindDefaultCredentials(ctx, urlScopes...)
	if err != nil {
		return nil, &AuthError{
			Code:             "NO_ADC_CREDENTIALS",
			Strategy:         "adc",
			HumanRemediation: fmt.Sprintf("run `gcloud auth application-default login` or set GOOGLE_APPLICATION_CREDENTIALS. Underlying error: %v", err),
		}
	}
	tok, err := creds.TokenSource.Token()
	if err != nil {
		return nil, &AuthError{
			Code:             "ADC_TOKEN_FETCH_FAILED",
			Strategy:         "adc",
			HumanRemediation: fmt.Sprintf("the ADC credentials were found but token exchange failed: %v", err),
		}
	}
	expiry := tok.Expiry
	if expiry.IsZero() {
		expiry = time.Now().Add(45 * time.Minute)
	}
	return &Credentials{
		Token:              tok.AccessToken,
		ExpiresAt:          expiry,
		Scopes:             urlScopes,
		StrategyName:       "adc",
		QuotaProjectID:     extractQuotaProject(creds.JSON),
		SubjectFingerprint: DeriveSubjectFingerprint("adc-live:" + extractSubject(creds.JSON)),
	}, nil
}

// extractSubject pulls a per-principal identifier out of an ADC JSON blob.
// For user credentials this is the refresh_token (rotated by gcloud per user);
// for service-account keys it is client_email. Falls back to the raw JSON if
// neither is present so distinct files still hash to distinct fingerprints.
func extractSubject(adcJSON []byte) string {
	if len(adcJSON) == 0 {
		return ""
	}
	var parsed struct {
		ClientEmail  string `json:"client_email"`
		RefreshToken string `json:"refresh_token"`
		ClientID     string `json:"client_id"`
	}
	if err := json.Unmarshal(adcJSON, &parsed); err != nil {
		return string(adcJSON)
	}
	switch {
	case parsed.ClientEmail != "":
		return parsed.ClientEmail
	case parsed.RefreshToken != "":
		return parsed.RefreshToken
	case parsed.ClientID != "":
		return parsed.ClientID
	default:
		return string(adcJSON)
	}
}

// extractQuotaProject reads the quota_project_id field from the ADC JSON if
// present. Falls back to the GOOGLE_CLOUD_QUOTA_PROJECT env var so callers can
// override without rewriting the ADC file.
//
// Both gcloud's `auth application-default set-quota-project` and service-account
// JSON formats may include this field. An empty return is fine — adapters will
// simply omit the X-Goog-User-Project header.
func extractQuotaProject(adcJSON []byte) string {
	if env := os.Getenv("GOOGLE_CLOUD_QUOTA_PROJECT"); env != "" {
		return env
	}
	if len(adcJSON) == 0 {
		return ""
	}
	var parsed struct {
		QuotaProjectID string `json:"quota_project_id"`
	}
	if err := json.Unmarshal(adcJSON, &parsed); err != nil {
		return ""
	}
	return parsed.QuotaProjectID
}

// NormaliseScopes is the exported form of normaliseScopes, used by the CLI to
// expand operator-supplied `--scope` short forms (e.g. "gmail.readonly") into
// the fully-qualified URLs Google's OAuth endpoints require.
func NormaliseScopes(scopes []string) []string {
	return normaliseScopes(scopes)
}

// normaliseScopes returns scopes in their fully-qualified URL form. Catalog
// scopes are stored as short identifiers like "gmail.readonly"; the oauth2
// library wants full URLs like
// "https://www.googleapis.com/auth/gmail.readonly".
func normaliseScopes(scopes []string) []string {
	const prefix = "https://www.googleapis.com/auth/"
	out := make([]string, 0, len(scopes))
	for _, s := range scopes {
		if s == "" {
			continue
		}
		if strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://") {
			// Already a full-URL scope — pass through unprefixed. (HasPrefix avoids
			// the old `len(s) >= 8` guard, which mis-handled a bare 7-char "http://"
			// and risked an index panic if "loosened" to >= 7.)
			out = append(out, s)
			continue
		}
		out = append(out, prefix+s)
	}
	return out
}

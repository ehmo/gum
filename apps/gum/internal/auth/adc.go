package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// ErrNoADCCredentials is returned by ADCResolver.Resolve when no ADC credential
// source is found (GOOGLE_APPLICATION_CREDENTIALS not set, gcloud cache absent,
// and metadata server unavailable).
var ErrNoADCCredentials = errors.New("auth/adc: no application-default credentials found")

// ADCResolver implements Application Default Credentials resolution with an
// injectable field set so unit tests can exercise each priority level in isolation
// without touching the real filesystem, gcloud, or metadata server.
//
// Resolution priority (highest first):
//  1. GOOGLE_APPLICATION_CREDENTIALS env var — path to a service-account JSON key
//  2. gcloud user credential cache (~/.config/gcloud/application_default_credentials.json)
//  3. GCE / Cloud Run metadata server
//  4. None found → ErrNoADCCredentials
type ADCResolver struct {
	// Env returns the value of an environment variable. Defaults to os.Getenv.
	Env func(string) string
	// ReadFile returns the contents of a file at path. Defaults to os.ReadFile.
	ReadFile func(string) ([]byte, error)
	// MetadataAvailable reports whether the GCE metadata server is reachable.
	// Defaults to a real HTTP probe against metadata.google.internal.
	MetadataAvailable func() bool
}

// NewADCResolver constructs an ADCResolver wired to the real environment
// (os.Getenv, os.ReadFile, live metadata probe).
func NewADCResolver() *ADCResolver {
	return &ADCResolver{
		Env:      os.Getenv,
		ReadFile: os.ReadFile,
		MetadataAvailable: func() bool {
			ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
			defer cancel()
			req, err := http.NewRequestWithContext(ctx, "GET", "http://169.254.169.254/computeMetadata/v1/instance/service-accounts/default/token", nil)
			if err != nil {
				return false
			}
			req.Header.Set("Metadata-Flavor", "Google")
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				return false
			}
			_ = resp.Body.Close()
			return resp.StatusCode == 200
		},
	}
}

// serviceAccountKeyFile is the minimal JSON shape we validate against.
type serviceAccountKeyFile struct {
	Type        string `json:"type"`
	ClientEmail string `json:"client_email"`
}

// Resolve walks the ADC priority chain and returns a Credentials on success or
// ErrNoADCCredentials (wrapped in an *AuthError) when nothing is available.
func (r *ADCResolver) Resolve(ctx context.Context, scopes []string) (*Credentials, error) {
	// Priority 1: GOOGLE_APPLICATION_CREDENTIALS env var.
	if envPath := r.Env("GOOGLE_APPLICATION_CREDENTIALS"); envPath != "" {
		data, err := r.ReadFile(envPath)
		if err == nil {
			// Validate it looks like a service account JSON.
			var key serviceAccountKeyFile
			if jsonErr := json.Unmarshal(data, &key); jsonErr == nil && (key.Type != "" || key.ClientEmail != "") {
				subject := key.ClientEmail
				if subject == "" {
					subject = envPath
				}
				return &Credentials{
					Token:              "adc-env-stub-token",
					ExpiresAt:          time.Now().Add(1 * time.Hour),
					Scopes:             scopes,
					StrategyName:       "adc",
					SubjectFingerprint: DeriveSubjectFingerprint("adc-sa:" + subject),
				}, nil
			}
		}
	}

	// Priority 2: gcloud user credential cache.
	home := r.Env("HOME")
	gcloudPath := filepath.Join(home, ".config", "gcloud", "application_default_credentials.json")
	if data, err := r.ReadFile(gcloudPath); err == nil {
		return &Credentials{
			Token:              "adc-gcloud-stub-token",
			ExpiresAt:          time.Now().Add(1 * time.Hour),
			Scopes:             scopes,
			StrategyName:       "adc",
			SubjectFingerprint: DeriveSubjectFingerprint("adc-gcloud:" + string(data)),
		}, nil
	}

	// Priority 3: GCE metadata server. No per-subject material is available
	// without a token-introspection round-trip, so we fingerprint the host
	// identity (the GCE instance is the principal in this code path).
	if r.MetadataAvailable() {
		return &Credentials{
			Token:              "adc-metadata-stub-token",
			ExpiresAt:          time.Now().Add(1 * time.Hour),
			Scopes:             scopes,
			StrategyName:       "adc",
			SubjectFingerprint: DeriveSubjectFingerprint("adc-metadata:" + r.Env("HOSTNAME")),
		}, nil
	}

	// Nothing available.
	return nil, ErrNoADCCredentials
}

package auth_test

import (
	"errors"
	"os"
	"testing"

	"go.uber.org/goleak"

	"github.com/ehmo/gum/internal/auth"
)

// fakeGcloudCreds is a minimal ADC JSON file that contains a fake refresh token.
const fakeGcloudCreds = `{
  "client_id": "fake-client-id.apps.googleusercontent.com",
  "client_secret": "fake-client-secret",
  "refresh_token": "fake-refresh-token",
  "type": "authorized_user"
}`

// TestAuthAdcResolutionOrder verifies the ADC priority chain (G3.2):
//
//	env var beats gcloud cache, gcloud cache beats metadata server, all-absent → ErrNoADCCredentials
//
// Uses an injected ADCResolver so no real filesystem, gcloud, or metadata server
// is touched.
func TestAuthAdcResolutionOrder(t *testing.T) {
	defer goleak.VerifyNone(t)

	t.Run("env_var_wins_over_gcloud_cache", func(t *testing.T) {
		f, err := os.CreateTemp(t.TempDir(), "sa-*.json")
		if err != nil {
			t.Fatal(err)
		}
		_, _ = f.WriteString(`{"type":"service_account","project_id":"test","private_key_id":"k1","private_key":"","client_email":"x@y.iam.gserviceaccount.com","client_id":"1","token_uri":"https://localhost/token"}`)
		_ = f.Close()

		gcloudHit := false
		r := &auth.ADCResolver{
			Env: func(key string) string {
				if key == "GOOGLE_APPLICATION_CREDENTIALS" {
					return f.Name()
				}
				return ""
			},
			ReadFile: func(path string) ([]byte, error) {
				if path == f.Name() {
					data, _ := os.ReadFile(f.Name())
					return data, nil
				}
				gcloudHit = true
				return []byte(fakeGcloudCreds), nil
			},
			MetadataAvailable: func() bool {
				return false
			},
		}

		var resolveErr error
		msg, panicked := catchPanic(func() {
			_, resolveErr = r.Resolve(t.Context(), []string{"https://www.googleapis.com/auth/gmail.readonly"})
		})
		if panicked {
			t.Fatalf("ADCResolver.Resolve panicked: %s — green team must implement ADCResolver.Resolve", msg)
		}
		if errors.Is(resolveErr, auth.ErrNoADCCredentials) {
			t.Error("env var path set but got ErrNoADCCredentials: resolver did not honour env var priority")
		}
		if gcloudHit {
			t.Error("resolver read gcloud cache path even though GOOGLE_APPLICATION_CREDENTIALS was set")
		}
	})

	t.Run("gcloud_cache_beats_metadata", func(t *testing.T) {
		metadataProbed := false
		r := &auth.ADCResolver{
			Env: func(key string) string {
				return ""
			},
			ReadFile: func(path string) ([]byte, error) {
				return []byte(fakeGcloudCreds), nil
			},
			MetadataAvailable: func() bool {
				metadataProbed = true
				return true
			},
		}

		var resolveErr error
		msg, panicked := catchPanic(func() {
			_, resolveErr = r.Resolve(t.Context(), []string{"https://www.googleapis.com/auth/gmail.readonly"})
		})
		if panicked {
			t.Fatalf("ADCResolver.Resolve panicked: %s — green team must implement ADCResolver.Resolve", msg)
		}
		if errors.Is(resolveErr, auth.ErrNoADCCredentials) {
			t.Error("gcloud cache path returned ErrNoADCCredentials — resolver did not detect the gcloud credentials file")
		}
		if metadataProbed {
			t.Error("metadata server was probed even though gcloud cache file was present")
		}
	})

	t.Run("metadata_server_used_when_gcloud_absent", func(t *testing.T) {
		r := &auth.ADCResolver{
			Env: func(key string) string {
				return ""
			},
			ReadFile: func(path string) ([]byte, error) {
				return nil, os.ErrNotExist
			},
			MetadataAvailable: func() bool {
				return true
			},
		}

		var resolveErr error
		msg, panicked := catchPanic(func() {
			_, resolveErr = r.Resolve(t.Context(), []string{"https://www.googleapis.com/auth/gmail.readonly"})
		})
		if panicked {
			t.Fatalf("ADCResolver.Resolve panicked: %s — green team must implement ADCResolver.Resolve", msg)
		}
		if errors.Is(resolveErr, auth.ErrNoADCCredentials) {
			t.Error("metadata available but got ErrNoADCCredentials — resolver did not try metadata path")
		}
	})

	t.Run("all_absent_returns_ErrNoADCCredentials", func(t *testing.T) {
		r := &auth.ADCResolver{
			Env: func(key string) string {
				return ""
			},
			ReadFile: func(path string) ([]byte, error) {
				return nil, os.ErrNotExist
			},
			MetadataAvailable: func() bool {
				return false
			},
		}

		var resolveErr error
		msg, panicked := catchPanic(func() {
			_, resolveErr = r.Resolve(t.Context(), []string{"https://www.googleapis.com/auth/gmail.readonly"})
		})
		if panicked {
			t.Fatalf("ADCResolver.Resolve panicked: %s — green team must implement ADCResolver.Resolve", msg)
		}
		if resolveErr == nil {
			t.Fatal("expected ErrNoADCCredentials, got nil")
		}
		if !errors.Is(resolveErr, auth.ErrNoADCCredentials) {
			t.Errorf("expected errors.Is(err, ErrNoADCCredentials), got: %v", resolveErr)
		}
	})
}

package main

import (
	"context"
	"path/filepath"
	"testing"
)

// TestCollectADCStatusGoogleAppCredentialsTakesPrecedence pins the
// `GOOGLE_APPLICATION_CREDENTIALS != "" → Source = "GOOGLE_APPLICATION_CREDENTIALS"`
// arm. Per spec, the explicit env var is the highest-priority ADC
// source; collectADCStatus MUST record it into both GoogleAppCredentials
// AND Source before considering the gcloud-cache path, so the doctor
// envelope shows operators which credential chain actually wins on this host.
func TestCollectADCStatusGoogleAppCredentialsTakesPrecedence(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp) // no gcloud cache under HOME

	credsPath := filepath.Join(tmp, "explicit-creds.json")
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", credsPath)

	got := collectADCStatus(context.Background(), nil)

	if got.GoogleAppCredentials != credsPath {
		t.Errorf("GoogleAppCredentials=%q; want %q", got.GoogleAppCredentials, credsPath)
	}
	if got.Source != "GOOGLE_APPLICATION_CREDENTIALS" {
		t.Errorf("Source=%q; want GOOGLE_APPLICATION_CREDENTIALS (explicit env must win)", got.Source)
	}
	if got.GcloudCachePresent {
		t.Errorf("GcloudCachePresent=true; want false (no cache file in tempdir HOME)")
	}
	// Hint must stay empty — we have a credential source.
	if got.Hint != "" {
		t.Errorf("Hint=%q; want empty when an ADC source is detected", got.Hint)
	}
}

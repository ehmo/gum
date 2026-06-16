package main

import (
	"context"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// TestDoctorAuthNoCredentialSourceReportsFailure pins the no-credential branch:
// when no BYO grant, API key, service-account key, or ADC source is detectable,
// doctor reports auth as failing and points at the v1 BYO OAuth setup path.
func TestDoctorAuthNoCredentialSourceReportsFailure(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", "")
	t.Setenv("GUM_API_KEY", "")
	t.Setenv("GUM_SERVICE_ACCOUNT_KEY", "")

	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())

	got := doctorAuth(cmd)
	if got.OK {
		t.Errorf("OK=true with no credentials available; want false")
	}
	if got.Name != "auth" {
		t.Errorf("Name=%q; want auth", got.Name)
	}
	if got.Summary != "no local credential source detected" {
		t.Errorf("Summary=%q; want 'no local credential source detected'", got.Summary)
	}
	if got.Hint == "" || !strings.Contains(got.Hint, "gum auth use-oauth-client") {
		t.Errorf("Hint=%q; want BYO OAuth setup command", got.Hint)
	}
}

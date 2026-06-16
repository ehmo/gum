package auth

import (
	"os"
	"path/filepath"
	"testing"
)

// TestNewDefaultCompositeResolverWiresSAWhenEnvValid pins the
// `if sa, err := NewServiceAccountResolverFromEnv(); err == nil →
// c.SA = sa` arm. The composite resolver is the production entry
// point used by `gum auth use-service-account`; a valid SA key file
// pointed at by GUM_SERVICE_ACCOUNT_KEY MUST be wired into c.SA at
// construction so dispatch can later route auth_strategy=
// service_account_key requests through it. Without this arm the
// operator would set the env var, see no error at startup, but then
// hit AUTH_RESOLVER_NOT_CONFIGURED at dispatch — a confusing surface.
func TestNewDefaultCompositeResolverWiresSAWhenEnvValid(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "stub-sa.json")
	// Use the package-internal stub key generator (test-only helper
	// from strategy_service_account_test.go) — same package, so we
	// can reuse it without exporting anything.
	keyJSON := makeStubServiceAccountJSON(t, "stub@stub-project.iam.gserviceaccount.com", "https://oauth2.googleapis.com/token")
	if err := os.WriteFile(keyPath, keyJSON, 0o600); err != nil {
		t.Fatalf("write stub key: %v", err)
	}
	t.Setenv(EnvServiceAccountKeyVar, keyPath)

	c := NewDefaultCompositeResolver()
	if c == nil {
		t.Fatal("NewDefaultCompositeResolver returned nil")
	}
	if c.SA == nil {
		t.Error("c.SA is nil despite valid GUM_SERVICE_ACCOUNT_KEY; want SA resolver wired")
	}
}

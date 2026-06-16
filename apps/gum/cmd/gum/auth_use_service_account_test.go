package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/auth"
)

// TestAuthUseServiceAccountMissingArg locks the cobra ExactArgs(1) gate —
// the operator must point at a key file; running the bare subcommand is a
// usage error.
func TestAuthUseServiceAccountMissingArg(t *testing.T) {
	cmd := newAuthUseServiceAccountCmd()
	cmd.SetArgs([]string{})
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error for missing argument")
	}
}

// TestAuthUseServiceAccountStatFail covers the cannot-stat branch: a path
// the OS refuses to read surfaces a wrapped "gum auth use-service-account"
// error.
func TestAuthUseServiceAccountStatFail(t *testing.T) {
	cmd := newAuthUseServiceAccountCmd()
	cmd.SetArgs([]string{"/definitely/not/a/real/sa-key.json"})
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for missing key file")
	}
	if !strings.Contains(err.Error(), "gum auth use-service-account") {
		t.Errorf("err=%q missing command prefix", err)
	}
}

// TestAuthUseServiceAccountInvalidJSON locks the parse-error path: a file
// that exists but isn't a valid SA key surfaces the constructor's typed
// error wrapped with the command context.
func TestAuthUseServiceAccountInvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(path, []byte("not-json"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cmd := newAuthUseServiceAccountCmd()
	cmd.SetArgs([]string{path})
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected parse error for malformed SA key")
	}
}

// TestAuthUseServiceAccountValidPrintsExport runs the happy path: a
// well-formed SA key file → stdout instructs the operator to export the
// env var pointing at the absolute path. The key bytes themselves are NOT
// echoed (only the file path).
func TestAuthUseServiceAccountValidPrintsExport(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sa.json")
	// Minimal service-account JSON the parser accepts (mirrors the shape
	// auth.NewServiceAccountResolver expects).
	saJSON := `{"type":"service_account","client_email":"sa@p.iam.gserviceaccount.com","private_key":"-----BEGIN PRIVATE KEY-----\nFAKE\n-----END PRIVATE KEY-----\n","token_uri":"https://example.test/token"}`
	if err := os.WriteFile(path, []byte(saJSON), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cmd := newAuthUseServiceAccountCmd()
	cmd.SetArgs([]string{path})
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v; stderr=%q", err, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, auth.EnvServiceAccountKeyVar) {
		t.Errorf("stdout missing env var name %q: %q", auth.EnvServiceAccountKeyVar, out)
	}
	abs, _ := filepath.Abs(path)
	if !strings.Contains(out, abs) {
		t.Errorf("stdout missing absolute key path %q: %q", abs, out)
	}
}

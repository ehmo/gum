package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeServiceAccountFile writes a service-account key whose token_uri points
// at tokenURL, and returns its path. The key is generated per test so the
// signed assertion the oauth2 library builds is valid without a real Google
// credential.
func writeServiceAccountFile(t *testing.T, tokenURL string) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("MarshalPKCS8PrivateKey: %v", err)
	}
	pemKey := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})

	blob, err := json.Marshal(map[string]string{
		"type":           "service_account",
		"project_id":     "test-project",
		"private_key_id": "key-1",
		"private_key":    string(pemKey),
		"client_email":   "Robot@test-project.iam.gserviceaccount.com",
		"client_id":      "1234567890",
		"token_uri":      tokenURL,
	})
	if err != nil {
		t.Fatalf("marshal key file: %v", err)
	}

	path := filepath.Join(t.TempDir(), "sa.json")
	if err := os.WriteFile(path, blob, 0o600); err != nil {
		t.Fatalf("write key file: %v", err)
	}
	return path
}

func TestLiveADCResolveReportsMissingCredentials(t *testing.T) {
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", filepath.Join(t.TempDir(), "absent.json"))

	_, err := NewLiveADCResolver().Resolve(context.Background(), []string{"webmasters.readonly"})
	var ae *AuthError
	if !errors.As(err, &ae) || ae.Code != "NO_ADC_CREDENTIALS" {
		t.Fatalf("Resolve = %v; want NO_ADC_CREDENTIALS", err)
	}
	if !strings.Contains(ae.HumanRemediation, "application-default login") {
		t.Errorf("remediation = %q; want the gcloud command named", ae.HumanRemediation)
	}
}

// TestLiveADCResolveDefaultsExpiryWhenTokenHasNone covers the expiry fallback:
// a token endpoint that omits expires_in must not yield a credential that
// looks already expired, which would make every call re-authenticate.
func TestLiveADCResolveDefaultsExpiryWhenTokenHasNone(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"at-1","token_type":"Bearer"}`))
	}))
	t.Cleanup(srv.Close)
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", writeServiceAccountFile(t, srv.URL))
	t.Setenv("GOOGLE_CLOUD_QUOTA_PROJECT", "")

	creds, err := NewLiveADCResolver().Resolve(context.Background(), []string{"webmasters.readonly"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if creds.Token != "at-1" {
		t.Errorf("Token = %q; want at-1", creds.Token)
	}
	if creds.ExpiresAt.IsZero() {
		t.Error("ExpiresAt is zero; a token without expires_in must get the 45-minute default")
	}
	if creds.SubjectFingerprint == "" {
		t.Error("SubjectFingerprint is empty; the key file's client_email identifies the principal")
	}
}

// TestADCSubjectPrefersCredentialFile pins that a credential file short-circuits
// the metadata lookup, and that the account email is lower-cased per §10.0.1.
func TestADCSubjectPrefersCredentialFile(t *testing.T) {
	got, err := adcSubject(context.Background(), []byte(`{"client_email":"Robot@example.test"}`))
	if err != nil {
		t.Fatalf("adcSubject: %v", err)
	}
	if got != "robot@example.test" {
		t.Fatalf("adcSubject = %q; want the lower-cased client_email", got)
	}
}

// TestADCSubjectRejectsEmptyMetadataEmail covers the arm that stops every
// workload on one platform from sharing a single subject: a metadata server
// that answers with a blank email must be an error, not an empty fingerprint.
func TestADCSubjectRejectsEmptyMetadataEmail(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Metadata-Flavor", "Google")
		_, _ = w.Write([]byte("   "))
	}))
	t.Cleanup(srv.Close)
	t.Setenv("GCE_METADATA_HOST", strings.TrimPrefix(srv.URL, "http://"))

	_, err := adcSubject(context.Background(), nil)
	if err == nil || !strings.Contains(err.Error(), "empty service-account email") {
		t.Fatalf("adcSubject(no JSON) = %v; want the empty email rejected", err)
	}
}

// TestADCEnvCredentialWithoutClientEmailNamesTheFile covers the stub
// resolver's fallback subject: a key file with a type but no client_email
// still has to produce a per-file fingerprint.
func TestADCEnvCredentialWithoutClientEmailNamesTheFile(t *testing.T) {
	const path = "/keys/adc.json"
	r := &ADCResolver{
		Env: func(k string) string {
			if k == "GOOGLE_APPLICATION_CREDENTIALS" {
				return path
			}
			return ""
		},
		ReadFile: func(string) ([]byte, error) { return []byte(`{"type":"service_account"}`), nil },
	}

	creds, err := r.Resolve(context.Background(), []string{"scope.a"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if creds.SubjectFingerprint != DeriveSubjectFingerprint("adc-sa:"+path) {
		t.Errorf("SubjectFingerprint = %q; want the fingerprint of the file path", creds.SubjectFingerprint)
	}
}

package auth_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/auth"
)

// fakeMetadataServer serves the two computeMetadata endpoints an ADC compute
// credential needs: the token endpoint the ComputeTokenSource reads, and the
// default service-account email that spec §10.0.1 names as the subject for a
// metadata-backed principal. emailStatus lets a caller break only the email
// endpoint. It returns the host:port for GCE_METADATA_HOST.
func fakeMetadataServer(t *testing.T, email string, emailStatus int) string {
	t.Helper()

	mux := http.NewServeMux()
	mux.HandleFunc("/computeMetadata/v1/instance/service-accounts/default/token",
		func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Metadata-Flavor", "Google")
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": "fake-token",
				"expires_in":   3600,
				"token_type":   "Bearer",
			})
		})
	mux.HandleFunc("/computeMetadata/v1/instance/service-accounts/default/email",
		func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Metadata-Flavor", "Google")
			if emailStatus != http.StatusOK {
				w.WriteHeader(emailStatus)
				return
			}
			_, _ = w.Write([]byte(email))
		})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	return strings.TrimPrefix(srv.URL, "http://")
}

// resolveOnFakeGCE resolves live ADC credentials against a fake metadata
// server. Setting GCE_METADATA_HOST makes metadata.OnGCE() short-circuit to
// true, so FindDefaultCredentials returns a ComputeTokenSource and creds.JSON
// is nil, which is exactly the shape a real GCE, Cloud Run, or GKE workload
// produces.
func resolveOnFakeGCE(t *testing.T, email string, emailStatus int) (*auth.Credentials, error) {
	t.Helper()

	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", "")
	t.Setenv("GCE_METADATA_HOST", fakeMetadataServer(t, email, emailStatus))

	return auth.NewLiveADCResolver().Resolve(t.Context(), []string{"gmail.readonly"})
}

// TestLiveADCMetadataSubjectFingerprint pins spec §10.0.1 for metadata-backed
// ADC. On GCE creds.JSON is nil, so a subject derived from the JSON alone is
// the same constant for every identity on the platform. That fingerprint keys
// the tee artifact HMAC, the recovery URI, the cache, and the gain ledger, so a
// shared value lets one service account read and overwrite another's artifacts
// inside one profile directory.
//
// The case-folding subtest is only meaningful together with the first: with a
// collapsed fingerprint every pair compares equal.
func TestLiveADCMetadataSubjectFingerprint(t *testing.T) {
	t.Run("distinct identities differ", func(t *testing.T) {
		first, err := resolveOnFakeGCE(t, "one@project.iam.gserviceaccount.com", http.StatusOK)
		if err != nil {
			t.Fatalf("Resolve first identity: %v", err)
		}
		second, err := resolveOnFakeGCE(t, "two@project.iam.gserviceaccount.com", http.StatusOK)
		if err != nil {
			t.Fatalf("Resolve second identity: %v", err)
		}
		if first.SubjectFingerprint == "" {
			t.Fatal("SubjectFingerprint empty; want a derived value")
		}
		if first.SubjectFingerprint == second.SubjectFingerprint {
			t.Errorf("two metadata identities share SubjectFingerprint %q; want distinct values",
				first.SubjectFingerprint)
		}
	})

	t.Run("email case is normalized", func(t *testing.T) {
		upper, err := resolveOnFakeGCE(t, "Mixed.Case@Project.IAM.gserviceaccount.com", http.StatusOK)
		if err != nil {
			t.Fatalf("Resolve mixed-case identity: %v", err)
		}
		lower, err := resolveOnFakeGCE(t, "mixed.case@project.iam.gserviceaccount.com", http.StatusOK)
		if err != nil {
			t.Fatalf("Resolve lower-case identity: %v", err)
		}
		if upper.SubjectFingerprint != lower.SubjectFingerprint {
			t.Errorf("SubjectFingerprint differs by email case: %q vs %q; §10.0.1 requires lower-case",
				upper.SubjectFingerprint, lower.SubjectFingerprint)
		}
	})
}

// TestLiveADCMetadataSubjectUnavailable pins the fail-closed arm. When the
// token endpoint works but the identity cannot be read, there is no subject to
// scope artifacts by. Returning credentials anyway would write them under a
// fingerprint that does not identify the caller, so Resolve must fail with a
// structured error instead.
func TestLiveADCMetadataSubjectUnavailable(t *testing.T) {
	creds, err := resolveOnFakeGCE(t, "", http.StatusInternalServerError)
	if err == nil {
		t.Fatalf("Resolve returned creds=%+v err=nil; want ADC_SUBJECT_UNKNOWN", creds)
	}
	if creds != nil {
		t.Errorf("creds=%+v on err path; want nil", creds)
	}
	var ae *auth.AuthError
	if !errors.As(err, &ae) {
		t.Fatalf("err type=%T (%v); want *auth.AuthError", err, err)
	}
	if ae.Code != "ADC_SUBJECT_UNKNOWN" {
		t.Errorf("Code=%q; want ADC_SUBJECT_UNKNOWN", ae.Code)
	}
	if ae.Strategy != "adc" {
		t.Errorf("Strategy=%q; want adc", ae.Strategy)
	}
	if ae.HumanRemediation == "" {
		t.Error("HumanRemediation empty; want a metadata-server hint")
	}
}

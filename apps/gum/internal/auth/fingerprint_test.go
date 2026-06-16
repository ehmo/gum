package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestDeriveSubjectFingerprintStable: same input → same output, distinct
// inputs → distinct outputs, empty input → empty output. Covers the §10.0.1
// invariant that the fingerprint is a deterministic function of the subject
// material.
func TestDeriveSubjectFingerprintStable(t *testing.T) {
	a1 := DeriveSubjectFingerprint("subject-a")
	a2 := DeriveSubjectFingerprint("subject-a")
	b := DeriveSubjectFingerprint("subject-b")
	if a1 == "" {
		t.Fatalf("empty fingerprint for non-empty input")
	}
	if a1 != a2 {
		t.Errorf("same input produced different fingerprints: %q vs %q", a1, a2)
	}
	if a1 == b {
		t.Errorf("distinct subjects produced identical fingerprints: both %q", a1)
	}
	if DeriveSubjectFingerprint("") != "" {
		t.Errorf("empty input produced non-empty fingerprint")
	}
	if len(a1) != 16 {
		t.Errorf("fingerprint length = %d; want 16 hex chars", len(a1))
	}
}

// TestToDispatchCredentialsCarriesFingerprint pins the wire: auth.Credentials
// .SubjectFingerprint must flow into dispatch.Credentials so the dispatcher's
// resolveAuthSubjectFingerprint helper picks it up and threads it through the
// semantic cache, tee, gain ledger, and audit lines.
func TestToDispatchCredentialsCarriesFingerprint(t *testing.T) {
	c := &Credentials{
		Token:              "tok",
		QuotaProjectID:     "qp",
		SubjectFingerprint: "fp-test",
	}
	d := c.ToDispatchCredentials()
	if d.SubjectFingerprint != "fp-test" {
		t.Errorf("dispatch.Credentials.SubjectFingerprint = %q; want fp-test", d.SubjectFingerprint)
	}
	if d.Token != "tok" || d.QuotaProjectID != "qp" {
		t.Errorf("other fields lost in conversion: %+v", d)
	}
}

// TestByoOAuthDerivesFingerprintFromRefreshToken: two distinct refresh tokens
// must produce distinct SubjectFingerprints on the returned Credentials, even
// when the same ClientID + scopes are used. This is the production code path
// that scopes the semantic cache for byo_oauth subjects.
func TestByoOAuthDerivesFingerprintFromRefreshToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"at","expires_in":3600,"scope":"x"}`))
	}))
	defer server.Close()

	mk := func(refresh string) string {
		kb := newFingerprintKeyring()
		bo := NewByoOAuth(ByoOAuthConfig{
			ClientID:      "cid",
			ClientSecret:  "csec",
			Scopes:        []string{"x"},
			TokenEndpoint: server.URL,
		}, kb)
		if err := bo.StoreRefreshToken(refresh); err != nil {
			t.Fatalf("StoreRefreshToken: %v", err)
		}
		creds, err := bo.Acquire(context.Background())
		if err != nil {
			t.Fatalf("Acquire(%s): %v", refresh, err)
		}
		if creds.SubjectFingerprint == "" {
			t.Fatalf("SubjectFingerprint empty for refresh %q", refresh)
		}
		return creds.SubjectFingerprint
	}
	fpA := mk("refresh-alice")
	fpB := mk("refresh-bob")
	if fpA == fpB {
		t.Errorf("two refresh tokens produced same fingerprint: %s", fpA)
	}
}

// TestADCResolverDerivesFingerprint: GOOGLE_APPLICATION_CREDENTIALS pointing at
// distinct service-account JSON keys must produce distinct fingerprints.
func TestADCResolverDerivesFingerprint(t *testing.T) {
	files := map[string][]byte{
		"/keys/alice.json": []byte(`{"type":"service_account","client_email":"alice@p.iam"}`),
		"/keys/bob.json":   []byte(`{"type":"service_account","client_email":"bob@p.iam"}`),
	}
	mk := func(path string) string {
		r := &ADCResolver{
			Env: func(k string) string {
				if k == "GOOGLE_APPLICATION_CREDENTIALS" {
					return path
				}
				return ""
			},
			ReadFile: func(p string) ([]byte, error) {
				if data, ok := files[p]; ok {
					return data, nil
				}
				return nil, errors.New("not found")
			},
			MetadataAvailable: func() bool { return false },
		}
		creds, err := r.Resolve(context.Background(), nil)
		if err != nil {
			t.Fatalf("Resolve(%s): %v", path, err)
		}
		if creds.SubjectFingerprint == "" {
			t.Fatalf("SubjectFingerprint empty for %s", path)
		}
		return creds.SubjectFingerprint
	}
	a := mk("/keys/alice.json")
	b := mk("/keys/bob.json")
	if a == b {
		t.Errorf("alice and bob ADC keys produced same fingerprint: %s", a)
	}
	if strings.HasPrefix(a, "alice") || strings.HasPrefix(b, "bob") {
		// Defensive: fingerprint must be opaque hex, never echo the subject literal.
		t.Errorf("fingerprint leaked subject literal: %s / %s", a, b)
	}
}

// fingerprintKeyring is a minimal in-memory KeyringBackend scoped to this
// internal-package test file. byooauth_test.go lives in auth_test (external)
// and has its own memKeyring; we need a separate type to avoid pulling that
// in here.
type fingerprintKeyring struct {
	items map[string]string
}

func newFingerprintKeyring() *fingerprintKeyring {
	return &fingerprintKeyring{items: map[string]string{}}
}

func (m *fingerprintKeyring) Get(k string) (string, error) { return m.items[k], nil }
func (m *fingerprintKeyring) Set(k, v string) error        { m.items[k] = v; return nil }
func (m *fingerprintKeyring) Delete(k string) error        { delete(m.items, k); return nil }


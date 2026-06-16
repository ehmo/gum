package auth_test

import (
	"errors"
	"testing"

	"go.uber.org/goleak"

	"github.com/ehmo/gum/internal/auth"
)

// TestLiveADCResolverResolveADCTokenFetchFailed pins the
// `creds.TokenSource.Token() err → AuthError{ADC_TOKEN_FETCH_FAILED}`
// arm: setting GCE_METADATA_HOST forces metadata.OnGCE() → true so
// FindDefaultCredentials returns a ComputeTokenSource without contacting
// the metadata server; the subsequent Token() call then dials the bogus
// host and fails, which MUST surface a structured AuthError with
// Strategy="adc" and a HumanRemediation pointing at the token-exchange
// failure (distinct from the NO_ADC_CREDENTIALS arm above).
//
// 127.0.0.1:1 is intentionally a refused-port so the dial fails fast and
// no background goroutines linger to trip goleak.
func TestLiveADCResolverResolveADCTokenFetchFailed(t *testing.T) {
	defer goleak.VerifyNone(t)

	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", "")
	t.Setenv("GCE_METADATA_HOST", "127.0.0.1:1")

	r := auth.NewLiveADCResolver()
	creds, err := r.Resolve(t.Context(), []string{"gmail.readonly"})
	if err == nil {
		t.Fatalf("Resolve returned creds=%+v err=nil; want ADC_TOKEN_FETCH_FAILED err", creds)
	}
	if creds != nil {
		t.Errorf("creds=%+v on err path; want nil", creds)
	}
	var ae *auth.AuthError
	if !errors.As(err, &ae) {
		t.Fatalf("err type=%T (%v); want *auth.AuthError", err, err)
	}
	if ae.Code != "ADC_TOKEN_FETCH_FAILED" {
		t.Errorf("Code=%q; want ADC_TOKEN_FETCH_FAILED", ae.Code)
	}
	if ae.Strategy != "adc" {
		t.Errorf("Strategy=%q; want adc", ae.Strategy)
	}
	if ae.HumanRemediation == "" {
		t.Error("HumanRemediation empty; want token-exchange-failed hint")
	}
}

package adapters

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

// TestStripCredsOnCrossHostRedirectFirstHop pins the len(via)==0 arm. The
// first call carries no history, so there is no origin host to compare and
// nothing to strip.
func TestStripCredsOnCrossHostRedirectFirstHop(t *testing.T) {
	t.Parallel()
	req, err := http.NewRequest(http.MethodGet, "https://www.googleapis.com/v1/x?key=secret", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if err := stripCredsOnCrossHostRedirect(req, nil); err != nil {
		t.Fatalf("err=%v; want nil on the first hop", err)
	}
	if got := req.URL.Query().Get("key"); got != "secret" {
		t.Errorf("key=%q; the first hop must keep the credential", got)
	}
}

// TestCtxSleepCompletesTheFullWait pins the timer arm of the retry sleep.
func TestCtxSleepCompletesTheFullWait(t *testing.T) {
	t.Parallel()
	if err := ctxSleep(context.Background(), time.Millisecond); err != nil {
		t.Errorf("err=%v; want nil when the wait elapses", err)
	}
}

// TestCtxSleepAbortsOnCancellation pins the ctx.Done arm. A cancelled call
// must return at once rather than hold the retry loop for the full
// Retry-After window.
func TestCtxSleepAbortsOnCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	start := time.Now()
	if err := ctxSleep(ctx, time.Hour); err == nil {
		t.Fatal("err=nil; want ctx.Err() on a cancelled sleep")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("ctxSleep took %v; want an immediate return", elapsed)
	}
}

// TestResolveRequestURLPrefixesARelativePath pins the default-base arm. A
// catalog binding normally carries a path, not an absolute URL, so this is
// the shape every generated Google op takes.
func TestResolveRequestURLPrefixesARelativePath(t *testing.T) {
	t.Parallel()
	sdk := NewTypedRestSDK()
	got, err := sdk.resolveRequestURL("/gmail/v1/users/me/messages", false)
	if err != nil {
		t.Fatalf("resolveRequestURL: %v", err)
	}
	if got != "https://www.googleapis.com/gmail/v1/users/me/messages" {
		t.Errorf("url = %q; want the googleapis base", got)
	}
}

// TestValidateCredentialURLRejections pins each guard on the credentialed
// absolute-URL path. Every one of these shapes would put a bearer token or an
// API key somewhere it does not belong.
func TestValidateCredentialURLRejections(t *testing.T) {
	t.Parallel()
	sdk := NewTypedRestSDK()

	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"unparseable", "https://exa mple.com/v1", "parse credentialed URL"},
		{"userinfo", "https://user:pass@www.googleapis.com/v1", "must not include userinfo"},
		{"fragment", "https://www.googleapis.com/v1#frag", "must not include a fragment"},
		{"plain http", "http://www.googleapis.com/v1", "must use https"},
		{"foreign host", "https://evil.example.com/v1", "is not allowed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := sdk.validateCredentialURL(tc.raw)
			if err == nil {
				t.Fatalf("validateCredentialURL(%q) = nil; want a rejection", tc.raw)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err=%q; want it to name %q", err, tc.want)
			}
		})
	}
}

// TestValidateCredentialURLAcceptsGoogleAPIs pins the accept arm: a
// googleapis.com subdomain over https is the production shape.
func TestValidateCredentialURLAcceptsGoogleAPIs(t *testing.T) {
	t.Parallel()
	sdk := NewTypedRestSDK()
	if err := sdk.validateCredentialURL("https://gmail.googleapis.com/gmail/v1/users/me"); err != nil {
		t.Errorf("err=%v; want a googleapis.com host to pass", err)
	}
}

// TestCredentialURLAllowedRejectsAnEmptyHost pins the empty-host guard. A URL
// with no authority has nothing to compare against an allowlist, so it can
// never be trusted with a credential.
func TestCredentialURLAllowedRejectsAnEmptyHost(t *testing.T) {
	t.Parallel()
	sdk := NewTypedRestSDK()
	u, err := url.Parse("https:///v1/x")
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	if sdk.credentialURLAllowed(u) {
		t.Error("credentialURLAllowed accepted a URL with no host")
	}
}

// TestURLExplicitlyAllowedEntryForms pins the allowlist entry parser: blank
// entries are skipped, bare host:port entries go through SplitHostPort, and a
// port on the entry has to match the target's.
func TestURLExplicitlyAllowedEntryForms(t *testing.T) {
	t.Parallel()
	sdk := NewTypedRestSDK()
	sdk.CredentialHostAllowlist = []string{"   ", "localhost:8080"}

	cases := []struct {
		name string
		raw  string
		want bool
	}{
		{"host and port match", "http://localhost:8080/v1", true},
		{"port mismatch", "http://localhost:9999/v1", false},
		{"host mismatch", "http://other:8080/v1", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			u, err := url.Parse(tc.raw)
			if err != nil {
				t.Fatalf("url.Parse: %v", err)
			}
			if got := sdk.urlExplicitlyAllowed(u); got != tc.want {
				t.Errorf("urlExplicitlyAllowed(%q) = %v; want %v", tc.raw, got, tc.want)
			}
		})
	}
}

// TestRetryAfterSecondsUnparseableValue pins the final fallback: a
// Retry-After that is neither delta-seconds nor an HTTP-date must read as "no
// hint", not as an arbitrary wait.
func TestRetryAfterSecondsUnparseableValue(t *testing.T) {
	t.Parallel()
	h := http.Header{}
	h.Set("Retry-After", "soonish")
	if got := retryAfterSeconds(h); got != 0 {
		t.Errorf("retryAfterSeconds = %d; want 0 for an unparseable value", got)
	}
}

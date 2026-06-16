package auth

import (
	"strings"
	"testing"
)

// TestOAuthRemediation locks the two error-body classifications and the
// "unknown body → empty string" passthrough. Casing is intentionally mixed
// in fixtures because Google's endpoints vary it across token services.
func TestOAuthRemediation(t *testing.T) {
	cases := []struct {
		name    string
		body    string
		wantSub string // substring expected in result; "" means must be empty
	}{
		{
			// The live BYO-login failure mode: a Google Desktop client registered
			// without its secret. The remediation must point at --secret-stdin.
			name:    "client_secret_missing",
			body:    `{"error":"invalid_request","error_description":"client_secret is missing."}`,
			wantSub: "--secret-stdin",
		},
		{name: "invalid_grant_lower", body: `{"error":"invalid_grant"}`, wantSub: "Refresh token revoked"},
		{name: "invalid_grant_mixed_case", body: `{"error":"Invalid_Grant"}`, wantSub: "Refresh token"},
		{name: "invalid_rapt_lower", body: `{"error":"invalid_rapt"}`, wantSub: "reauth-proof token"},
		{name: "invalid_rapt_uppercase", body: `INVALID_RAPT`, wantSub: "reauth-proof"},
		{name: "unknown_body_returns_empty", body: `{"error":"unauthorized_client"}`, wantSub: ""},
		{name: "empty_body", body: "", wantSub: ""},
		{
			// invalid_rapt wins over invalid_grant when both are present —
			// the rapt branch is checked first because it is the more
			// specific failure mode.
			name:    "rapt_wins_over_grant_when_both_present",
			body:    "invalid_grant invalid_rapt",
			wantSub: "reauth-proof",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := OAuthRemediation(tc.body)
			if tc.wantSub == "" {
				if got != "" {
					t.Errorf("OAuthRemediation(%q) = %q, want empty", tc.body, got)
				}
				return
			}
			if !strings.Contains(got, tc.wantSub) {
				t.Errorf("OAuthRemediation(%q) = %q, want substring %q", tc.body, got, tc.wantSub)
			}
		})
	}
}

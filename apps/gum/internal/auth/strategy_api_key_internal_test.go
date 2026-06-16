package auth

import "testing"

// TestAPIKeyKeyringKeyShapes pins the per-profile derivation and the
// empty/whitespace fallback to "default". The keychain entry name is
// the unit of isolation between dev/prod profiles — a regression that
// dropped the profile would silently merge keys across profiles.
func TestAPIKeyKeyringKeyShapes(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"prod", "gum.api_key.prod"},
		{"dev", "gum.api_key.dev"},
		{"", "gum.api_key.default"},
		{"   ", "gum.api_key.default"},
		{"\tdefault\n", "gum.api_key.default"},
	}
	for _, tc := range cases {
		if got := apiKeyKeyringKey(tc.in); got != tc.want {
			t.Errorf("apiKeyKeyringKey(%q)=%q; want %q", tc.in, got, tc.want)
		}
	}
}

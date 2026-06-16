package auth

import (
	"encoding/base64"
	"testing"
)

// TestRandomURLTokenShapes pins three properties: encoded length
// matches the RawURLEncoding of n bytes; calls are non-deterministic
// (sanity check that crypto/rand is wired); the encoded form decodes
// back to exactly n bytes — so callers can rely on the inverse for
// fingerprinting if needed.
func TestRandomURLTokenShapes(t *testing.T) {
	for _, n := range []int{1, 16, 32, 48} {
		a, err := randomURLToken(n)
		if err != nil {
			t.Fatalf("n=%d: %v", n, err)
		}
		b, err := randomURLToken(n)
		if err != nil {
			t.Fatalf("n=%d second call: %v", n, err)
		}
		if a == b {
			t.Errorf("n=%d: two calls produced identical tokens (rand wiring?)", n)
		}
		raw, err := base64.RawURLEncoding.DecodeString(a)
		if err != nil {
			t.Errorf("n=%d: not RawURLEncoded: %v", n, err)
		}
		if len(raw) != n {
			t.Errorf("n=%d: decoded len=%d; want %d", n, len(raw), n)
		}
	}
}

// TestPKCES256IsDeterministic pins the S256 challenge contract: the
// same verifier always produces the same challenge (no hidden salt)
// and the output is a 43-char base64url string (sha256 → 32 bytes →
// 43 chars RawURLEncoded). The OAuth callback verifies the challenge
// by recomputing it, so non-determinism would break login.
func TestPKCES256IsDeterministic(t *testing.T) {
	const verifier = "fixed-verifier-for-test"
	a := pkceS256(verifier)
	b := pkceS256(verifier)
	if a != b {
		t.Errorf("pkceS256 non-deterministic: %q vs %q", a, b)
	}
	if len(a) != 43 {
		t.Errorf("pkceS256 len=%d; want 43", len(a))
	}
	if got := pkceS256("different"); got == a {
		t.Errorf("different verifier produced same challenge: %q", got)
	}
}

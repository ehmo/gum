package lro

import (
	"strings"
	"testing"
	"time"
)

// TestTimeoutErrorError locks the message shape. The format must include
// "lro:" prefix, the elapsed duration, and the operation name in quotes so
// audit log readers and operators can grep on a stable string.
func TestTimeoutErrorError(t *testing.T) {
	e := &TimeoutError{
		OperationName: "operations/foo-123",
		Elapsed:       5 * time.Minute,
	}
	msg := e.Error()
	for _, want := range []string{"lro:", "5m0s", `"operations/foo-123"`, "poll timeout"} {
		if !strings.Contains(msg, want) {
			t.Errorf("Error()=%q, missing %q", msg, want)
		}
	}
}

// TestTruncate covers the body-snippet helper used in error envelopes when
// the upstream returns a non-JSON payload. The rule: <=n returned verbatim,
// >n returns the first n bytes plus a single-rune ellipsis.
func TestTruncate(t *testing.T) {
	cases := []struct {
		name string
		in   string
		n    int
		want string
	}{
		{name: "shorter_than_n", in: "hello", n: 10, want: "hello"},
		{name: "exactly_n", in: "hello", n: 5, want: "hello"},
		{name: "longer_than_n", in: "abcdefghij", n: 3, want: "abc…"},
		{name: "empty_input", in: "", n: 5, want: ""},
		{name: "zero_n_truncates_everything", in: "x", n: 0, want: "…"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := truncate([]byte(tc.in), tc.n)
			if got != tc.want {
				t.Errorf("truncate(%q, %d) = %q, want %q", tc.in, tc.n, got, tc.want)
			}
		})
	}
}

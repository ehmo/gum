package auth

import (
	"reflect"
	"testing"
)

// TestNormaliseScopes pins the scope normalization: bare names get the
// googleapis prefix, already-full-URL scopes pass through unchanged, and the
// empty string is dropped. The HasPrefix form (audit fix) also leaves a bare
// "http://"/"https://" scheme alone instead of double-prefixing it — the old
// `len(s) >= 8` guard mis-handled a 7-char "http://".
func TestNormaliseScopes(t *testing.T) {
	const prefix = "https://www.googleapis.com/auth/"
	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{
			name: "bare names get prefixed",
			in:   []string{"gmail.readonly", "calendar"},
			want: []string{prefix + "gmail.readonly", prefix + "calendar"},
		},
		{
			name: "full https URL passes through",
			in:   []string{prefix + "gmail.send"},
			want: []string{prefix + "gmail.send"},
		},
		{
			name: "custom http URL passes through",
			in:   []string{"http://example.test/custom"},
			want: []string{"http://example.test/custom"},
		},
		{
			name: "empty string dropped",
			in:   []string{"", "drive"},
			want: []string{prefix + "drive"},
		},
		{
			name: "bare scheme not double-prefixed",
			in:   []string{"http://", "https://"},
			want: []string{"http://", "https://"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := normaliseScopes(tc.in)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("normaliseScopes(%v) = %v; want %v", tc.in, got, tc.want)
			}
		})
	}
}

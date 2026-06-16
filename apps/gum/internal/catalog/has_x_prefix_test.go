package catalog

import "testing"

// TestHasXPrefix locks the vendor-extension prefix detector used by
// BindingType.IsVendorExtension and InvocationKind.IsVendorExtension. The
// rule is "starts with literal 'x-' AND has at least one trailing char."
func TestHasXPrefix(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"x-batch", true},
		{"x-anything-here", true},
		{"", false},
		{"x", false},
		{"x-", false},     // too short — needs at least one char after the dash
		{"X-batch", false}, // case-sensitive per spec
		{"y-batch", false},
		{"http", false},
		{"-x", false},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			if got := hasXPrefix(tc.in); got != tc.want {
				t.Errorf("hasXPrefix(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

package catalog

import "testing"

// TestStabilityValid pins the closed enum: only stable/beta/alpha are
// accepted; anything else (including empty) is rejected so a typo'd
// catalog entry can't slip through the catalog validator.
func TestStabilityValid(t *testing.T) {
	cases := []struct {
		in   Stability
		want bool
	}{
		{StabilityStable, true},
		{StabilityBeta, true},
		{StabilityAlpha, true},
		{"experimental", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := tc.in.Valid(); got != tc.want {
			t.Errorf("(%q).Valid()=%v; want %v", tc.in, got, tc.want)
		}
	}
}

// TestBackendKindValid pins the closed enum + the x-prefix vendor
// extension escape hatch. Same shape as InterfaceKind.Valid — a typo
// must fail validation while x-<anything> passes through so a vendor
// extension can land without a spec revision.
func TestBackendKindValid(t *testing.T) {
	cases := []struct {
		in   BackendKind
		want bool
	}{
		{BackendKindTypedRestSDK, true},
		{BackendKindDiscoveryREST, true},
		{BackendKindRawHTTP, true},
		{BackendKindGRPCSDK, true},
		{BackendKindMCPPlugin, true},
		{BackendKindGRPCPlugin, true},
		{BackendKindMapsSDK, true},
		{BackendKindGenAI, true},
		{"x-experimental", true},
		{"unknown", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := tc.in.Valid(); got != tc.want {
			t.Errorf("(%q).Valid()=%v; want %v", tc.in, got, tc.want)
		}
	}
}

// TestInterfaceKindValid pins the closed enum + the x-prefix vendor
// extension escape hatch. Both branches must stay distinguishable so
// a future spec revision can tighten the validator without quietly
// allowing typos like "x-" (too short — must be x-<at-least-one>).
func TestInterfaceKindValid(t *testing.T) {
	cases := []struct {
		in   InterfaceKind
		want bool
	}{
		{InterfaceKindDiscoveryREST, true},
		{InterfaceKindGRPC, true},
		{InterfaceKindPluginMCP, true},
		{InterfaceKindPluginGRPC, true},
		{InterfaceKindSDKNative, true},
		{"x-custom", true},
		{"x-", false},
		{"", false},
		{"unknown", false},
	}
	for _, tc := range cases {
		if got := tc.in.Valid(); got != tc.want {
			t.Errorf("(%q).Valid()=%v; want %v", tc.in, got, tc.want)
		}
	}
}

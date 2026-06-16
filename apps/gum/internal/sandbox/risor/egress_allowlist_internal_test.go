package risor

import (
	"net"
	"testing"
)

func TestMatchHostAllowlistWildcardSubdomainSemantics(t *testing.T) {
	cases := []struct {
		name      string
		host      string
		allowlist []string
		want      bool
	}{
		{
			name:      "subdomain_matches_wildcard",
			host:      "foo.example.com",
			allowlist: []string{"*.example.com"},
			want:      true,
		},
		{
			name:      "deep_subdomain_matches_wildcard",
			host:      "a.b.example.com",
			allowlist: []string{"*.example.com"},
			want:      true,
		},
		{
			name:      "apex_does_not_match_wildcard",
			host:      "example.com",
			allowlist: []string{"*.example.com"},
			want:      false,
		},
		{
			name:      "unrelated_host_denied",
			host:      "other.com",
			allowlist: []string{"*.example.com"},
			want:      false,
		},
		{
			name:      "default_googleapis_case_insensitive",
			host:      "FOO.googleapis.com",
			allowlist: []string{"*.googleapis.com"},
			want:      true,
		},
		{
			name:      "exact_match",
			host:      "api.example.com",
			allowlist: []string{"api.example.com"},
			want:      true,
		},
		{
			name:      "exact_match_case_insensitive",
			host:      "API.example.com",
			allowlist: []string{"api.example.com"},
			want:      true,
		},
		{
			name:      "empty_entry_ignored",
			host:      "api.example.com",
			allowlist: []string{""},
			want:      false,
		},
		{
			name:      "star_entry_ignored",
			host:      "api.example.com",
			allowlist: []string{"*"},
			want:      false,
		},
		{
			name:      "empty_wildcard_entry_ignored",
			host:      "api.example.com",
			allowlist: []string{"*."},
			want:      false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := matchHostAllowlist(tc.host, tc.allowlist); got != tc.want {
				t.Fatalf("matchHostAllowlist(%q, %v) = %v, want %v", tc.host, tc.allowlist, got, tc.want)
			}
		})
	}
}

func TestIsBlockedEgressIP(t *testing.T) {
	cases := []struct {
		name string
		ip   net.IP
		want bool
	}{
		{name: "nil", ip: nil, want: false},
		{name: "public_ipv4", ip: net.ParseIP("8.8.8.8"), want: false},
		{name: "loopback", ip: net.ParseIP("127.0.0.1"), want: true},
		{name: "private_ipv4", ip: net.ParseIP("10.0.0.1"), want: true},
		{name: "link_local_unicast", ip: net.ParseIP("169.254.169.254"), want: true},
		{name: "unspecified", ip: net.ParseIP("0.0.0.0"), want: true},
		{name: "private_ipv6", ip: net.ParseIP("fd00::1"), want: true},
		{name: "link_local_multicast", ip: net.ParseIP("ff02::1"), want: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isBlockedEgressIP(tc.ip); got != tc.want {
				t.Fatalf("isBlockedEgressIP(%v) = %v, want %v", tc.ip, got, tc.want)
			}
		})
	}
}

package main

import (
	"errors"
	"testing"

	"github.com/ehmo/gum/internal/plugins"
)

// TestCanaryErrorCode locks the spec §8 projection: today every error
// (known plugin sentinel or arbitrary) maps to SERVICE_DOWN. A future
// remap should rewrite this table along with the function.
func TestCanaryErrorCode(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{name: "manifest_not_found", err: plugins.ErrManifestNotFound, want: "SERVICE_DOWN"},
		{name: "manifest_invalid", err: plugins.ErrManifestInvalid, want: "SERVICE_DOWN"},
		{name: "executable_untrusted", err: plugins.ErrExecutableUntrusted, want: "SERVICE_DOWN"},
		{name: "arbitrary_error", err: errors.New("disk full"), want: "SERVICE_DOWN"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := canaryErrorCode(tc.err); got != tc.want {
				t.Errorf("canaryErrorCode(%v) = %q, want %q", tc.err, got, tc.want)
			}
		})
	}
}

// TestCanarySourceErrorCode pins the Go-side sentinel mapping: audit
// readers correlate "SERVICE_DOWN" + a stable Go name. Unknown errors fall
// through to "Unknown" so the channel never carries an empty string.
func TestCanarySourceErrorCode(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{name: "manifest_not_found", err: plugins.ErrManifestNotFound, want: "ErrManifestNotFound"},
		{name: "manifest_invalid", err: plugins.ErrManifestInvalid, want: "ErrManifestInvalid"},
		{name: "executable_untrusted", err: plugins.ErrExecutableUntrusted, want: "ErrExecutableUntrusted"},
		{name: "unknown", err: errors.New("disk full"), want: "Unknown"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := canarySourceErrorCode(tc.err); got != tc.want {
				t.Errorf("canarySourceErrorCode(%v) = %q, want %q", tc.err, got, tc.want)
			}
		})
	}
}

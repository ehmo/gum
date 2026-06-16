// gum-d7t: pure-function unit tests for the §9.2 roots selection algorithm
// and the PROJECT_ROOT_REQUIRED envelope shape. The end-to-end acceptance
// test (TestProfileResolutionFromMCPRoots) lives in roots_acceptance_test.go
// so it can use the shared mcp_test fixtures.

package mcp

import (
	"reflect"
	"testing"
)

// TestResolveProjectRootBranches pins the §9.2 selection algorithm. Each
// branch corresponds to a normative bullet in spec lines 2050-2052.
func TestResolveProjectRootBranches(t *testing.T) {
	const r1 = "file:///tmp/projA"
	const r2 = "file:///tmp/projB"

	t.Run("no_roots_disables_project_local", func(t *testing.T) {
		uri, err := resolveProjectRoot(nil, "")
		if err != nil {
			t.Fatalf("unexpected err: %+v", err)
		}
		if uri != "" {
			t.Errorf("uri=%q; want empty (project-local disabled)", uri)
		}
	})

	t.Run("single_root_no_gumroot_picks_root", func(t *testing.T) {
		uri, err := resolveProjectRoot([]string{r1}, "")
		if err != nil {
			t.Fatalf("unexpected err: %+v", err)
		}
		if uri != r1 {
			t.Errorf("uri=%q; want %q", uri, r1)
		}
	})

	t.Run("single_root_matching_gumroot_picks_root", func(t *testing.T) {
		uri, err := resolveProjectRoot([]string{r1}, r1)
		if err != nil {
			t.Fatalf("unexpected err: %+v", err)
		}
		if uri != r1 {
			t.Errorf("uri=%q; want %q", uri, r1)
		}
	})

	t.Run("single_root_mismatched_gumroot_errors", func(t *testing.T) {
		_, err := resolveProjectRoot([]string{r1}, "file:///tmp/other")
		if err == nil {
			t.Fatal("want error for unknown gumRoot")
		}
		if err.Reason != "gumroot_not_in_negotiated_set" {
			t.Errorf("reason=%q; want gumroot_not_in_negotiated_set", err.Reason)
		}
	})

	t.Run("multi_root_missing_gumroot_errors", func(t *testing.T) {
		_, err := resolveProjectRoot([]string{r1, r2}, "")
		if err == nil {
			t.Fatal("want error for missing gumRoot in multi-root session")
		}
		if err.Reason != "missing_gumroot_in_multi_root_session" {
			t.Errorf("reason=%q; want missing_gumroot_in_multi_root_session", err.Reason)
		}
		if !reflect.DeepEqual(err.NegotiatedRoots, []string{r1, r2}) {
			t.Errorf("negotiated_roots=%v; want both", err.NegotiatedRoots)
		}
	})

	t.Run("multi_root_matching_gumroot_picks_it", func(t *testing.T) {
		uri, err := resolveProjectRoot([]string{r1, r2}, r2)
		if err != nil {
			t.Fatalf("unexpected err: %+v", err)
		}
		if uri != r2 {
			t.Errorf("uri=%q; want %q", uri, r2)
		}
	})

	t.Run("multi_root_unknown_gumroot_errors", func(t *testing.T) {
		_, err := resolveProjectRoot([]string{r1, r2}, "file:///tmp/projC")
		if err == nil {
			t.Fatal("want error for gumRoot outside negotiated set")
		}
		if err.Reason != "gumroot_not_in_negotiated_set" {
			t.Errorf("reason=%q; want gumroot_not_in_negotiated_set", err.Reason)
		}
	})

	t.Run("non_file_gumroot_rejected", func(t *testing.T) {
		_, err := resolveProjectRoot([]string{r1, r2}, "https://example.com/proj")
		if err == nil {
			t.Fatal("want error for non-file gumRoot")
		}
		if err.Reason != "gumroot_not_file_uri" {
			t.Errorf("reason=%q; want gumroot_not_file_uri", err.Reason)
		}
	})
}

// TestProjectRootRequiredEnvelopeShape pins the PROJECT_ROOT_REQUIRED
// envelope's required-field surface (spec §1421).
func TestProjectRootRequiredEnvelopeShape(t *testing.T) {
	env := projectRootRequiredEnvelope(&projectRootError{
		Reason:          "missing_gumroot_in_multi_root_session",
		NegotiatedRoots: []string{"file:///a", "file:///b"},
	})
	if env["error_code"] != "PROJECT_ROOT_REQUIRED" {
		t.Errorf("error_code=%v; want PROJECT_ROOT_REQUIRED", env["error_code"])
	}
	if env["reason"] != "missing_gumroot_in_multi_root_session" {
		t.Errorf("reason=%v; want missing_gumroot_in_multi_root_session", env["reason"])
	}
	if _, ok := env["negotiated_roots"]; !ok {
		t.Error("envelope missing negotiated_roots field")
	}
	if _, ok := env["user_message"]; !ok {
		t.Error("envelope missing user_message field")
	}
}

// TestRootURIToPath round-trips file:// URIs through rootURIToPath. Non-file
// schemes and malformed URIs yield "".
func TestRootURIToPath(t *testing.T) {
	cases := []struct {
		uri  string
		want string
	}{
		{"file:///tmp/proj", "/tmp/proj"},
		{"file:///Users/nan/Work/ai/gum", "/Users/nan/Work/ai/gum"},
		{"file:///path%20with%20spaces", "/path with spaces"},
		{"https://example.com/x", ""},
		{"", ""},
	}
	for _, c := range cases {
		if got := rootURIToPath(c.uri); got != c.want {
			t.Errorf("rootURIToPath(%q)=%q; want %q", c.uri, got, c.want)
		}
	}
}

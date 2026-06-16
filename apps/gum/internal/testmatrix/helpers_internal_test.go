package testmatrix

import (
	"strings"
	"testing"
)

// TestTopLevelBranches pins both arms of the topLevel splitter: a
// "Group/Subgroup" name returns the leading segment; a flat name
// returns itself unchanged. A regression here would corrupt the
// per-group aggregation cmd/test-matrix uses for its summary table.
func TestTopLevelBranches(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"Group/Sub", "Group"},
		{"Group/Sub/Deep", "Group"},
		{"NoSlash", "NoSlash"},
		{"/leading-slash", "/leading-slash"},
		{"", ""},
	}
	for _, tc := range cases {
		if got := topLevel(tc.in); got != tc.want {
			t.Errorf("topLevel(%q)=%q; want %q", tc.in, got, tc.want)
		}
	}
}

// TestTruncateBranches pins both arms of the truncate helper:
//   - Input shorter than n returns the input verbatim.
//   - Input longer than n returns prefix + "[truncated N bytes]" suffix
//     where N matches the dropped byte count.
func TestTruncateBranches(t *testing.T) {
	t.Run("short_input_passthrough", func(t *testing.T) {
		if got := truncate("hello", 10); got != "hello" {
			t.Errorf("truncate(short)=%q; want passthrough", got)
		}
	})
	t.Run("at_limit_passthrough", func(t *testing.T) {
		if got := truncate("hello", 5); got != "hello" {
			t.Errorf("truncate(at-limit)=%q; want passthrough", got)
		}
	})
	t.Run("over_limit_appends_suffix", func(t *testing.T) {
		got := truncate("hello-world", 5)
		if !strings.HasPrefix(got, "hello") {
			t.Errorf("got=%q; want prefix 'hello'", got)
		}
		if !strings.Contains(got, "truncated 6 bytes") {
			t.Errorf("got=%q; want 'truncated 6 bytes' tag (len 11 - n 5)", got)
		}
	})
}

// TestRunnerWorkDirDefault pins both arms of workDir: empty
// configuration returns "." (current dir) so `go test` runs in
// the matrix process's cwd; an explicit setting is returned
// verbatim.
func TestRunnerWorkDirDefault(t *testing.T) {
	if got := (&Runner{}).workDir(); got != "." {
		t.Errorf("default workDir=%q; want '.'", got)
	}
	if got := (&Runner{WorkDir: "/tmp/x"}).workDir(); got != "/tmp/x" {
		t.Errorf("custom workDir=%q; want /tmp/x", got)
	}
}

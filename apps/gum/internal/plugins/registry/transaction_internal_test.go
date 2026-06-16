package registry

import (
	"encoding/json"
	"errors"
	"io/fs"
	"syscall"
	"testing"
)

// TestProfileDirReturnsBound verifies the trivial accessor: ProfileDir
// returns exactly the path the Registry was constructed with so callers
// composing artifact paths (tee, audit) don't drift from the registry's
// view.
func TestProfileDirReturnsBound(t *testing.T) {
	r := New("/some/profile/dir")
	if got := r.ProfileDir(); got != "/some/profile/dir" {
		t.Errorf("ProfileDir() = %q, want /some/profile/dir", got)
	}
}

// TestIsFsyncUnsupported covers the three matched signatures and the
// non-match fallthrough. NFS/FUSE/overlay return ENOTSUP or EINVAL; some
// platforms surface fs.ErrInvalid. Anything else (e.g., EACCES) must NOT
// trigger the fallback.
func TestIsFsyncUnsupported(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{name: "ENOTSUP_matches", err: syscall.ENOTSUP, want: true},
		{name: "EINVAL_matches", err: syscall.EINVAL, want: true},
		{name: "ErrInvalid_matches", err: fs.ErrInvalid, want: true},
		{name: "EACCES_does_not_match", err: syscall.EACCES, want: false},
		{name: "arbitrary_error_does_not_match", err: errors.New("nope"), want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isFsyncUnsupported(tc.err); got != tc.want {
				t.Errorf("isFsyncUnsupported(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

// TestNameOf covers the three branches of the plugin-name extractor used
// during deterministic sort:
//   - map[string]any with "name" string field — used in test fixtures.
//   - json.RawMessage path — used after Unmarshal of the on-disk file.
//   - unknown type fallthrough returns "" (so unparseable entries sort
//     ahead of everything else, preserving determinism).
func TestNameOf(t *testing.T) {
	t.Run("map_string_any_returns_name", func(t *testing.T) {
		if got := nameOf(map[string]any{"name": "alpha"}); got != "alpha" {
			t.Errorf("got %q, want alpha", got)
		}
	})

	t.Run("map_missing_name_returns_empty", func(t *testing.T) {
		if got := nameOf(map[string]any{"version": "1"}); got != "" {
			t.Errorf("got %q, want empty", got)
		}
	})

	t.Run("map_name_not_string_returns_empty", func(t *testing.T) {
		if got := nameOf(map[string]any{"name": 42}); got != "" {
			t.Errorf("got %q, want empty", got)
		}
	})

	t.Run("rawmessage_returns_name", func(t *testing.T) {
		raw := json.RawMessage(`{"name":"beta","other":"x"}`)
		if got := nameOf(raw); got != "beta" {
			t.Errorf("got %q, want beta", got)
		}
	})

	t.Run("rawmessage_malformed_returns_empty", func(t *testing.T) {
		raw := json.RawMessage(`not json`)
		if got := nameOf(raw); got != "" {
			t.Errorf("got %q, want empty", got)
		}
	})

	t.Run("unknown_type_returns_empty", func(t *testing.T) {
		if got := nameOf(123); got != "" {
			t.Errorf("got %q, want empty", got)
		}
	})
}

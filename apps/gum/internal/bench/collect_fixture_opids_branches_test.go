package bench

import (
	"os"
	"path/filepath"
	"testing"
)

// TestCollectFixtureOpIDsWalkRootMissingReturnsErr pins
// collectFixtureOpIDs's `WalkDir top-level err → return nil, err` arm
// (spec_scale_catalog.go:167-169). A nonexistent root makes the very
// first WalkDir callback receive a path-resolution error; the
// function then surfaces it verbatim rather than masking it as "no
// fixtures".
func TestCollectFixtureOpIDsWalkRootMissingReturnsErr(t *testing.T) {
	t.Parallel()
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	got, err := collectFixtureOpIDs(missing)
	if err == nil {
		t.Errorf("collectFixtureOpIDs(missing-root) err=nil got=%v; want WalkDir err", got)
	}
}

// TestCollectFixtureOpIDsSkipsUnreadableAndMalformed pins three
// per-entry continue arms:
//   - WalkDir callback err → return err (146-147), triggered by an
//     unreadable subdirectory (chmod 0o000) when euid != 0
//   - request.json ReadFile err → return nil (153-154), via chmod 0o000
//     on a planted request.json
//   - request.json JSON unmarshal err → return nil (159-160), via
//     intentionally-invalid JSON
//
// A valid request.json sits alongside them; the function must still
// surface that one op_id and ignore the bad entries WITHOUT returning
// an error (per the docstring: "the walk yields the sorted unique
// set").
//
// NOTE: the WalkDir-callback-err arm is special — when filepath.WalkDir
// hits an error, it stops the walk and our callback returns the error,
// which then becomes the function's error. So we test this arm
// separately from the silent-skip arms.
func TestCollectFixtureOpIDsSkipsUnreadableAndMalformed(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses unix mode bits")
	}
	t.Parallel()
	root := t.TempDir()

	// Valid op
	good := filepath.Join(root, "good", "request.json")
	if err := os.MkdirAll(filepath.Dir(good), 0o755); err != nil {
		t.Fatalf("mkdir good: %v", err)
	}
	if err := os.WriteFile(good, []byte(`{"op_id":"valid.op"}`), 0o600); err != nil {
		t.Fatalf("write good: %v", err)
	}

	// Unreadable request.json → ReadFile err → silent skip
	unreadable := filepath.Join(root, "unreadable", "request.json")
	if err := os.MkdirAll(filepath.Dir(unreadable), 0o755); err != nil {
		t.Fatalf("mkdir unreadable: %v", err)
	}
	if err := os.WriteFile(unreadable, []byte(`{"op_id":"chmod.skipped"}`), 0o600); err != nil {
		t.Fatalf("plant unreadable: %v", err)
	}
	if err := os.Chmod(unreadable, 0o000); err != nil {
		t.Fatalf("chmod unreadable: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(unreadable, 0o600) })

	// Malformed request.json → json.Unmarshal err → silent skip
	malformed := filepath.Join(root, "malformed", "request.json")
	if err := os.MkdirAll(filepath.Dir(malformed), 0o755); err != nil {
		t.Fatalf("mkdir malformed: %v", err)
	}
	if err := os.WriteFile(malformed, []byte("{not json"), 0o600); err != nil {
		t.Fatalf("plant malformed: %v", err)
	}

	got, err := collectFixtureOpIDs(root)
	if err != nil {
		t.Fatalf("collectFixtureOpIDs: %v", err)
	}
	if len(got) != 1 || got[0] != "valid.op" {
		t.Errorf("got=%v; want [valid.op] (bad entries must skip silently)", got)
	}
}

// TestCollectFixtureOpIDsWalkDirCallbackErrPropagates pins the
// `WalkDir callback err → return err` arm (146-147). When the walker
// can't enter a subdirectory it invokes the callback with err != nil;
// our callback returns it, halting the walk and surfacing the error
// to the caller. This is distinct from the per-file silent-skip arms
// because directory traversal errors signal a bigger problem (likely
// fixture-tree corruption) and must NOT be masked.
func TestCollectFixtureOpIDsWalkDirCallbackErrPropagates(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses unix mode bits")
	}
	t.Parallel()
	root := t.TempDir()
	locked := filepath.Join(root, "locked")
	if err := os.MkdirAll(locked, 0o755); err != nil {
		t.Fatalf("mkdir locked: %v", err)
	}
	// chmod 0o000 on directory → WalkDir cannot ReadDir it → callback
	// receives a non-nil err.
	if err := os.Chmod(locked, 0o000); err != nil {
		t.Fatalf("chmod locked: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })

	_, err := collectFixtureOpIDs(root)
	if err == nil {
		t.Errorf("collectFixtureOpIDs(unreadable-subdir) err=nil; want WalkDir EACCES")
	}
}

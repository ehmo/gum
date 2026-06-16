package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDoctorCacheProbeWriteEISDIRReportsFailure pins the
// `os.WriteFile(.doctor-probe) err → OK=false, Summary="cache dir not
// writable"` arm. doctorCache uses an on-disk probe file to prove the
// cache dir is writable from the gum process; if the probe can't be
// created the doctor envelope MUST surface the failure with the
// underlying syscall error in Hint so the operator sees the exact
// reason (e.g. EISDIR, EACCES, ENOSPC) without re-running with -v.
func TestDoctorCacheProbeWriteEISDIRReportsFailure(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", tmp)

	// Pre-create the cache dir AND plant a directory at the probe path
	// so os.WriteFile fails with EISDIR (a non-ENOENT, non-EACCES
	// failure that's portable across macOS and linux).
	cacheDir := filepath.Join(tmp, "gum", "myprofile")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatalf("mkdir cacheDir: %v", err)
	}
	if err := os.Mkdir(filepath.Join(cacheDir, ".doctor-probe"), 0o755); err != nil {
		t.Fatalf("plant probe dir: %v", err)
	}

	got := doctorCache("myprofile")
	if got.OK {
		t.Error("OK=true with EISDIR-blocked probe; want false")
	}
	if got.Summary != "cache dir not writable" {
		t.Errorf("Summary=%q; want 'cache dir not writable'", got.Summary)
	}
	if got.Hint == "" || !strings.Contains(strings.ToLower(got.Hint), "is a directory") {
		t.Errorf("Hint=%q; want EISDIR-shaped hint", got.Hint)
	}
}

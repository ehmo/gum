package plugins

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestNewHostHomeDirErrorFallsBackToHomeEnv pins NewHost's
// `os.UserHomeDir err → home = os.Getenv("HOME")` arm
// (host.go:136-138). When HOME is unset os.UserHomeDir returns an
// error; the fallback then reads HOME again (returning "") so the
// resulting InstallRoot is the relative `.local/share/gum/plugins`
// path. Internal test inspects h.cfg.InstallRoot directly to prove
// the fallback path executed without panic. Skipped on Windows
// because UserHomeDir uses USERPROFILE there, not HOME.
func TestNewHostHomeDirErrorFallsBackToHomeEnv(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("UserHomeDir on Windows reads USERPROFILE, not HOME")
	}
	t.Setenv("HOME", "")
	h := NewHost(HostConfig{})
	want := filepath.Join(".local", "share", "gum", "plugins")
	if !strings.HasSuffix(h.cfg.InstallRoot, want) {
		t.Errorf("InstallRoot = %q; want suffix %q (HOME-empty fallback)", h.cfg.InstallRoot, want)
	}
}

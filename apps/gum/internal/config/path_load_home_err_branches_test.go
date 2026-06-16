package config_test

import (
	"runtime"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/config"
)

// TestPathHomeUnavailableSurfacesWrap pins the
// `os.UserHomeDir err → "config: resolve home:" wrap` arm of Path.
// Reached when XDG_CONFIG_HOME is unset AND $HOME is unset (CI
// sandboxes, k8s pods). The "config: resolve home:" prefix is the
// operator's grep handle to triage env-misconfig separately from
// "config file unreadable" downstream — without the prefix, an
// os.PathError would propagate that doesn't mention "config".
func TestPathHomeUnavailableSurfacesWrap(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("HOME-unset trick is darwin/linux-specific")
	}
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "")

	got, err := config.Path("alpha")
	if err == nil {
		t.Fatalf("Path(HOME=unset)=%q nil err; want UserHomeDir wrap", got)
	}
	if !strings.Contains(err.Error(), "config: resolve home") {
		t.Errorf("err=%q; want 'config: resolve home:' wrap", err)
	}
	if got != "" {
		t.Errorf("got=%q; want \"\" on err", got)
	}
}

// TestLoadPropagatesPathHomeError pins the
// `Path err → return nil, nil, err` arm of Load. Load wraps Path
// as its first step; any Path failure MUST short-circuit before
// the os.ReadFile call so the caller never sees a confusing
// "config: read :" wrap with an empty path. The propagation also
// preserves Path's "config: resolve home:" wrap so operators see
// the root cause without double-wrapping.
func TestLoadPropagatesPathHomeError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("HOME-unset trick is darwin/linux-specific")
	}
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "")

	cfg, warnings, err := config.Load("alpha")
	if err == nil {
		t.Fatal("Load(HOME=unset)=nil err; want Path err propagation")
	}
	if cfg != nil {
		t.Errorf("cfg=%v; want nil on err", cfg)
	}
	if warnings != nil {
		t.Errorf("warnings=%v; want nil on err", warnings)
	}
	// Path's "config: resolve home:" wrap MUST survive through Load
	// (no double wrap like "config: load: config: resolve home:").
	if !strings.Contains(err.Error(), "config: resolve home") {
		t.Errorf("err=%q; want 'config: resolve home:' from Path (proves Load didn't add a wrap)", err)
	}
	if strings.Count(err.Error(), "config:") > 1 {
		t.Errorf("err=%q; 'config:' appears more than once (Load added a redundant wrap?)", err)
	}
}

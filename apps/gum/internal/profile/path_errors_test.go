package profile

import (
	"runtime"
	"strings"
	"testing"
)

func TestPathsReportMissingHome(t *testing.T) {
	if runtime.GOOS == "windows" || runtime.GOOS == "plan9" {
		t.Skip("Unix home-directory lookup")
	}
	t.Setenv("HOME", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("XDG_CACHE_HOME", "")
	for _, tc := range []struct {
		name    string
		resolve func() (string, error)
	}{
		{"config", DefaultName.ConfigPath},
		{"data", DefaultName.DataDir},
		{"cache", DefaultName.CacheDir},
		{"notify", DefaultName.NotifyPath},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path, err := tc.resolve()
			if path != "" || err == nil || !strings.Contains(err.Error(), "HOME") {
				t.Fatalf("path=%q error=%v; want missing HOME error", path, err)
			}
		})
	}
}

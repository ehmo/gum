package gain_test

import (
	"testing"

	"github.com/ehmo/gum/internal/config"
	"github.com/ehmo/gum/internal/output/gain"
	"github.com/ehmo/gum/internal/profile"
)

// TestEnabledDefaultsOnWithAnUnreadableConfig pins the load-failure arm. A
// config gum cannot parse must not silently switch accounting off: a zero
// savings report would be indistinguishable from an idle profile.
func TestEnabledDefaultsOnWithAnUnreadableConfig(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv(gain.DisabledEnv, "")

	path, err := config.Path(profile.DefaultName.String())
	if err != nil {
		t.Fatalf("config.Path: %v", err)
	}
	mustWriteFile(t, path, "this line has no equals sign\n")

	if _, _, loadErr := config.Load(profile.DefaultName.String()); loadErr == nil {
		t.Fatal("config.Load err = nil; the fixture must be unparseable")
	}
	if !gain.Enabled(profile.DefaultName) {
		t.Error("an unreadable config must leave gain accounting on")
	}
}

package mcp

import (
	"testing"

	"github.com/ehmo/gum/internal/config"
)

// Spec §2689 makes `gum config set gain.enabled=false` the opt-out, and says
// gum.gain() then returns GAIN_DISABLED. handleGain checked only the
// GUM_GAIN_DISABLED env var, so a user who set the documented config key still
// got their savings reported back over MCP.
func TestGainConfigOptOutReturnsGainDisabled(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("GUM_GAIN_DISABLED", "")

	if err := config.Save("default", &config.Config{Values: map[string]string{"gain.enabled": "false"}}); err != nil {
		t.Fatalf("save config: %v", err)
	}

	body := invokeGainExpectError(t, NewServer(noopDispatcher{}))
	if body["error_code"] != "GAIN_DISABLED" {
		t.Errorf("error_code=%v want GAIN_DISABLED (%v)", body["error_code"], body)
	}
}

// gain.enabled=true is the same as an absent key: stats still report.
func TestGainConfigTrueKeepsReporting(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("GUM_GAIN_DISABLED", "")

	if err := config.Save("default", &config.Config{Values: map[string]string{"gain.enabled": "true"}}); err != nil {
		t.Fatalf("save config: %v", err)
	}

	body := invokeGainExpectSuccess(t, NewServer(noopDispatcher{}))
	if _, ok := body["savings_pct"]; !ok {
		t.Errorf("GainResult missing savings_pct: %v", body)
	}
}

// An unparseable value must not silently disable accounting.
func TestGainConfigGarbageValueKeepsReporting(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("GUM_GAIN_DISABLED", "")

	if err := config.Save("default", &config.Config{Values: map[string]string{"gain.enabled": "maybe"}}); err != nil {
		t.Fatalf("save config: %v", err)
	}

	body := invokeGainExpectSuccess(t, NewServer(noopDispatcher{}))
	if _, ok := body["savings_pct"]; !ok {
		t.Errorf("GainResult missing savings_pct: %v", body)
	}
}

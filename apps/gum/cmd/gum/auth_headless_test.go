package main

import (
	"runtime"
	"testing"
)

// TestIsHeadless_NonLinuxAlwaysFalse verifies the macOS/Windows shortcut:
// these platforms always have a usable session API, so the helper returns
// false without inspecting env vars or /proc.
func TestIsHeadless_NonLinuxAlwaysFalse(t *testing.T) {
	if runtime.GOOS == "linux" {
		t.Skip("linux-specific branches are exercised in TestIsHeadless_Linux below")
	}
	if isHeadless() {
		t.Errorf("isHeadless()=true on %s, want false (non-linux short-circuit)", runtime.GOOS)
	}
}

// TestIsHeadless_Linux exercises the linux-only branches by toggling DISPLAY /
// WAYLAND_DISPLAY and asserting the resulting classification.
func TestIsHeadless_Linux(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux-only branches; non-linux is covered above")
	}

	t.Run("display_set_means_not_headless", func(t *testing.T) {
		t.Setenv("DISPLAY", ":0")
		t.Setenv("WAYLAND_DISPLAY", "")
		if isHeadless() {
			t.Errorf("DISPLAY=:0 should not be headless")
		}
	})

	t.Run("wayland_set_means_not_headless", func(t *testing.T) {
		t.Setenv("DISPLAY", "")
		t.Setenv("WAYLAND_DISPLAY", "wayland-0")
		if isHeadless() {
			t.Errorf("WAYLAND_DISPLAY set should not be headless")
		}
	})
}

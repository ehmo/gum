package main

import (
	"runtime"
	"strings"
	"testing"
)

// TestResolveProfileDirHomeUnavailableSurfacesWrap pins the
// `os.UserHomeDir err → "resolve profile dir:" wrap` arm. Reached
// when XDG_DATA_HOME is unset AND $HOME is unset — the helper MUST
// surface a wrapped error so the caller can distinguish "config
// env not set" from "registry create failed downstream". The wrap
// label "resolve profile dir:" is the operator's grep handle for
// environment-misconfig in CI runners (k8s pods, sandboxes) where
// HOME may legitimately be unpopulated.
func TestResolveProfileDirHomeUnavailableSurfacesWrap(t *testing.T) {
	if runtime.GOOS == "windows" {
		// Windows uses %USERPROFILE% with different unsetting semantics;
		// the darwin/linux unset trick doesn't apply.
		t.Skip("HOME-unset trick is darwin/linux-specific")
	}
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("HOME", "")

	_, err := resolveProfileDir("alpha")
	if err == nil {
		t.Fatal("resolveProfileDir(HOME=unset)=nil err; want UserHomeDir wrap")
	}
	if !strings.Contains(err.Error(), "resolve profile dir") {
		t.Errorf("err=%q; want 'resolve profile dir:' wrap", err)
	}
}

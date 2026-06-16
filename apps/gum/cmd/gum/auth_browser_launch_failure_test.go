package main

import (
	"bytes"
	"strings"
	"testing"
)

// TestNewBrowserOpenerLaunchFailureIsNonFatal pins newBrowserOpener's
// `launchBrowser err → print fallback notice, return nil` arm
// (auth.go:285-290). Reached when noBrowser=false AND headlessFn returns
// false but the platform helper binary isn't on PATH. The spec requires
// that a failed launch be NON-FATAL because the auth URL has already
// been printed to stderr — the user can copy/paste it manually.
func TestNewBrowserOpenerLaunchFailureIsNonFatal(t *testing.T) {
	// Empty PATH so exec.Command("open"|"xdg-open"|"rundll32") fails to
	// resolve. Start() returns the LookPath err verbatim → triggers the
	// "Browser launch failed:" print + nil return.
	t.Setenv("PATH", "")

	var stderr bytes.Buffer
	opener := newBrowserOpener(&stderr, false, func() bool { return false })

	const url = "https://example/auth?state=x"
	if err := opener(url); err != nil {
		t.Fatalf("opener returned error on launch failure; spec says non-fatal: %v", err)
	}
	out := stderr.String()
	if !strings.Contains(out, url) {
		t.Errorf("stderr missing URL: %q", out)
	}
	if !strings.Contains(out, "Browser launch failed") {
		t.Errorf("stderr missing 'Browser launch failed' notice: %q", out)
	}
	if !strings.Contains(out, "URL above still works") {
		t.Errorf("stderr missing fallback hint: %q", out)
	}
}

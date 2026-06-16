// Spec gum-4v5o: the OAuth login flow must ALWAYS print the
// authorization URL to stderr before launching the browser so headless
// (SSH/devcontainer) users can recover. --no-browser suppresses launch
// but keeps the print.

package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestNewBrowserOpenerPrintsURLBeforeLaunch(t *testing.T) {
	var stderr bytes.Buffer
	// noBrowser=true so the test never actually launches anything; headlessFn=nil.
	opener := newBrowserOpener(&stderr, true, nil)

	const url = "https://accounts.google.com/o/oauth2/auth?state=abc&code_challenge=xyz"
	if err := opener(url); err != nil {
		t.Fatalf("opener: %v", err)
	}
	if !strings.Contains(stderr.String(), url) {
		t.Errorf("stderr missing URL: %q", stderr.String())
	}
	if !strings.Contains(stderr.String(), "Open this URL") {
		t.Errorf("stderr missing call-to-action: %q", stderr.String())
	}
	if !strings.Contains(stderr.String(), "--no-browser") {
		t.Errorf("stderr should mention --no-browser opt-out: %q", stderr.String())
	}
}

func TestNewBrowserOpenerHeadlessShortCircuit(t *testing.T) {
	var stderr bytes.Buffer
	opener := newBrowserOpener(&stderr, false, func() bool { return true })
	if err := opener("https://example/auth"); err != nil {
		t.Fatalf("opener: %v", err)
	}
	if !strings.Contains(stderr.String(), "https://example/auth") {
		t.Errorf("stderr missing URL: %q", stderr.String())
	}
	if !strings.Contains(stderr.String(), "headless") {
		t.Errorf("stderr should announce headless skip: %q", stderr.String())
	}
}

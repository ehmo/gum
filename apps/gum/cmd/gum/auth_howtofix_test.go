package main

import (
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/dispatch"
)

// TestHowToFixAuthUsesSetupCommand pins spec §7 lines 1378-1381: a non-gum_oauth
// auth failure must not be answered with a hint that browser OAuth alone fixes
// it. When the envelope names a setup_command, that command is the hint.
func TestHowToFixAuthUsesSetupCommand(t *testing.T) {
	se := dispatch.NewStructuredError(dispatch.ErrCodeAuthRequired, "compound auth requires multiple components").
		WithDetail("auth_strategy", "compound").
		WithDetail("missing_components", []string{"developer_token", "customer_id"}).
		WithDetail("setup_command", "gum auth setup googleads.generateKeywordIdeas")

	got := howToFix(se, nil)
	if !strings.Contains(got, "gum auth setup googleads.generateKeywordIdeas") {
		t.Errorf("hint does not name the setup command: %q", got)
	}
	if strings.Contains(got, "gum auth login") {
		t.Errorf("hint wrongly suggests browser OAuth for a compound failure: %q", got)
	}
	if !strings.Contains(got, "developer_token") {
		t.Errorf("hint does not name the missing components: %q", got)
	}
}

// TestHowToFixAuthNeverMentionsGcloud pins spec §1239: "There is no gcloud
// dependency". gum runs its own loopback redirect, so pointing the user at
// gcloud sends them to a tool gum does not use.
func TestHowToFixAuthNeverMentionsGcloud(t *testing.T) {
	for _, se := range []*dispatch.StructuredError{
		dispatch.NewStructuredError(dispatch.ErrCodeAuthRequired, "not logged in"),
		dispatch.NewStructuredError(dispatch.ErrCodeAuthRequired, "x").WithDetail("auth_strategy", "byo_oauth"),
	} {
		if got := howToFix(se, nil); strings.Contains(got, "gcloud") {
			t.Errorf("auth hint names gcloud: %q", got)
		}
	}
}

// TestHowToFixAuthKeychainCodeKeepsOwnHint pins that the codes the kernel used
// to flatten into AUTH_REQUIRED get their own remediation. Telling a user with
// a locked keychain to run `gum auth login` sends them through a login that
// will fail again at the same keychain write.
func TestHowToFixAuthKeychainCodeKeepsOwnHint(t *testing.T) {
	se := dispatch.NewStructuredError("AUTH_KEYCHAIN_UNAVAILABLE", "keychain locked").
		WithDetail("auth_strategy", "byo_oauth").
		WithDetail("user_message", "Unlock your login keychain and retry.")

	got := howToFix(se, nil)
	if !strings.Contains(got, "Unlock your login keychain") {
		t.Errorf("hint dropped the envelope's user_message: %q", got)
	}
}

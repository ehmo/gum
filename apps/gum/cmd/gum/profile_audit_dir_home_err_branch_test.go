package main

import (
	"runtime"
	"testing"
)

// TestProfileAuditDirHomeUnavailableSurfacesError pins the
// `os.UserHomeDir err → return "", err` arm of profileAuditDir.
// Reached when XDG_DATA_HOME is unset AND $HOME is unset — the
// audit-log writer caller (dispatch.NewDispatcherWithConfig) MUST
// see the err so it can fall back to a "no audit log" mode rather
// than silently writing to "<empty>/.local/share/gum/<profile>"
// which would land in the current working directory.
//
// profileAuditDir surfaces UserHomeDir's err verbatim (no wrap) so
// the caller's outer wrap stays the source-of-truth in audit-log
// failure messages.
func TestProfileAuditDirHomeUnavailableSurfacesError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("HOME-unset trick is darwin/linux-specific")
	}
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("HOME", "")

	got, err := profileAuditDir("alpha")
	if err == nil {
		t.Fatalf("profileAuditDir(HOME=unset)=%q nil err; want UserHomeDir surface", got)
	}
	if got != "" {
		t.Errorf("got=%q; want \"\" on err (don't leak partial path)", got)
	}
}

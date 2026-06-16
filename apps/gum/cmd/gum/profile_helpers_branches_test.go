package main

import (
	"path/filepath"
	"testing"
)

// TestResolveProfileFlagNilCmdReturnsDefault pins the `cmd == nil →
// return "default"` defensive arm. resolveProfileFlag is called from
// every audit/notify code-path; a nil cobra.Command (e.g. an out-of-
// band callsite or a test that hasn't constructed a full command tree)
// MUST still surface "default" rather than panic on cmd.Root().
func TestResolveProfileFlagNilCmdReturnsDefault(t *testing.T) {
	if got := resolveProfileFlag(nil); got != "default" {
		t.Errorf("resolveProfileFlag(nil)=%q; want default", got)
	}
}

// TestProfileAuditDirEmptyProfileNormalizesToDefault pins the
// `profile == "" → profile = "default"` arm. profileAuditDir is the
// audit.jsonl path resolver; an empty profile passed by the caller
// MUST map to the "default" profile so an early audit row never
// lands in a parentless `<dataHome>/gum//audit.jsonl` directory.
func TestProfileAuditDirEmptyProfileNormalizesToDefault(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmp)

	got, err := profileAuditDir("")
	if err != nil {
		t.Fatalf("profileAuditDir(\"\"): %v", err)
	}
	want := filepath.Join(tmp, "gum", "default")
	if got != want {
		t.Errorf("profileAuditDir(\"\")=%q; want %q", got, want)
	}
}

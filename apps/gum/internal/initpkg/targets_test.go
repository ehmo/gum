package initpkg

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestSupportedTargets locks the canonical ordered list. The list drives
// --help output and ResolveTarget's error message; reorderings need a doc
// update.
func TestSupportedTargets(t *testing.T) {
	got := SupportedTargets()
	want := []Target{TargetClaudeCode, TargetClaudeDesktop, TargetCursor}
	if len(got) != len(want) {
		t.Fatalf("SupportedTargets len = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("SupportedTargets[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// TestResolveTarget routes between the three host strategies and reports
// a useful error on unknown targets.
func TestResolveTarget(t *testing.T) {
	home := "/home/u"
	proj := "/work/p"

	t.Run("empty_target_routes_to_claude_code_project_local", func(t *testing.T) {
		got, err := ResolveTarget(home, proj, "", "", false)
		if err != nil {
			t.Fatalf("err = %v", err)
		}
		// Project-local => .claude under projectDir.
		want := filepath.Join(proj, ".claude", "settings.json")
		if got.Path != want {
			t.Errorf("Path = %q, want %q", got.Path, want)
		}
	})

	t.Run("explicit_claude_code_global", func(t *testing.T) {
		got, err := ResolveTarget(home, proj, "", TargetClaudeCode, true)
		if err != nil {
			t.Fatalf("err = %v", err)
		}
		want := filepath.Join(home, ".claude", "settings.json")
		if got.Path != want {
			t.Errorf("global Path = %q, want %q", got.Path, want)
		}
	})

	t.Run("cursor_target", func(t *testing.T) {
		got, err := ResolveTarget(home, proj, "", TargetCursor, false)
		if err != nil {
			t.Fatalf("err = %v", err)
		}
		want := filepath.Join(home, ".cursor", "mcp.json")
		if got.Path != want {
			t.Errorf("Path = %q, want %q", got.Path, want)
		}
	})

	t.Run("claude_desktop_target", func(t *testing.T) {
		got, err := ResolveTarget(home, proj, "", TargetClaudeDesktop, false)
		if err != nil {
			t.Fatalf("err = %v", err)
		}
		if !strings.Contains(got.Path, "claude_desktop_config.json") {
			t.Errorf("Path = %q, want claude_desktop_config.json suffix", got.Path)
		}
	})

	t.Run("unknown_target_errors", func(t *testing.T) {
		_, err := ResolveTarget(home, proj, "", "vscode", false)
		if err == nil {
			t.Fatal("err = nil, want unsupported error")
		}
		for _, want := range []string{"unsupported", "claude-code", "cursor"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("err = %q, missing %q", err, want)
			}
		}
	})
}

// TestClaudeDesktopTargetPerOS exercises the supported OS branches. The current
// runtime.GOOS determines which path is canonical; we assert the right tail
// for the live OS.
func TestClaudeDesktopTargetPerOS(t *testing.T) {
	home := "/home/u"

	switch runtime.GOOS {
	case "darwin":
		st, err := claudeDesktopTarget(home, "")
		if err != nil {
			t.Fatalf("darwin: err = %v", err)
		}
		want := filepath.Join(home, "Library", "Application Support", "Claude", "claude_desktop_config.json")
		if st.Path != want {
			t.Errorf("darwin Path = %q, want %q", st.Path, want)
		}
	case "linux":
		st, err := claudeDesktopTarget(home, "")
		if err != nil {
			t.Fatalf("linux: err = %v", err)
		}
		want := filepath.Join(home, ".config", "Claude", "claude_desktop_config.json")
		if st.Path != want {
			t.Errorf("linux Path = %q, want %q", st.Path, want)
		}
	}
}

// TestCursorTarget locks the global ~/.cursor/mcp.json path shape.
func TestCursorTarget(t *testing.T) {
	st := cursorTarget("/home/u")
	wantPath := filepath.Join("/home/u", ".cursor", "mcp.json")
	wantLock := filepath.Join("/home/u", ".cursor", "mcp.lock")
	if st.Path != wantPath {
		t.Errorf("Path = %q, want %q", st.Path, wantPath)
	}
	if st.LockPath != wantLock {
		t.Errorf("LockPath = %q, want %q", st.LockPath, wantLock)
	}
}

// TestJoinTargets verifies the trivial join helper produces a comma-space
// list (used inside ResolveTarget's error message).
func TestJoinTargets(t *testing.T) {
	got := joinTargets([]Target{"a", "b", "c"})
	if got != "a, b, c" {
		t.Errorf("got %q, want %q", got, "a, b, c")
	}
	if joinTargets(nil) != "" {
		t.Errorf("nil → non-empty: %q", joinTargets(nil))
	}
}

// TestFormatDiff covers both render branches: "create" for fresh paths and
// the default "update" for everything else. The PatchedBytes are appended
// verbatim after the header.
func TestFormatDiff(t *testing.T) {
	t.Run("create_action", func(t *testing.T) {
		got := FormatDiff(&PatchPlan{
			Action:       "create",
			Path:         "/tmp/x.json",
			PatchedBytes: []byte("{}\n"),
		})
		if !strings.HasPrefix(got, "CREATE /tmp/x.json\n") {
			t.Errorf("missing CREATE header: %q", got)
		}
		if !strings.Contains(got, "{}\n") {
			t.Errorf("missing body: %q", got)
		}
	})

	t.Run("update_action_is_default", func(t *testing.T) {
		got := FormatDiff(&PatchPlan{
			Action:       "update",
			Path:         "/tmp/y.json",
			PatchedBytes: []byte(`{"k":1}`),
		})
		if !strings.HasPrefix(got, "UPDATE /tmp/y.json\n") {
			t.Errorf("missing UPDATE header: %q", got)
		}
	})

	t.Run("unknown_action_falls_back_to_update", func(t *testing.T) {
		got := FormatDiff(&PatchPlan{
			Action: "whatever",
			Path:   "/tmp/z.json",
		})
		if !strings.HasPrefix(got, "UPDATE /tmp/z.json\n") {
			t.Errorf("got %q, want UPDATE header (default branch)", got)
		}
	})
}

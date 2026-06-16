package initpkg

import (
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
)

// Target is the MCP-host config file `gum init` will patch.
//
// All three supported hosts share the same JSON shape under
// mcpServers.<name>.{command,args}, so the PlanPatch/Apply machinery is
// reused as-is — only the destination file and "is this project-local?"
// behavior change between targets.
type Target string

const (
	TargetClaudeCode    Target = "claude-code"
	TargetClaudeDesktop Target = "claude-desktop"
	TargetCursor        Target = "cursor"
)

// SupportedTargets is the canonical list rendered in `gum init --help` and
// validated by ResolveTarget. Kept in declaration order so the help text is
// stable across releases.
func SupportedTargets() []Target {
	return []Target{TargetClaudeCode, TargetClaudeDesktop, TargetCursor}
}

// ResolveTarget maps a target keyword to the concrete SettingsTarget gum
// will patch. The global flag is only honored for claude-code (the others
// have a single canonical location per OS).
//
// projectDir is read literally; callers pass their resolved working
// directory. envAppData is retained for API stability and ignored on the
// supported release platforms.
func ResolveTarget(homeDir, projectDir, envAppData string, target Target, global bool) (SettingsTarget, error) {
	switch target {
	case "", TargetClaudeCode:
		return ResolveSettingsTarget(homeDir, projectDir, global), nil
	case TargetClaudeDesktop:
		return claudeDesktopTarget(homeDir, envAppData)
	case TargetCursor:
		return cursorTarget(homeDir), nil
	}
	return SettingsTarget{}, fmt.Errorf("initpkg: unsupported --target %q; want one of: %s",
		target, joinTargets(SupportedTargets()))
}

// claudeDesktopTarget returns the per-OS Claude Desktop config path.
// References:
//   - macOS:   ~/Library/Application Support/Claude/claude_desktop_config.json
//   - Linux:   ~/.config/Claude/claude_desktop_config.json
func claudeDesktopTarget(homeDir, _ string) (SettingsTarget, error) {
	var dir string
	switch runtime.GOOS {
	case "darwin":
		dir = filepath.Join(homeDir, "Library", "Application Support", "Claude")
	default:
		dir = filepath.Join(homeDir, ".config", "Claude")
	}
	return SettingsTarget{
		Path:     filepath.Join(dir, "claude_desktop_config.json"),
		LockPath: filepath.Join(dir, "claude_desktop_config.lock"),
	}, nil
}

// cursorTarget returns the single canonical Cursor MCP config path. Cursor
// uses a global ~/.cursor/mcp.json (no project-local equivalent in the IDE's
// public docs as of v0.40+).
func cursorTarget(homeDir string) SettingsTarget {
	dir := filepath.Join(homeDir, ".cursor")
	return SettingsTarget{
		Path:     filepath.Join(dir, "mcp.json"),
		LockPath: filepath.Join(dir, "mcp.lock"),
	}
}

func joinTargets(ts []Target) string {
	s := make([]string, len(ts))
	for i, t := range ts {
		s[i] = string(t)
	}
	return strings.Join(s, ", ")
}

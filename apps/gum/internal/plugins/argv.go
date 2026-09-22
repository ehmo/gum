package plugins

import (
	"fmt"
	"path/filepath"
	"strings"
)

// NormalizeArgv implements spec §8.7 "Install-time command normalization".
//
// The manifest's `command` is an author-provided selector, not the runtime
// argv. Install resolves it against the verified install root and records
// the result as `argv_normalized` in the profile's plugins.lock; the spawn
// path launches that argv and nothing else.
//
// Resolution dispatches on the manifest's [package] source. For `local`,
// `bundled`, `github_release`, and `git`, resolution is exact: command[0]
// MUST name the manifest's declared `executable`, and the residual tokens
// become argv_normalized[1:]. A PATH-only token, a shell interpreter, or a
// wrapper script outside the install root therefore fails with
// PLUGIN_EXECUTABLE_UNTRUSTED before any process starts. An empty `command`
// means the executable takes no arguments.
//
// For `pypi`, command[0] is the author's resolver name (`uvx`, `pipx`), not
// the executable: gum built the venv itself, so the resolver token is
// dropped, the next token is the console script, and the argv binds to
// `<install_dir>/venv/bin/<script>` (§8.7's `uvx fli mcp` →
// `<install_root>/venv/bin/fli mcp` example).
//
// Dev profiles keep the §8.7 escape hatch on the exact-match rule: an
// unresolvable command[0] is accepted there and the declared executable
// still wins, because a local checkout is already marked dev-untrusted in
// inventory. The pypi rule has no escape hatch; the venv layout is gum's
// own, so a mismatch is always an error.
func NormalizeArgv(installDir string, m *Manifest, profileIsDev bool) ([]string, error) {
	if m == nil {
		return nil, fmt.Errorf("%w: nil manifest", ErrExecutableUntrusted)
	}
	if m.Package.Kind() == SourcePyPI {
		return normalizePyPIArgv(installDir, m)
	}
	execPath := filepath.Join(installDir, m.Executable)
	if len(m.Command) == 0 {
		return []string{execPath}, nil
	}

	selector := strings.TrimSpace(m.Command[0])
	if !selectorMatchesExecutable(selector, m.Executable) && !profileIsDev {
		return nil, fmt.Errorf("%w: plugin %s command[0] %q does not resolve to declared executable %q",
			ErrExecutableUntrusted, m.PluginID, m.Command[0], m.Executable)
	}

	argv := make([]string, 0, len(m.Command))
	argv = append(argv, execPath)
	argv = append(argv, m.Command[1:]...)
	return argv, nil
}

// pypiResolverTokens are the PATH-only launcher names a PyPI manifest's
// command may lead with. They never survive normalization: gum resolves the
// package itself, so the resolver names a convention, not a process.
var pypiResolverTokens = map[string]bool{
	"uvx":  true,
	"pipx": true,
}

// normalizePyPIArgv binds a pypi plugin's argv to the console script inside
// the install's own venv. An optional leading resolver token is dropped;
// the next token names the console script; the manifest's declared
// executable must be exactly `venv/bin/<script>`.
func normalizePyPIArgv(installDir string, m *Manifest) ([]string, error) {
	execPath := filepath.Join(installDir, m.Executable)
	if len(m.Command) == 0 {
		return []string{execPath}, nil
	}

	tokens := m.Command
	if pypiResolverTokens[strings.TrimSpace(tokens[0])] {
		tokens = tokens[1:]
	}
	if len(tokens) == 0 {
		return nil, fmt.Errorf("%w: plugin %s pypi command names a resolver and no console script",
			ErrExecutableUntrusted, m.PluginID)
	}

	script := strings.TrimSpace(tokens[0])
	want := filepath.Join("venv", "bin", script)
	if script == "" || strings.ContainsRune(script, '/') || filepath.Clean(m.Executable) != want {
		return nil, fmt.Errorf("%w: plugin %s pypi console script %q does not resolve to declared executable %q",
			ErrExecutableUntrusted, m.PluginID, tokens[0], m.Executable)
	}

	argv := make([]string, 0, len(tokens))
	argv = append(argv, filepath.Join(installDir, want))
	argv = append(argv, tokens[1:]...)
	return argv, nil
}

// selectorMatchesExecutable reports whether the author's command[0] names the
// declared executable. `./bin/mcp` and `bin/mcp` both match `bin/mcp`; an
// absolute path, a bare PATH name, and a traversal do not.
func selectorMatchesExecutable(selector, executable string) bool {
	if selector == "" || filepath.IsAbs(selector) {
		return false
	}
	return filepath.Clean(selector) == filepath.Clean(executable)
}

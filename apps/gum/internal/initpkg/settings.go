package initpkg

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// DefaultLockTimeout is the spec §12.2 ceiling for acquiring the patch lock
// (30s, matches §8.7 plugins.install.lock).
const DefaultLockTimeout = 30 * time.Second

// MCPEntry is the JSON value `gum init` adds to mcpServers.gum in the
// target settings.json (spec §12.2 step 2). Encoded with stable key order.
type MCPEntry struct {
	Command string   `json:"command"`
	Args    []string `json:"args"`
}

// DefaultMCPEntry is the spec §12.2 entry inserted under mcpServers.gum.
func DefaultMCPEntry() MCPEntry {
	return MCPEntry{Command: "gum", Args: []string{"mcp", "--stdio"}}
}

// SettingsTarget describes the file `gum init` is about to patch.
//
// Path is the absolute settings.json path. LockPath is the per-directory
// advisory lock file (spec §12.2 normative "Atomic settings.json patch").
type SettingsTarget struct {
	Path     string
	LockPath string
}

// ResolveSettingsTarget returns the user-global path (~/.claude/settings.json)
// when global=true, otherwise the project-local path (.claude/settings.json
// rooted at projectDir). projectDir is taken literally; callers pass their
// resolved working directory.
func ResolveSettingsTarget(homeDir, projectDir string, global bool) SettingsTarget {
	if global {
		dir := filepath.Join(homeDir, ".claude")
		return SettingsTarget{
			Path:     filepath.Join(dir, "settings.json"),
			LockPath: filepath.Join(dir, "settings.lock"),
		}
	}
	dir := filepath.Join(projectDir, ".claude")
	return SettingsTarget{
		Path:     filepath.Join(dir, "settings.json"),
		LockPath: filepath.Join(dir, "settings.lock"),
	}
}

// PatchPlan is the diff `gum init` previews to the user before applying.
//
// Before is the existing JSON-decoded settings (nil if the file does not
// exist). After is the merged-in result. PatchedBytes is the canonical
// pretty-printed JSON to write. Action is "create" or "update" depending on
// whether the file existed.
type PatchPlan struct {
	Action       string
	Path         string
	Before       map[string]any
	After        map[string]any
	PatchedBytes []byte
	NoOp         bool // true when the file already has the exact target entry.
}

// PlanPatch computes the diff for inserting entry under mcpServers[name] in
// the target file. The function does not acquire the file lock; callers use
// Apply for the read-merge-write sequence.
func PlanPatch(target SettingsTarget, name string, entry MCPEntry) (*PatchPlan, error) {
	plan := &PatchPlan{Path: target.Path}
	raw, err := os.ReadFile(target.Path)
	if errors.Is(err, os.ErrNotExist) {
		plan.Action = "create"
		plan.Before = nil
	} else if err != nil {
		return nil, fmt.Errorf("initpkg: read %s: %w", target.Path, err)
	} else {
		plan.Action = "update"
		var existing map[string]any
		if jerr := json.Unmarshal(raw, &existing); jerr != nil {
			return nil, fmt.Errorf("initpkg: %s is not valid JSON: %w", target.Path, jerr)
		}
		plan.Before = existing
	}
	merged := cloneMap(plan.Before)
	// cloneMap is shallow, so merged["mcpServers"] still aliases plan.Before's
	// nested map. Clone it before mutating, or the servers[name]=... write below
	// would corrupt plan.Before (the documented pre-patch snapshot).
	servers, _ := merged["mcpServers"].(map[string]any)
	if servers == nil {
		servers = map[string]any{}
	} else {
		servers = cloneMap(servers)
	}
	if existing, ok := servers[name].(map[string]any); ok {
		if entryEqualsMap(entry, existing) {
			plan.NoOp = true
		}
	}
	servers[name] = mcpEntryAsMap(entry)
	merged["mcpServers"] = servers
	plan.After = merged

	encoded, err := canonicalJSON(merged)
	if err != nil {
		return nil, fmt.Errorf("initpkg: encode merged settings: %w", err)
	}
	plan.PatchedBytes = encoded
	return plan, nil
}

// Apply writes plan.PatchedBytes to plan.Path atomically while holding the
// advisory lock on target.LockPath for the duration of the read-merge-write.
//
// timeout is the lock-acquisition ceiling (spec §12.2 default 30s).
func Apply(target SettingsTarget, plan *PatchPlan, timeout time.Duration) error {
	if plan.NoOp {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(target.Path), 0o755); err != nil {
		return fmt.Errorf("initpkg: mkdir %s: %w", filepath.Dir(target.Path), err)
	}
	release, err := acquireSettingsLock(target.LockPath, timeout)
	if err != nil {
		return err
	}
	defer func() { _ = release() }()
	return atomicWrite(target.Path, plan.PatchedBytes, 0o644)
}

// WriteGUMmd renders the embedded template at the requested path. global=true
// chooses ~/GUM.md; otherwise <projectDir>/GUM.md. Writes are atomic and use
// 0644 mode (the file is non-secret).
func WriteGUMmd(homeDir, projectDir, version string, global bool) (string, error) {
	body, err := RenderGUMmd(version)
	if err != nil {
		return "", err
	}
	var dest string
	if global {
		dest = filepath.Join(homeDir, "GUM.md")
	} else {
		dest = filepath.Join(projectDir, "GUM.md")
	}
	return dest, atomicWrite(dest, body, 0o644)
}

// canonicalJSON encodes v as pretty-printed JSON with stable key ordering at
// every depth. Using json.Marshal directly would produce a single line; the
// diff path needs human-readable output and stable ordering for golden tests.
func canonicalJSON(v any) ([]byte, error) {
	sorted := sortKeys(v)
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(sorted); err != nil {
		return nil, err
	}
	out := bytes.TrimRight(buf.Bytes(), "\n")
	out = append(out, '\n')
	return out, nil
}

// sortKeys deep-copies v with map keys ordered. Slices and scalars pass through.
func sortKeys(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		// Marshal honors insertion order of map[string]any; cycle through json.
		// To force key order we round-trip via json.RawMessage with a sorted
		// intermediate type below.
		for _, k := range keys {
			out[k] = sortKeys(t[k])
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, e := range t {
			out[i] = sortKeys(e)
		}
		return out
	default:
		return v
	}
}

// cloneMap returns a shallow copy of m. Returns a fresh empty map when m is nil.
func cloneMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m)+1)
	for k, v := range m {
		out[k] = v
	}
	return out
}

func mcpEntryAsMap(e MCPEntry) map[string]any {
	args := make([]any, len(e.Args))
	for i, a := range e.Args {
		args[i] = a
	}
	return map[string]any{"command": e.Command, "args": args}
}

func entryEqualsMap(e MCPEntry, m map[string]any) bool {
	cmd, _ := m["command"].(string)
	if cmd != e.Command {
		return false
	}
	rawArgs, _ := m["args"].([]any)
	if len(rawArgs) != len(e.Args) {
		return false
	}
	for i, a := range rawArgs {
		s, _ := a.(string)
		if s != e.Args[i] {
			return false
		}
	}
	return true
}

// atomicWrite writes data to path via a same-directory temp file then renames.
// Mode applies to the destination after rename.
func atomicWrite(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".gum-init-*.tmp")
	if err != nil {
		return fmt.Errorf("initpkg: tempfile in %s: %w", dir, err)
	}
	tmpPath := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpPath) }
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("initpkg: write %s: %w", tmpPath, err)
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("initpkg: chmod %s: %w", tmpPath, err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("initpkg: close %s: %w", tmpPath, err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		cleanup()
		return fmt.Errorf("initpkg: rename %s -> %s: %w", tmpPath, path, err)
	}
	return nil
}

// FormatDiff returns a human-readable presentation of plan for the user
// preview before --yes confirmation. Format is intentionally simple: action
// header + canonical JSON body. CI scripts that grep this output should match
// on the first line only.
func FormatDiff(plan *PatchPlan) string {
	var buf bytes.Buffer
	switch plan.Action {
	case "create":
		fmt.Fprintf(&buf, "CREATE %s\n", plan.Path)
	default:
		fmt.Fprintf(&buf, "UPDATE %s\n", plan.Path)
	}
	buf.Write(plan.PatchedBytes)
	return buf.String()
}

package agents

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ehmo/gum/internal/fsatomic"
	skillreg "github.com/ehmo/gum/internal/skills"
)

const (
	TargetAll    = "all"
	TargetCodex  = "codex"
	TargetClaude = "claude"
	TargetCursor = "cursor"
	TargetGemini = "gemini"

	ScopeUser    = "user"
	ScopeProject = "project"

	FeatureSkills = "skills"
	FeatureMCP    = "mcp"

	DefaultToolset = "core"
)

var supportedTargets = []string{TargetCodex, TargetClaude, TargetCursor, TargetGemini}

type Options struct {
	Target   string
	Scope    string
	Features []string
	Toolset  string
	MCPArgs  []string
	Force    bool
	DryRun   bool
	HomeDir  string
	WorkDir  string
}

type Plan struct {
	Target   string   `json:"target"`
	Scope    string   `json:"scope"`
	Features []string `json:"features"`
	Toolset  string   `json:"toolset"`
	DryRun   bool     `json:"dry_run,omitempty"`
	Actions  []Action `json:"actions"`
}

type Action struct {
	Target string `json:"target"`
	Kind   string `json:"kind"`
	Path   string `json:"path"`
	Status string `json:"status"`
}

type fileWrite struct {
	target   string
	kind     string
	path     string
	contents string
	merge    func(existing []byte) ([]byte, error)
}

func Install(opts Options) (Plan, error) {
	opts = normalizeOptions(opts)
	if err := validateOptions(opts); err != nil {
		return Plan{}, err
	}
	writes, err := planWrites(opts)
	if err != nil {
		return Plan{}, err
	}
	writes = dedupeWrites(writes)
	plan := Plan{
		Target:   opts.Target,
		Scope:    opts.Scope,
		Features: append([]string{}, opts.Features...),
		Toolset:  opts.Toolset,
		DryRun:   opts.DryRun,
		Actions:  make([]Action, 0, len(writes)),
	}
	for _, write := range writes {
		status := "wrote"
		if opts.DryRun {
			status = "would_write"
			plan.Actions = append(plan.Actions, Action{Target: write.target, Kind: write.kind, Path: write.path, Status: status})
			continue
		}
		safePath, err := resolveSafeWrite(write.path)
		if err != nil {
			return Plan{}, err
		}
		exists, err := regularFileExists(safePath)
		if err != nil {
			return Plan{}, err
		}
		if exists && write.merge == nil && !opts.Force {
			plan.Actions = append(plan.Actions, Action{Target: write.target, Kind: write.kind, Path: write.path, Status: "skipped"})
			continue
		}
		if err := os.MkdirAll(filepath.Dir(safePath), 0o700); err != nil {
			return Plan{}, err
		}
		contents := []byte(write.contents)
		if write.merge != nil {
			existing, err := os.ReadFile(safePath)
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				return Plan{}, err
			}
			contents, err = write.merge(existing)
			if err != nil {
				return Plan{}, err
			}
		}
		if err := fsatomic.WriteFile(safePath, contents, 0o600); err != nil {
			return Plan{}, err
		}
		plan.Actions = append(plan.Actions, Action{Target: write.target, Kind: write.kind, Path: write.path, Status: status})
	}
	return plan, nil
}

func normalizeOptions(opts Options) Options {
	if opts.Target == "" {
		opts.Target = TargetAll
	}
	if opts.Scope == "" {
		opts.Scope = ScopeUser
	}
	if len(opts.Features) == 0 {
		opts.Features = []string{FeatureSkills, FeatureMCP}
	}
	if opts.Toolset == "" {
		opts.Toolset = DefaultToolset
	}
	return opts
}

func validateOptions(opts Options) error {
	if opts.Target != TargetAll && !contains(supportedTargets, opts.Target) {
		return fmt.Errorf("unsupported agents target: %s", opts.Target)
	}
	if opts.Scope != ScopeUser && opts.Scope != ScopeProject {
		return fmt.Errorf("unsupported agents scope: %s", opts.Scope)
	}
	if opts.Toolset != DefaultToolset {
		return fmt.Errorf("unsupported MCP toolset: %s", opts.Toolset)
	}
	for _, feature := range opts.Features {
		if feature != FeatureSkills && feature != FeatureMCP {
			return fmt.Errorf("unsupported agents feature: %s", feature)
		}
	}
	for _, arg := range opts.MCPArgs {
		if arg == "" || strings.ContainsAny(arg, "\x00\r\n") {
			return fmt.Errorf("invalid MCP arg: %q", arg)
		}
	}
	return nil
}

func planWrites(opts Options) ([]fileWrite, error) {
	home, work, err := roots(opts)
	if err != nil {
		return nil, err
	}
	targets := []string{opts.Target}
	if opts.Target == TargetAll {
		targets = append([]string{}, supportedTargets...)
	}
	var writes []fileWrite
	for _, target := range targets {
		if hasFeature(opts.Features, FeatureSkills) {
			skillRoot := skillRootFor(target, opts.Scope, home, work)
			for _, item := range skillreg.DefaultInstallableSkills() {
				for _, file := range item.Files {
					rel, err := installableRelativePath(item.Directory, file.Path)
					if err != nil {
						return nil, err
					}
					writes = append(writes, fileWrite{
						target:   target,
						kind:     FeatureSkills,
						path:     filepath.Join(skillRoot, rel),
						contents: file.Contents,
					})
				}
			}
		}
		if hasFeature(opts.Features, FeatureMCP) {
			writes = append(writes, mcpWriteFor(target, opts.Scope, home, work, opts.Toolset, opts.MCPArgs))
		}
	}
	return writes, nil
}

func roots(opts Options) (string, string, error) {
	home := opts.HomeDir
	if home == "" {
		var err error
		home, err = os.UserHomeDir()
		if err != nil {
			return "", "", fmt.Errorf("resolve home directory: %w", err)
		}
	}
	work := opts.WorkDir
	if work == "" {
		var err error
		work, err = os.Getwd()
		if err != nil {
			return "", "", fmt.Errorf("resolve working directory: %w", err)
		}
	}
	var err error
	home, err = canonicalRoot(home)
	if err != nil {
		return "", "", fmt.Errorf("resolve home directory: %w", err)
	}
	work, err = canonicalRoot(work)
	if err != nil {
		return "", "", fmt.Errorf("resolve working directory: %w", err)
	}
	return home, work, nil
}

func canonicalRoot(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", err
	}
	return real, nil
}

func skillRootFor(target, scope, home, work string) string {
	if target == TargetClaude {
		if scope == ScopeProject {
			return filepath.Join(work, ".claude", "skills")
		}
		return filepath.Join(home, ".claude", "skills")
	}
	if scope == ScopeProject {
		return filepath.Join(work, ".agents", "skills")
	}
	return filepath.Join(home, ".agents", "skills")
}

func mcpWriteFor(target, scope, home, work, toolset string, extraArgs []string) fileWrite {
	switch target {
	case TargetCodex:
		path := filepath.Join(home, ".codex", "config.toml")
		if scope == ScopeProject {
			path = filepath.Join(work, ".codex", "config.toml")
		}
		return fileWrite{
			target: target,
			kind:   FeatureMCP,
			path:   path,
			merge:  func(existing []byte) ([]byte, error) { return mergeCodexTOML(existing, toolset, extraArgs) },
		}
	case TargetClaude:
		path := filepath.Join(home, ".claude", "mcp.json")
		if scope == ScopeProject {
			path = filepath.Join(work, ".mcp.json")
		}
		return jsonMCPWrite(target, path, toolset, extraArgs)
	case TargetCursor:
		path := filepath.Join(home, ".cursor", "mcp.json")
		if scope == ScopeProject {
			path = filepath.Join(work, ".cursor", "mcp.json")
		}
		return jsonMCPWrite(target, path, toolset, extraArgs)
	case TargetGemini:
		path := filepath.Join(home, ".gemini", "settings.json")
		if scope == ScopeProject {
			path = filepath.Join(work, ".gemini", "settings.json")
		}
		return jsonMCPWrite(target, path, toolset, extraArgs)
	default:
		return fileWrite{target: target, kind: FeatureMCP}
	}
}

func jsonMCPWrite(target, path, toolset string, extraArgs []string) fileWrite {
	return fileWrite{
		target: target,
		kind:   FeatureMCP,
		path:   path,
		merge:  func(existing []byte) ([]byte, error) { return mergeMCPJSON(existing, toolset, extraArgs) },
	}
}

func mergeMCPJSON(existing []byte, toolset string, extraArgs []string) ([]byte, error) {
	root := map[string]any{}
	if len(strings.TrimSpace(string(existing))) > 0 {
		if err := json.Unmarshal(existing, &root); err != nil {
			return nil, fmt.Errorf("parse existing MCP JSON: %w", err)
		}
	}
	servers, _ := root["mcpServers"].(map[string]any)
	if servers == nil {
		servers = map[string]any{}
		root["mcpServers"] = servers
	}
	servers["gum"] = map[string]any{
		"command": "gum",
		"args":    mcpArgs(toolset, extraArgs),
	}
	data, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func mergeCodexTOML(existing []byte, toolset string, extraArgs []string) ([]byte, error) {
	text := string(existing)
	begin := "# BEGIN gum managed mcp server"
	end := "# END gum managed mcp server"
	block := begin + "\n" +
		"[mcp_servers.gum]\n" +
		"command = \"gum\"\n" +
		"args = [" + quoteTOMLStrings(mcpArgs(toolset, extraArgs)) + "]\n" +
		end + "\n"
	start := strings.Index(text, begin)
	if start >= 0 {
		stop := strings.Index(text[start:], end)
		if stop < 0 {
			return nil, errors.New("existing gum managed MCP block is missing end marker")
		}
		stop = start + stop + len(end)
		replaced := strings.TrimRight(text[:start], "\n") + "\n\n" + block + strings.TrimLeft(text[stop:], "\n")
		return []byte(replaced), nil
	}
	if strings.Contains(text, "[mcp_servers.gum]") {
		return nil, errors.New("mcp_servers.gum already exists outside the managed block")
	}
	if strings.TrimSpace(text) == "" {
		return []byte(block), nil
	}
	return []byte(strings.TrimRight(text, "\n") + "\n\n" + block), nil
}

func mcpArgs(_ string, extra []string) []string {
	args := []string{"mcp", "--stdio"}
	return append(args, extra...)
}

func quoteTOMLStrings(values []string) string {
	quoted := make([]string, 0, len(values))
	for _, value := range values {
		data, _ := json.Marshal(value)
		quoted = append(quoted, string(data))
	}
	return strings.Join(quoted, ", ")
}

func resolveSafeWrite(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if err := rejectExistingParentSymlinks(abs); err != nil {
		return "", err
	}
	if st, err := os.Lstat(abs); err == nil {
		if st.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("path contains symlink component: %s", path)
		}
		if st.IsDir() {
			return "", fmt.Errorf("path is a directory: %s", path)
		}
	} else if !os.IsNotExist(err) {
		return "", err
	}
	return abs, nil
}

func rejectExistingParentSymlinks(path string) error {
	parent := filepath.Dir(path)
	volume := filepath.VolumeName(parent)
	rest := strings.TrimPrefix(parent, volume)
	root := volume + string(os.PathSeparator)
	cur := root
	for _, part := range strings.Split(strings.Trim(rest, string(os.PathSeparator)), string(os.PathSeparator)) {
		if part == "" || part == "." {
			continue
		}
		cur = filepath.Join(cur, part)
		st, err := os.Lstat(cur)
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if st.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("path contains symlink component: %s", path)
		}
		if !st.IsDir() {
			return fmt.Errorf("path component is not a directory: %s", cur)
		}
	}
	return nil
}

func regularFileExists(path string) (bool, error) {
	st, err := os.Lstat(path)
	if err == nil {
		if st.Mode()&os.ModeSymlink != 0 {
			return false, fmt.Errorf("path contains symlink component: %s", path)
		}
		if st.IsDir() {
			return false, fmt.Errorf("path is a directory: %s", path)
		}
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

func dedupeWrites(writes []fileWrite) []fileWrite {
	seen := map[string]int{}
	out := make([]fileWrite, 0, len(writes))
	for _, write := range writes {
		key := filepath.Clean(write.path)
		if idx, ok := seen[key]; ok {
			if !containsTarget(out[idx].target, write.target) {
				out[idx].target += "," + write.target
			}
			continue
		}
		seen[key] = len(out)
		out = append(out, write)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].path < out[j].path
	})
	return out
}

func installableRelativePath(dir, file string) (string, error) {
	if dir == "" || filepath.Clean(dir) != dir || filepath.IsAbs(dir) || strings.ContainsAny(dir, `/\`) || strings.HasPrefix(dir, ".") {
		return "", fmt.Errorf("invalid installable skill directory: %s", dir)
	}
	clean := filepath.Clean(file)
	if clean == "." || filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("invalid installable skill file path: %s", file)
	}
	return filepath.Join(dir, clean), nil
}

func hasFeature(features []string, want string) bool {
	return contains(features, want)
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func containsTarget(raw, target string) bool {
	for _, part := range strings.Split(raw, ",") {
		if part == target {
			return true
		}
	}
	return false
}

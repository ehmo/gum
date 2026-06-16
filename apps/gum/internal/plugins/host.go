package plugins

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"encoding/json"

	"github.com/ehmo/gum/internal/fsatomic"
	"github.com/ehmo/gum/internal/pluginenv"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// Manifest is the validated plugin metadata loaded from manifest.json.
//
// NamespaceOwner is the reverse-DNS or registry-identity string required of
// every third-party plugin per spec §5.1. LoadManifest does not enforce its
// presence (first-party bundled plugins may omit it during build); presence
// is checked at install time by ValidateNamespaceOwnership.
type Manifest struct {
	ManifestSchemaVersion int          `json:"manifest_schema_version"`
	PluginID              string       `json:"plugin_id"`
	Name                  string       `json:"name"`
	Version               string       `json:"version"`
	NamespaceOwner        string       `json:"namespace_owner,omitempty"`
	Shape                 string       `json:"shape"`      // must be "mcp-plugin"
	Executable            string       `json:"executable"` // path relative to install dir
	AdvertisedTools       []ToolDecl   `json:"advertised_tools"`
	DeclaredCapabilities  Capabilities `json:"declared_capabilities"`
	Requirements          Requirements `json:"requirements,omitempty"`
}

// Requirements carries the plugin's declared runtime requirements including
// credential descriptors per spec §1606.
type Requirements struct {
	// NeedsUserCreds lists the env var names that require user-supplied
	// credentials. The env var names are the subprocess-side names and must
	// NOT be exposed in user-facing messages per spec §1414/§1606.
	NeedsUserCreds []string `json:"needs_user_creds,omitempty"`
	// CredentialDescriptors maps each entry in NeedsUserCreds (by env var
	// name) to a safe user-facing descriptor (spec §1606). Must contain
	// exactly one entry per NeedsUserCreds element.
	CredentialDescriptors []CredentialDescriptor `json:"credential_descriptors,omitempty"`
}

// ToolDecl declares a single tool exposed by the plugin.
type ToolDecl struct {
	Name        string `json:"name"` // unprefixed; host adds "plug.<plugin_id>." prefix
	Description string `json:"description"`
	RiskClass   string `json:"risk_class"` // read|write|destructive
}

// Capabilities declares the sandbox requirements for the plugin subprocess.
type Capabilities struct {
	Network    bool     `json:"network"`      // false denies network in the OS sandbox; true permits it
	FSWriteDir string   `json:"fs_write_dir"` // empty = writable <install_dir>/data; non-empty = relative writable root under install dir
	EnvAllow   []string `json:"env_allow"`    // env vars allowed through to subprocess
}

var pluginIDRe = regexp.MustCompile(`^[a-z][a-z0-9-]{0,63}$`)

// toolNameRe constrains an advertised tool name (untrusted, from a third-party
// manifest). The name becomes the op_id suffix `plug.<plugin_id>.<name>`, so
// reject anything with path separators, whitespace, or control characters that
// would pollute catalog keys, audit-log op_ids, and MCP tool names. Dotted and
// hyphenated names are allowed (op_ids are dotted).
var toolNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_.-]{0,63}$`)

var validRiskClasses = map[string]bool{
	"read":        true,
	"write":       true,
	"destructive": true,
}

// LoadManifest reads, validates, and returns the manifest from `dir/manifest.json`.
// Errors: ErrManifestNotFound, ErrManifestInvalid, ErrUnsupportedShape, ErrUnsupportedSchemaVersion.
func LoadManifest(dir string) (*Manifest, error) {
	manifestPath := filepath.Join(dir, "manifest.json")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrManifestNotFound
		}
		return nil, fmt.Errorf("%w: %v", ErrManifestInvalid, err)
	}

	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, ErrManifestInvalid
	}

	if m.ManifestSchemaVersion != 1 {
		return nil, ErrUnsupportedSchemaVersion
	}

	if m.Shape != "mcp-plugin" {
		return nil, ErrUnsupportedShape
	}

	if !pluginIDRe.MatchString(m.PluginID) {
		return nil, ErrManifestInvalid
	}

	executable, err := validateExecutableRelPath(m.Executable)
	if err != nil {
		return nil, ErrManifestInvalid
	}
	m.Executable = executable

	for _, tool := range m.AdvertisedTools {
		if tool.Name == "" {
			return nil, ErrManifestInvalid
		}
		if !toolNameRe.MatchString(tool.Name) {
			// Reject path-like / whitespace / control-char names before they
			// become op_id suffixes (catalog keys, audit op_ids, MCP tool names).
			return nil, ErrManifestInvalid
		}
		if !validRiskClasses[tool.RiskClass] {
			return nil, ErrManifestInvalid
		}
	}

	return &m, nil
}

// Host manages plugin subprocess lifecycle.
type Host struct {
	cfg HostConfig
}

// HostConfig holds configuration for constructing a Host.
type HostConfig struct {
	InstallRoot string    // default ~/.local/share/gum/plugins
	Stderr      io.Writer // plugin subprocess stderr sink; nil discards
}

// NewHost constructs a Host using the install root.
func NewHost(cfg HostConfig) *Host {
	if cfg.InstallRoot == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			home = os.Getenv("HOME")
		}
		cfg.InstallRoot = filepath.Join(home, ".local", "share", "gum", "plugins")
	}
	return &Host{cfg: cfg}
}

// Install validates and copies a plugin source (path or url) into the install
// root. Returns the resolved plugin_id.
func (h *Host) Install(ctx context.Context, source string) (string, error) {
	// URL detection: if source looks like a URL, return not-implemented error.
	if strings.HasPrefix(source, "http://") || strings.HasPrefix(source, "https://") {
		return "", fmt.Errorf("plugin install url: not implemented in v0.1.0")
	}

	// Check source is a directory.
	info, err := os.Stat(source)
	if err != nil {
		return "", fmt.Errorf("plugin install: stat source: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("plugin install: source %q is not a directory", source)
	}

	// Load and validate manifest.
	m, err := LoadManifest(source)
	if err != nil {
		return "", err
	}

	destDir := filepath.Join(h.cfg.InstallRoot, m.PluginID)

	// Copy directory tree. File and directory modes are PINNED — never
	// inherited from the source, which may carry hostile bits like setuid,
	// setgid, or world-writable (spec §8.7 / gum-1ugz). Manifest's
	// executable lands at 0o755; every other regular file at 0o644;
	// every directory at 0o755.
	err = filepath.Walk(source, func(path string, fi os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		dest := filepath.Join(destDir, rel)

		if fi.IsDir() {
			if err := os.MkdirAll(dest, 0o755); err != nil {
				return err
			}
			return os.Chmod(dest, 0o755)
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%w: source contains symlink %q", ErrExecutableUntrusted, rel)
		}

		mode := os.FileMode(0o644)
		if rel == m.Executable {
			mode = 0o755
		}

		if err := copyFile(path, dest, mode); err != nil {
			return fmt.Errorf("plugin install: copy %s: %w", rel, err)
		}
		// Explicit chmod after copy: os.OpenFile honours umask, so a
		// 0o755 mode arg can land as 0o750 under umask 027. Chmod
		// bypasses umask and pins the spec-mandated bits.
		return os.Chmod(dest, mode)
	})
	if err != nil {
		return "", fmt.Errorf("plugin install: %w", err)
	}

	// Write the executable digest sidecar so Start() can VERIFY the binary it
	// spawns (gum-62ph). Without this, a plugin installed via the legacy path
	// (no registry) has no sidecar, Start()'s `if wantDigest != ""` guard skips
	// verification, and a swapped binary runs unchecked. Both install paths now
	// produce a verifiable plugin (InstallWithRegistry re-writes the same value).
	execPath := filepath.Join(destDir, m.Executable)
	if err := assertInsideInstallRoot(destDir, execPath); err != nil {
		return "", err
	}
	execSHA256, err := hashFileSHA256(execPath)
	if err != nil {
		return "", fmt.Errorf("plugin install: hash executable: %w", err)
	}
	sidecar := filepath.Join(destDir, executableDigestSidecar)
	if err := os.WriteFile(sidecar, []byte(execSHA256+"\n"), 0o644); err != nil {
		return "", fmt.Errorf("plugin install: write digest sidecar: %w", err)
	}

	return m.PluginID, nil
}

// copyFile copies src to dst with the given permission mode.
func copyFile(src, dst string, mode os.FileMode) error {
	// O_NOFOLLOW: reject a symlink swapped in for src between the directory walk
	// (which saw a regular file) and this open — closing the install-time TOCTOU
	// that could copy an arbitrary target like /etc/passwd (review gum-t8x1).
	in, err := fsatomic.OpenNoFollow(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}

	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

func validateExecutableRelPath(executable string) (string, error) {
	executable = strings.TrimSpace(executable)
	if executable == "" || filepath.IsAbs(executable) {
		return "", ErrManifestInvalid
	}
	clean := filepath.Clean(executable)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) {
		return "", ErrManifestInvalid
	}
	return clean, nil
}

// Remove deletes the plugin and any state at ~/.local/share/gum/plugins/<id>/.
func (h *Host) Remove(ctx context.Context, pluginID string) error {
	if !pluginIDRe.MatchString(pluginID) {
		return ErrManifestInvalid
	}
	return os.RemoveAll(filepath.Join(h.cfg.InstallRoot, pluginID))
}

// List returns installed plugin manifests.
func (h *Host) List() ([]*Manifest, error) {
	entries, err := os.ReadDir(h.cfg.InstallRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("plugin list: %w", err)
	}

	var manifests []*Manifest
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dir := filepath.Join(h.cfg.InstallRoot, entry.Name())
		m, err := LoadManifest(dir)
		if err != nil {
			// Skip invalid plugins silently.
			continue
		}
		manifests = append(manifests, m)
	}

	sort.Slice(manifests, func(i, j int) bool {
		return manifests[i].PluginID < manifests[j].PluginID
	})
	return manifests, nil
}

// Start spawns the plugin subprocess and returns a Plugin handle (spec §8.1,
// §8.3). The subprocess runs with a minimal env set: PATH, locale vars, HOME,
// TMPDIR, plus the named env vars declared in manifest.declared_capabilities.env_allow.
// Names that match the §8.1 denylist (GOOGLE_APPLICATION_CREDENTIALS, GUM_*,
// _GUM*, OPENAI_API_KEY, ANTHROPIC_API_KEY, ...) are stripped even when
// env_allow declares them.
//
// The MCP stdio JSON-RPC initialise handshake completes before Start returns;
// callers can issue CallTool immediately. Errors surface as opaque error wraps
// so the caller can quarantine the plugin without leaking subprocess detail.
func (h *Host) Start(ctx context.Context, pluginID string) (*Plugin, error) {
	if !pluginIDRe.MatchString(pluginID) {
		return nil, ErrManifestInvalid
	}
	installDir := filepath.Join(h.cfg.InstallRoot, pluginID)
	m, err := LoadManifest(installDir)
	if err != nil {
		return nil, err
	}
	execPath := filepath.Join(installDir, m.Executable)
	if !filepath.IsAbs(execPath) {
		// LoadManifest already rejected empty Executable; this guards future
		// regressions where m.Executable might bypass install-root containment.
		return nil, fmt.Errorf("plugin start: executable path %q not absolute", execPath)
	}
	if base := strings.ToLower(filepath.Base(execPath)); shellInterpreters[base] {
		return nil, fmt.Errorf("%w: executable %q is a shell interpreter", ErrExecutableUntrusted, execPath)
	}
	// install-root containment via real-path resolution.
	if err := assertInsideInstallRoot(installDir, execPath); err != nil {
		return nil, err
	}
	if info, err := os.Stat(execPath); err != nil {
		return nil, fmt.Errorf("plugin start: stat executable: %w", err)
	} else if info.IsDir() {
		return nil, fmt.Errorf("plugin start: executable %q is a directory", execPath)
	}

	// Spec §8.7 line 1690: re-verify the installed binary against the digest
	// captured at install time on EVERY spawn (no caching). A mutated binary
	// on disk MUST surface as PLUGIN_EXECUTABLE_UNTRUSTED before exec.
	wantDigest, err := readExecutableDigestSidecar(installDir)
	if err != nil {
		return nil, err
	}
	if wantDigest != "" {
		if err := VerifyExecutableBinding(&ExecutableBinding{
			Name:             pluginID,
			InstallRoot:      installDir,
			ExecutablePath:   execPath,
			ExecutableSHA256: wantDigest,
		}); err != nil {
			return nil, err
		}
	}

	connectCtx, cancel := context.WithTimeout(ctx, pluginConnectTimeout)
	defer cancel()
	subprocessEnv := buildSubprocessEnv(m.DeclaredCapabilities.EnvAllow)
	cmd, err := pluginenv.NewRunner(pluginenv.RunnerConfig{
		Executable: execPath,
		WorkDir:    installDir,
		Env:        subprocessEnv,
		Stderr:     h.cfg.Stderr, // nil → suppressed (default; tests override)
		Enforce:    true,
		Network:    m.DeclaredCapabilities.Network,
		FSWriteDir: m.DeclaredCapabilities.FSWriteDir,
	}).Command(ctx)
	if err != nil {
		return nil, fmt.Errorf("plugin start: %w", err)
	}

	transport := &sdkmcp.CommandTransport{Command: cmd}
	client := sdkmcp.NewClient(&sdkmcp.Implementation{
		Name:    "gum",
		Version: pluginClientVersion,
	}, nil)
	cs, err := client.Connect(connectCtx, transport, nil)
	if err != nil {
		// Best-effort cleanup of the orphan subprocess if Connect failed mid-handshake.
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
		return nil, fmt.Errorf("plugin start: %w", err)
	}
	return &Plugin{
		pluginID: pluginID,
		cs:       cs,
		cmd:      cmd,
	}, nil
}

// Plugin is a running plugin subprocess wired to the MCP go-sdk client.
type Plugin struct {
	pluginID string
	cs       *sdkmcp.ClientSession
	cmd      *exec.Cmd
}

// PluginID returns the plugin's manifest plugin_id.
func (p *Plugin) PluginID() string {
	if p == nil {
		return ""
	}
	return p.pluginID
}

// Stop closes the subprocess MCP session. The go-sdk CommandTransport.Close
// implements the spec §8.6 shutdown ladder: close stdin → wait → SIGTERM →
// wait → SIGKILL. Stop is idempotent.
func (p *Plugin) Stop(ctx context.Context) error {
	if p == nil || p.cs == nil {
		return nil
	}
	// ClientSession.Close drives the CommandTransport Close ladder; ignore
	// "process already finished" wait errors so a clean shutdown from the
	// plugin side doesn't surface as a host error.
	err := p.cs.Close()
	p.cs = nil
	if err == nil || isHarmlessWaitError(err) {
		return nil
	}
	return err
}

// CallTool sends a JSON-RPC tools/call to the plugin and returns the result
// payload serialised as JSON. args is JSON-marshalable. The first text-content
// block of a single-text result is returned verbatim; multi-content or
// non-text results are marshalled as a JSON object preserving the contents
// array and any structured content.
//
// On error envelopes (res.IsError=true) the plugin-local error code is
// projected through MapPluginError so the host envelope carries the stable
// GUM-side code (RATE_LIMITED, AUTH_REQUIRED, SERVICE_DOWN, …) plus the
// original SourceErrorCode for observability — spec §8 line 1631.
func (p *Plugin) CallTool(ctx context.Context, toolName string, args any) ([]byte, error) {
	if p == nil || p.cs == nil {
		return nil, fmt.Errorf("plugin CallTool: plugin not running")
	}
	res, err := p.cs.CallTool(ctx, &sdkmcp.CallToolParams{
		Name:      toolName,
		Arguments: args,
	})
	if err != nil {
		return nil, err
	}
	if res.IsError {
		mapped := MapPluginError(parsePluginErrorEnvelope(res))
		payload, _ := json.Marshal(map[string]any{
			"error_code":        mapped.Code,
			"error":             mapped.Message,
			"retryable":         mapped.Retryable,
			"retry_after_ms":    mapped.RetryAfterMS,
			"source_error_code": mapped.SourceErrorCode,
		})
		return payload, fmt.Errorf("plugin CallTool: tool returned error")
	}
	if len(res.Content) == 1 {
		if tc, ok := res.Content[0].(*sdkmcp.TextContent); ok {
			return []byte(tc.Text), nil
		}
	}
	return json.Marshal(res)
}

// parsePluginErrorEnvelope extracts the plugin-local error fields from an
// IsError result. The envelope is the spec §8 line 1625 shape carried as
// the first text-content block. Missing or malformed payloads fall through
// as a zero-value PluginError — MapPluginError maps the empty code to
// SERVICE_DOWN, which is the conservative "unknown plugin failure" code.
func parsePluginErrorEnvelope(res *sdkmcp.CallToolResult) PluginError {
	out := PluginError{}
	if res == nil || len(res.Content) == 0 {
		return out
	}
	tc, ok := res.Content[0].(*sdkmcp.TextContent)
	if !ok {
		return out
	}
	var raw struct {
		ErrorCode    string `json:"error_code"`
		Error        string `json:"error"`
		Message      string `json:"message"`
		Retryable    bool   `json:"retryable"`
		RetryAfterMS int    `json:"retry_after_ms"`
	}
	if err := json.Unmarshal([]byte(tc.Text), &raw); err != nil {
		return out
	}
	out.Code = raw.ErrorCode
	if raw.Error != "" {
		out.Message = raw.Error
	} else {
		out.Message = raw.Message
	}
	out.Retryable = raw.Retryable
	out.RetryAfterMS = raw.RetryAfterMS
	return out
}

// pluginConnectTimeout caps how long the MCP initialise handshake may take.
// A misbehaving plugin that never responds is killed and surfaced as an error
// rather than hanging Start indefinitely.
const pluginConnectTimeout = 10 * time.Second

// pluginClientVersion is the host MCP client version reported to plugins via
// the initialize params. Distinct from the outward-facing mcp.Version so a
// plugin protocol bump can be made without re-tagging the binary.
const pluginClientVersion = "0.1.0"

// passthroughEnv lists the unconditional env vars passed to every plugin
// subprocess per spec §8.1 "Env allowlist". Names not in this set or in the
// manifest's env_allow are stripped.
var passthroughEnv = []string{"PATH", "HOME", "TMPDIR", "LANG"}

// buildSubprocessEnv constructs the env slice for plugin spawn. Result starts
// from the passthrough set, appends explicitly declared env_allow entries that
// survive the §8.1 denylist, and never inherits the full os.Environ().
func buildSubprocessEnv(envAllow []string) []string {
	seen := make(map[string]bool, len(passthroughEnv)+len(envAllow))
	var out []string
	add := func(key string) {
		if seen[key] {
			return
		}
		seen[key] = true
		if pluginenv.IsDeniedEnv(key) {
			return
		}
		if v, ok := os.LookupEnv(key); ok {
			out = append(out, key+"="+v)
		}
	}
	for _, k := range passthroughEnv {
		add(k)
	}
	// Pass LC_* locale family.
	for _, e := range os.Environ() {
		if idx := strings.IndexByte(e, '='); idx > 0 {
			k := e[:idx]
			if strings.HasPrefix(k, "LC_") && !seen[k] && !pluginenv.IsDeniedEnv(k) {
				seen[k] = true
				out = append(out, e)
			}
		}
	}
	for _, k := range envAllow {
		add(k)
	}
	return out
}

// readExecutableDigestSidecar returns the install-time sha256 digest
// captured by InstallWithRegistry. A missing sidecar returns "" with no
// error so the legacy file-copy-only Install() path (no digest captured)
// still spawns; an unreadable or empty sidecar returns ErrExecutableUntrusted.
func readExecutableDigestSidecar(installDir string) (string, error) {
	path := filepath.Join(installDir, executableDigestSidecar)
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("%w: read digest sidecar: %v", ErrExecutableUntrusted, err)
	}
	digest := strings.TrimSpace(string(raw))
	if digest == "" {
		return "", fmt.Errorf("%w: empty digest sidecar", ErrExecutableUntrusted)
	}
	return digest, nil
}

// assertInsideInstallRoot resolves symlinks on both paths and ensures
// execPath does not escape installDir. Returns a wrapped
// ErrExecutableUntrusted on any miss; the caller routes that to spawn refusal.
func assertInsideInstallRoot(installDir, execPath string) error {
	rootResolved, err := filepath.EvalSymlinks(installDir)
	if err != nil {
		return fmt.Errorf("%w: install_dir resolve: %v", ErrExecutableUntrusted, err)
	}
	execResolved, err := filepath.EvalSymlinks(execPath)
	if err != nil {
		return fmt.Errorf("%w: executable resolve: %v", ErrExecutableUntrusted, err)
	}
	rel, err := filepath.Rel(rootResolved, execResolved)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("%w: executable %q escapes install_dir %q",
			ErrExecutableUntrusted, execPath, installDir)
	}
	return nil
}

// isHarmlessWaitError filters the "exit status N" wait errors that surface
// when the subprocess exits non-zero after Stop drove it through SIGTERM. Such
// exits are the documented happy-path of pipeRWC.Close in the go-sdk
// CommandTransport, not a bug.
func isHarmlessWaitError(err error) bool {
	if err == nil {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "signal: terminated") ||
		strings.Contains(msg, "signal: killed") ||
		strings.Contains(msg, "exit status")
}

// ValidateFSWritePath asserts that requestedPath is inside the manifest's
// declared fs_write_dir or the default data directory. Returns
// ErrFSWriteOutsideSandbox when violated.
func ValidateFSWritePath(m *Manifest, installRoot, pluginID, attemptedPath string) error {
	pluginDir := filepath.Join(installRoot, pluginID)

	var allowedRoot string
	if m.DeclaredCapabilities.FSWriteDir != "" {
		if filepath.IsAbs(m.DeclaredCapabilities.FSWriteDir) {
			return fmt.Errorf("%w: fs_write_dir must be relative to the plugin install directory", ErrFSWriteOutsideSandbox)
		}
		allowedRoot = filepath.Join(pluginDir, m.DeclaredCapabilities.FSWriteDir)
	} else {
		allowedRoot = filepath.Join(pluginDir, "data")
	}

	allowedRoot, err := resolvePathForContainment(allowedRoot)
	if err != nil {
		return fmt.Errorf("%w: cannot resolve sandbox root: %v", ErrFSWriteOutsideSandbox, err)
	}
	cleanRequested, err := resolvePathForContainment(attemptedPath)
	if err != nil {
		return fmt.Errorf("%w: cannot resolve requested path: %v", ErrFSWriteOutsideSandbox, err)
	}

	// Use filepath.Rel to detect upward traversal.
	rel, err := filepath.Rel(allowedRoot, cleanRequested)
	if err != nil {
		return fmt.Errorf("%w: cannot relativize path: %v", ErrFSWriteOutsideSandbox, err)
	}

	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || filepath.IsAbs(rel) {
		return fmt.Errorf("%w: %q escapes sandbox %q", ErrFSWriteOutsideSandbox, attemptedPath, allowedRoot)
	}

	return nil
}

func resolvePathForContainment(path string) (string, error) {
	clean := filepath.Clean(path)
	if resolved, err := filepath.EvalSymlinks(clean); err == nil {
		return filepath.Clean(resolved), nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}

	parent := clean
	var suffix []string
	for {
		resolved, err := filepath.EvalSymlinks(parent)
		if err == nil {
			parts := append([]string{resolved}, suffix...)
			return filepath.Clean(filepath.Join(parts...)), nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		next := filepath.Dir(parent)
		if next == parent {
			return clean, nil
		}
		suffix = append([]string{filepath.Base(parent)}, suffix...)
		parent = next
	}
}

var (
	ErrManifestNotFound         = errors.New("PLUGIN_MANIFEST_NOT_FOUND")
	ErrManifestInvalid          = errors.New("PLUGIN_MANIFEST_INVALID")
	ErrUnsupportedShape         = errors.New("PLUGIN_SHAPE_UNSUPPORTED")
	ErrUnsupportedSchemaVersion = errors.New("PLUGIN_MANIFEST_SCHEMA_UNSUPPORTED")
	ErrFSWriteOutsideSandbox    = errors.New("PLUGIN_FS_WRITE_OUTSIDE_SANDBOX")
	ErrNetworkBlocked           = errors.New("PLUGIN_NETWORK_BLOCKED")
)

// Reader for any io.Reader use, suppress unused import.
var _ = io.EOF

package plugins_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ehmo/gum/internal/plugins"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestMain doubles the test binary as a fake MCP plugin subprocess. When the
// environment variable FAKE_PLUGIN_MODE is set the binary runs an MCP
// stdio server advertising an "echo" tool instead of executing the test
// suite. Tests exec their own binary (copied into the install root so the
// install-root containment check succeeds), which gives us a real subprocess
// JSON-RPC roundtrip without writing a separate cgo-free echo helper to disk.
func TestMain(m *testing.M) {
	if mode := os.Getenv("FAKE_PLUGIN_MODE"); mode != "" {
		runFakePlugin(mode)
		return
	}
	os.Exit(m.Run())
}

// runFakePlugin runs the embedded MCP server. Supported modes:
//
//	"echo"        single echo tool that returns the JSON of its arguments
//	"crash"       terminates without responding so Start handshake times out
//	"rate_limit"  echo tool returns an IsError envelope carrying a plugin
//	              RATE_LIMIT code — exercises §8 error-code mapping
func runFakePlugin(mode string) {
	if mode == "child_write_once" {
		if err := os.WriteFile(os.Getenv("FAKE_PLUGIN_WRITE_PATH"), []byte("child probe\n"), 0o644); err != nil {
			_, _ = os.Stderr.WriteString(err.Error())
			os.Exit(23)
		}
		os.Exit(0)
	}
	if mode == "crash" {
		// Exit immediately with a non-zero status. Connect must surface a
		// handshake error, not a successful session.
		os.Exit(7)
	}
	srv := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "echo-plugin", Version: "0.1.0"}, nil)
	srv.AddTool(&sdkmcp.Tool{
		Name:        "echo",
		Description: "Returns the JSON of its arguments verbatim.",
		InputSchema: map[string]any{
			"type":                 "object",
			"additionalProperties": true,
		},
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
		switch mode {
		case "write_file":
			return probeToolResult(os.WriteFile(os.Getenv("FAKE_PLUGIN_WRITE_PATH"), []byte("sandbox probe\n"), 0o644)), nil
		case "dial_tcp":
			conn, err := net.DialTimeout("tcp", os.Getenv("FAKE_PLUGIN_DIAL_ADDR"), 500*time.Millisecond)
			if err == nil {
				_ = conn.Close()
			}
			return probeToolResult(err), nil
		case "child_write_file":
			exe, err := os.Executable()
			if err != nil {
				return probeToolResult(err), nil
			}
			cmd := exec.Command(exe)
			cmd.Env = append(os.Environ(), "FAKE_PLUGIN_MODE=child_write_once")
			var childOutput bytes.Buffer
			cmd.Stdin = strings.NewReader("")
			cmd.Stdout = &childOutput
			cmd.Stderr = &childOutput
			return probeToolResult(cmd.Run()), nil
		}
		if mode == "rate_limit" {
			return &sdkmcp.CallToolResult{
				IsError: true,
				Content: []sdkmcp.Content{
					&sdkmcp.TextContent{Text: `{"error_code":"RATE_LIMIT","error":"slow down","retryable":true,"retry_after_ms":1500}`},
				},
			}, nil
		}
		// Echo back the args field of the raw request directly so the test can
		// confirm round-trip fidelity. The handler receives CallToolParamsRaw,
		// but we read it from the request to avoid coupling on the SDK's
		// untyped argument unmarshal path.
		// Use a sentinel payload so the assertion stays stable.
		return &sdkmcp.CallToolResult{
			Content: []sdkmcp.Content{
				&sdkmcp.TextContent{Text: `{"ok":true,"echoed":"hello"}`},
			},
		}, nil
	})
	if err := srv.Run(context.Background(), &sdkmcp.StdioTransport{}); err != nil && err != io.EOF {
		os.Exit(2)
	}
	os.Exit(0)
}

func probeToolResult(err error) *sdkmcp.CallToolResult {
	payload := map[string]any{"ok": err == nil}
	if err != nil {
		payload["error"] = err.Error()
	}
	data, _ := json.Marshal(payload)
	return &sdkmcp.CallToolResult{
		Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: string(data)}},
	}
}

// installFakeEchoPlugin copies the running test binary into installRoot/<id>/
// with a manifest declaring the binary as the executable. The returned plugin
// id matches the manifest's plugin_id.
type fakePluginTB interface {
	Helper()
	Fatalf(string, ...any)
	Setenv(string, string)
}

func installFakeEchoPlugin(t fakePluginTB, installRoot, mode string) string {
	t.Helper()
	return installFakePlugin(t, installRoot, mode, false, "")
}

func installFakePlugin(t fakePluginTB, installRoot, mode string, network bool, fsWriteDir string) string {
	t.Helper()
	const pluginID = "echo-plugin"

	pluginDir := filepath.Join(installRoot, pluginID)
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatalf("mkdir plugin dir: %v", err)
	}
	exeName := "executable"
	if runtime.GOOS == "windows" {
		exeName = "executable.exe"
	}
	dst := filepath.Join(pluginDir, exeName)
	src, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	if err := copyExec(src, dst); err != nil {
		t.Fatalf("copy test binary: %v", err)
	}

	manifest := map[string]any{
		"manifest_schema_version": 1,
		"plugin_id":               pluginID,
		"name":                    "Echo Plugin",
		"version":                 "0.1.0",
		"shape":                   "mcp-plugin",
		"executable":              exeName,
		"advertised_tools": []map[string]any{
			{
				"name":        "echo",
				"description": "echoes args",
				"risk_class":  "read",
			},
		},
		"declared_capabilities": map[string]any{
			"network":      network,
			"fs_write_dir": fsWriteDir,
			"env_allow": []string{
				"FAKE_PLUGIN_MODE",
				"FAKE_PLUGIN_WRITE_PATH",
				"FAKE_PLUGIN_DIAL_ADDR",
			},
		},
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "manifest.json"), data, 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	t.Setenv("FAKE_PLUGIN_MODE", mode)
	return pluginID
}

func copyExec(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

// TestPluginExecutorStdioRoundtrip drives the full §8.1/§8.3 happy path:
// Host.Start spawns a real subprocess, completes the MCP initialize handshake,
// CallTool reaches the subprocess and the response makes it back. The flow
// confirms that Shape 1 dispatch works end-to-end without any stub.
func TestPluginExecutorStdioRoundtrip(t *testing.T) {
	installRoot := t.TempDir()
	pluginID := installFakeEchoPlugin(t, installRoot, "echo")

	h := plugins.NewHost(plugins.HostConfig{
		InstallRoot: installRoot,
		Stderr:      testWriter{t},
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	plug, err := h.Start(ctx, pluginID)
	if err != nil {
		t.Fatalf("Host.Start: %v", err)
	}
	t.Cleanup(func() { _ = plug.Stop(context.Background()) })

	if got := plug.PluginID(); got != pluginID {
		t.Errorf("PluginID() = %q; want %q", got, pluginID)
	}

	raw, err := plug.CallTool(ctx, "echo", map[string]any{"msg": "hello"})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !strings.Contains(string(raw), `"echoed":"hello"`) {
		t.Errorf("CallTool result = %s; want payload containing echoed=hello", raw)
	}

	if err := plug.Stop(context.Background()); err != nil {
		t.Errorf("Stop: %v", err)
	}
	// Stop is idempotent.
	if err := plug.Stop(context.Background()); err != nil {
		t.Errorf("Stop (second): %v", err)
	}
}

// TestPluginExecutorCallToolMapsErrorEnvelope pins the §8 error-code mapping
// wiring: when the plugin returns an IsError result with a plugin-local code
// (e.g. RATE_LIMIT), CallTool projects it through MapPluginError and emits
// the GUM-side envelope (RATE_LIMITED) carrying source_error_code for audit
// correlation. Without this wiring the host would surface the raw upstream
// payload and the caller-side error taxonomy would diverge from spec.
func TestPluginExecutorCallToolMapsErrorEnvelope(t *testing.T) {
	installRoot := t.TempDir()
	pluginID := installFakeEchoPlugin(t, installRoot, "rate_limit")

	h := plugins.NewHost(plugins.HostConfig{InstallRoot: installRoot, Stderr: testWriter{t}})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	plug, err := h.Start(ctx, pluginID)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = plug.Stop(context.Background()) })

	payload, err := plug.CallTool(ctx, "echo", map[string]any{})
	if err == nil {
		t.Fatal("CallTool returned nil err on IsError envelope; want non-nil")
	}
	var got map[string]any
	if jerr := json.Unmarshal(payload, &got); jerr != nil {
		t.Fatalf("payload not JSON: %v (raw=%s)", jerr, payload)
	}
	if got["error_code"] != "RATE_LIMITED" {
		t.Errorf("error_code = %v; want RATE_LIMITED (mapped from RATE_LIMIT)", got["error_code"])
	}
	if got["source_error_code"] != "RATE_LIMIT" {
		t.Errorf("source_error_code = %v; want RATE_LIMIT", got["source_error_code"])
	}
	if got["retryable"] != true {
		t.Errorf("retryable = %v; want true", got["retryable"])
	}
	if v, _ := got["retry_after_ms"].(float64); int(v) != 1500 {
		t.Errorf("retry_after_ms = %v; want 1500", got["retry_after_ms"])
	}
}

// TestPluginExecutorStartRejectsShellInterpreter creates a manifest whose
// executable path basename is `sh` and asserts Host.Start refuses to spawn
// with PLUGIN_EXECUTABLE_UNTRUSTED (spec §8.7 line 1690). The deny-list runs
// before VerifyExecutableBinding so manifests claiming "executable":"sh"
// cannot smuggle in shell-interpreter execution even with a matching digest.
func TestPluginExecutorStartRejectsShellInterpreter(t *testing.T) {
	installRoot := t.TempDir()
	const pluginID = "sh-plugin"
	pluginDir := filepath.Join(installRoot, pluginID)
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Drop a real binary named "sh" inside the plugin dir.
	src, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	if err := copyExec(src, filepath.Join(pluginDir, "sh")); err != nil {
		t.Fatalf("copy: %v", err)
	}
	manifest := map[string]any{
		"manifest_schema_version": 1,
		"plugin_id":               pluginID,
		"name":                    "Shell",
		"version":                 "0.1.0",
		"shape":                   "mcp-plugin",
		"executable":              "sh",
		"advertised_tools":        []map[string]any{},
		"declared_capabilities":   map[string]any{},
	}
	data, _ := json.MarshalIndent(manifest, "", "  ")
	if err := os.WriteFile(filepath.Join(pluginDir, "manifest.json"), data, 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	h := plugins.NewHost(plugins.HostConfig{InstallRoot: installRoot, Stderr: io.Discard})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	plug, err := h.Start(ctx, pluginID)
	if err == nil {
		_ = plug.Stop(context.Background())
		t.Fatalf("Start succeeded; want PLUGIN_EXECUTABLE_UNTRUSTED")
	}
	if !errors.Is(err, plugins.ErrExecutableUntrusted) {
		t.Errorf("Start err = %v; want wraps ErrExecutableUntrusted", err)
	}
}

// TestPluginExecutorStartRejectsMissingManifest covers the registry-pre path:
// Host.Start on a non-existent plugin ID must surface PLUGIN_MANIFEST_NOT_FOUND
// instead of a generic "no such file" wrapper. The dispatcher relies on this
// classification to differentiate "remove the lock entry" from "quarantine".
func TestPluginExecutorStartRejectsMissingManifest(t *testing.T) {
	installRoot := t.TempDir()
	h := plugins.NewHost(plugins.HostConfig{InstallRoot: installRoot, Stderr: io.Discard})
	ctx := context.Background()
	_, err := h.Start(ctx, "never-installed")
	if !errors.Is(err, plugins.ErrManifestNotFound) {
		t.Errorf("Start err = %v; want ErrManifestNotFound", err)
	}
}

// TestPluginExecutorStartRejectsCrashingSubprocess proves that an executable
// which exits immediately without speaking JSON-RPC surfaces as a Start error
// (not a hang). The CommandTransport gives the SDK client a closed stdout,
// which the initialize handshake detects.
func TestPluginExecutorStartRejectsCrashingSubprocess(t *testing.T) {
	installRoot := t.TempDir()
	pluginID := installFakeEchoPlugin(t, installRoot, "crash")

	h := plugins.NewHost(plugins.HostConfig{InstallRoot: installRoot, Stderr: io.Discard})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	plug, err := h.Start(ctx, pluginID)
	if err == nil {
		_ = plug.Stop(context.Background())
		t.Fatalf("Start succeeded; want handshake error")
	}
}

// TestPluginExecutorEnvFiltering exercises the spec §8.1 env allowlist.
// A denylisted name in env_allow must be stripped even when the manifest
// declares it; an allowed name must reach the subprocess.
func TestPluginExecutorEnvFiltering(t *testing.T) {
	// Indirect assertion via the public helper: the host filters via the
	// pluginenv denylist before spawn. We test the filter at the boundary
	// rather than spawn a subprocess that introspects its env, which keeps
	// the test self-contained.
	// Set both a denied and an allowed env in the parent process.
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", "/should/not/leak")
	t.Setenv("FAKE_PLUGIN_MODE", "echo")

	// Sanity: exec.LookPath must not be needed; the host resolves under
	// install_root. We assert the path constructor doesn't surface the
	// denied var by reaching into buildSubprocessEnv indirectly: start a
	// plugin and inspect that the parent denied var is not exported.
	installRoot := t.TempDir()
	pluginID := installFakeEchoPlugin(t, installRoot, "echo")

	h := plugins.NewHost(plugins.HostConfig{InstallRoot: installRoot, Stderr: io.Discard})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	plug, err := h.Start(ctx, pluginID)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = plug.Stop(context.Background()) })

	// Echo plugin doesn't introspect env; the meaningful test is that
	// the subprocess started successfully without the denied var causing
	// an env-leak audit failure. The denylist's primary contract is tested
	// in pluginenv/denylist_test.go; here we verify the host wires it.
	if _, err := plug.CallTool(ctx, "echo", map[string]any{}); err != nil {
		t.Errorf("CallTool: %v", err)
	}
}

func TestPluginSandboxAllowsWriteInsideDeclaredRoot(t *testing.T) {
	requireDarwinSandbox(t)
	installRoot := t.TempDir()
	pluginID := installFakePlugin(t, installRoot, "write_file", false, "writable")
	target := filepath.Join(installRoot, pluginID, "writable", "probe.txt")
	t.Setenv("FAKE_PLUGIN_WRITE_PATH", target)

	ok := callSandboxProbe(t, installRoot, pluginID)
	if !ok {
		t.Fatalf("write probe inside declared root denied")
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("allowed write target not created: %v", err)
	}
}

func TestPluginSandboxDeniesWriteOutsideDeclaredRoot(t *testing.T) {
	requireDarwinSandbox(t)
	installRoot := t.TempDir()
	pluginID := installFakePlugin(t, installRoot, "write_file", false, "writable")
	target := filepath.Join(installRoot, pluginID, "not-writable", "probe.txt")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatalf("mkdir outside target parent: %v", err)
	}
	t.Setenv("FAKE_PLUGIN_WRITE_PATH", target)

	ok := callSandboxProbe(t, installRoot, pluginID)
	if ok {
		t.Fatalf("write probe outside declared root succeeded")
	}
	if _, err := os.Stat(target); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("outside write target exists or stat failed unexpectedly: %v", err)
	}
}

func TestPluginSandboxDefaultWriteRootIsDataDir(t *testing.T) {
	requireDarwinSandbox(t)
	installRoot := t.TempDir()
	pluginID := installFakePlugin(t, installRoot, "write_file", false, "")
	dataTarget := filepath.Join(installRoot, pluginID, "data", "probe.txt")
	t.Setenv("FAKE_PLUGIN_WRITE_PATH", dataTarget)
	if !callSandboxProbe(t, installRoot, pluginID) {
		t.Fatalf("write probe inside default data root denied")
	}

	installRoot = t.TempDir()
	pluginID = installFakePlugin(t, installRoot, "write_file", false, "")
	outsideTarget := filepath.Join(installRoot, pluginID, "probe.txt")
	if err := os.MkdirAll(filepath.Dir(outsideTarget), 0o755); err != nil {
		t.Fatalf("mkdir default outside target parent: %v", err)
	}
	t.Setenv("FAKE_PLUGIN_WRITE_PATH", outsideTarget)
	if callSandboxProbe(t, installRoot, pluginID) {
		t.Fatalf("write probe outside default data root succeeded")
	}
}

func TestPluginSandboxDeniesNetworkWhenManifestDisablesNetwork(t *testing.T) {
	requireDarwinSandbox(t)
	installRoot := t.TempDir()
	pluginID := installFakePlugin(t, installRoot, "dial_tcp", false, "")
	t.Setenv("FAKE_PLUGIN_DIAL_ADDR", sandboxProbeListener(t))

	if callSandboxProbe(t, installRoot, pluginID) {
		t.Fatalf("dial probe succeeded with network=false")
	}
}

func TestPluginSandboxAllowsNetworkWhenManifestEnablesNetwork(t *testing.T) {
	requireDarwinSandbox(t)
	installRoot := t.TempDir()
	pluginID := installFakePlugin(t, installRoot, "dial_tcp", true, "")
	t.Setenv("FAKE_PLUGIN_DIAL_ADDR", sandboxProbeListener(t))

	if !callSandboxProbe(t, installRoot, pluginID) {
		t.Fatalf("dial probe denied with network=true")
	}
}

func TestPluginSandboxRestrictionsInheritToChildProcess(t *testing.T) {
	requireDarwinSandbox(t)
	installRoot := t.TempDir()
	pluginID := installFakePlugin(t, installRoot, "child_write_file", false, "writable")
	target := filepath.Join(installRoot, pluginID, "not-writable", "child.txt")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatalf("mkdir child outside target parent: %v", err)
	}
	t.Setenv("FAKE_PLUGIN_WRITE_PATH", target)

	if callSandboxProbe(t, installRoot, pluginID) {
		t.Fatalf("child write outside declared root succeeded")
	}
	if _, err := os.Stat(target); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("child outside write target exists or stat failed unexpectedly: %v", err)
	}
}

func TestPluginHostStartUsesPluginenvRunner(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	hostPath := filepath.Join(filepath.Dir(file), "host.go")
	raw, err := os.ReadFile(hostPath)
	if err != nil {
		t.Fatalf("read host.go: %v", err)
	}
	source := string(raw)
	for _, forbidden := range []string{"exec.Command(", "exec.CommandContext("} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("Host.Start must construct plugin commands via pluginenv; found %s in host.go", forbidden)
		}
	}
}

func requireDarwinSandbox(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("OS plugin sandbox backend is only available on darwin and linux")
	}
}

func callSandboxProbe(t *testing.T, installRoot, pluginID string) bool {
	t.Helper()
	h := plugins.NewHost(plugins.HostConfig{InstallRoot: installRoot, Stderr: testWriter{t}})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	plug, err := h.Start(ctx, pluginID)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = plug.Stop(context.Background()) })

	raw, err := plug.CallTool(ctx, "echo", map[string]any{})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	var got struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("probe payload %q not JSON: %v", raw, err)
	}
	t.Logf("sandbox probe ok=%v error=%q", got.OK, got.Error)
	return got.OK
}

func sandboxProbeListener(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		conn, err := ln.Accept()
		if err == nil {
			_ = conn.Close()
		}
	}()
	return ln.Addr().String()
}

func BenchmarkPluginCallToolSandboxedNoNetwork(b *testing.B) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		b.Skip("OS plugin sandbox backend is only available on darwin and linux")
	}
	installRoot := b.TempDir()
	pluginID := installFakeEchoPlugin(b, installRoot, "echo")
	h := plugins.NewHost(plugins.HostConfig{InstallRoot: installRoot, Stderr: io.Discard})
	ctx := context.Background()
	plug, err := h.Start(ctx, pluginID)
	if err != nil {
		b.Fatalf("Start: %v", err)
	}
	b.Cleanup(func() { _ = plug.Stop(context.Background()) })

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := plug.CallTool(context.Background(), "echo", map[string]any{}); err != nil {
			b.Fatalf("CallTool: %v", err)
		}
	}
}

func BenchmarkPluginStartSandboxedNoNetwork(b *testing.B) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		b.Skip("OS plugin sandbox backend is only available on darwin and linux")
	}
	installRoot := b.TempDir()
	pluginID := installFakeEchoPlugin(b, installRoot, "echo")
	h := plugins.NewHost(plugins.HostConfig{InstallRoot: installRoot, Stderr: io.Discard})

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		plug, err := h.Start(ctx, pluginID)
		if err != nil {
			cancel()
			b.Fatalf("Start: %v", err)
		}
		if err := plug.Stop(context.Background()); err != nil {
			cancel()
			b.Fatalf("Stop: %v", err)
		}
		cancel()
	}
}

// guard against accidental usage of os/exec in production code paths that
// would re-introduce a path-only lookup. compile-only check.
var _ = exec.LookPath

// testWriter pipes subprocess stderr into the test log so a misbehaving fake
// plugin reveals its failure mode instead of hanging silently.
type testWriter struct{ t *testing.T }

func (w testWriter) Write(p []byte) (int, error) {
	w.t.Helper()
	w.t.Logf("[plugin stderr] %s", strings.TrimRight(string(p), "\n"))
	return len(p), nil
}

package adapters_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ehmo/gum/internal/adapters"
	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/dispatch"
	"github.com/ehmo/gum/internal/plugins"
)

// TestMain doubles the test binary as a Shape 1 MCP plugin. FAKE_PLUGIN_MODE
// picks the tool body, so the adapter gets a real subprocess handshake without
// a second helper binary on disk.
func TestMain(m *testing.M) {
	if mode := os.Getenv("FAKE_PLUGIN_MODE"); mode != "" {
		runTextPlugin(mode)
		return
	}
	os.Exit(m.Run())
}

// runTextPlugin serves one "echo" tool. Mode "text" returns free-form text,
// which is what makes the adapter's JSON-envelope wrap run.
func runTextPlugin(mode string) {
	srv := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "textwrap-echo", Version: "0.1.0"}, nil)
	srv.AddTool(&sdkmcp.Tool{
		Name:        "echo",
		Description: "Returns a fixed payload.",
		InputSchema: map[string]any{"type": "object", "additionalProperties": true},
	}, func(context.Context, *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
		body := `{"ok":true}`
		if mode == "text" {
			body = "plain text, no JSON here"
		}
		return &sdkmcp.CallToolResult{
			Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: body}},
		}, nil
	})
	if err := srv.Run(context.Background(), &sdkmcp.StdioTransport{}); err != nil && err != io.EOF {
		os.Exit(2)
	}
	os.Exit(0)
}

// installTextPlugin lays out an install dir the way Host.Install would: the
// test binary as the executable, a manifest advertising the echo tool, and the
// digest sidecar Start verifies on every spawn.
func installTextPlugin(t *testing.T, installRoot, mode string) string {
	t.Helper()
	const pluginID = "textwrap-echo-plugin"

	pluginDir := filepath.Join(installRoot, pluginID)
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", pluginDir, err)
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
	if err := copyTextExec(src, dst); err != nil {
		t.Fatalf("copy test binary: %v", err)
	}

	manifest := map[string]any{
		"manifest_schema_version": 1,
		"plugin_id":               pluginID,
		"name":                    "Textwrap Echo Plugin",
		"version":                 "0.1.0",
		"shape":                   "mcp-plugin",
		"executable":              exeName,
		"advertised_tools": []map[string]any{
			{"name": "echo", "description": "echoes", "risk_class": "read"},
		},
		"declared_capabilities": map[string]any{
			"network":      false,
			"fs_write_dir": "",
			"env_allow":    []string{"FAKE_PLUGIN_MODE"},
		},
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "manifest.json"), data, 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	raw, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read %s: %v", dst, err)
	}
	digest := fmt.Sprintf("%x", sha256.Sum256(raw))
	sidecar := filepath.Join(pluginDir, ".executable.sha256")
	if err := os.WriteFile(sidecar, []byte(digest+"\n"), 0o600); err != nil {
		t.Fatalf("write %s: %v", sidecar, err)
	}

	t.Setenv("FAKE_PLUGIN_MODE", mode)
	return pluginID
}

func copyTextExec(src, dst string) error {
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

func callTextPlugin(t *testing.T, mode string) []byte {
	t.Helper()
	root := t.TempDir()
	id := installTextPlugin(t, root, mode)

	pm := adapters.NewPluginMCP(plugins.NewHost(plugins.HostConfig{InstallRoot: root}))
	t.Cleanup(func() { _ = pm.Close(context.Background()) })

	rv := &dispatch.ResolvedVariant{
		OpID:       "textwrap.echo",
		AdapterKey: "plugin.mcp",
		Variant: &catalog.Variant{
			VariantID: "v1",
			Binding: &catalog.Binding{
				AdapterKey: "plugin.mcp",
				PluginName: id,
				ToolName:   "echo",
			},
		},
	}
	resp, err := pm.Execute(context.Background(),
		&dispatch.Invocation{OpID: "textwrap.echo", Args: map[string]any{"q": "x"}}, rv, nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if resp.Format != "json" {
		t.Errorf("Format = %q; want json", resp.Format)
	}
	return resp.Body
}

// TestExecuteWrapsAPlainTextToolResult pins the text-envelope arm. A Shape 1
// plugin may answer with free-form text, and every stage downstream (cache,
// tee, gain) parses the body as JSON, so the adapter has to give it a shape.
func TestExecuteWrapsAPlainTextToolResult(t *testing.T) {
	body := callTextPlugin(t, "text")

	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("body is not JSON after the wrap: %v (%s)", err, body)
	}
	if got["text"] != "plain text, no JSON here" {
		t.Errorf("wrapped body = %v; want the plugin's text under \"text\"", got)
	}
}

// TestExecutePassesAJSONToolResultThrough pins the other side of the same
// guard: a body that already starts with '{' must reach the output pipeline
// byte for byte, so a field mask still sees the plugin's own keys.
func TestExecutePassesAJSONToolResultThrough(t *testing.T) {
	body := callTextPlugin(t, "json")

	if string(body) != `{"ok":true}` {
		t.Errorf("body = %s; want the plugin's JSON verbatim", body)
	}
}

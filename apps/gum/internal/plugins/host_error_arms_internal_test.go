package plugins

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ehmo/gum/internal/plugins/registry"
)

// TestHashFileSHA256CopyErrorSurfaces pins the io.Copy arm of
// hashFileSHA256 (binding.go:108-110). os.Open on a directory succeeds on
// unix, and the first Read then reports EISDIR, so a directory path is the
// hermetic way to fail the copy after a successful open.
func TestHashFileSHA256CopyErrorSurfaces(t *testing.T) {
	dir := t.TempDir()
	_, err := hashFileSHA256(dir)
	if err == nil {
		t.Fatal("hashFileSHA256(dir) err=nil; want read failure")
	}
	if !strings.Contains(err.Error(), "is a directory") {
		t.Errorf("err=%v; want EISDIR from the copy", err)
	}
}

// TestLoadManifestUnreadableManifestWraps pins the non-IsNotExist ReadFile
// arm (host.go:96). A directory planted at manifest.json reads as EISDIR,
// which must surface as ErrManifestInvalid rather than ErrManifestNotFound.
func TestLoadManifestUnreadableManifestWraps(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "manifest.json"), 0o755); err != nil {
		t.Fatalf("plant directory: %v", err)
	}
	_, err := LoadManifest(dir)
	if !errors.Is(err, ErrManifestInvalid) {
		t.Fatalf("LoadManifest err=%v; want ErrManifestInvalid", err)
	}
	if errors.Is(err, ErrManifestNotFound) {
		t.Error("EISDIR must not be reported as a missing manifest")
	}
}

// TestLoadManifestRejectsAdvertisedToolShape pins the two per-tool guards
// that the existing path-like-name test does not reach: an empty name
// (host.go:130-132) and a risk_class outside the allowed set
// (host.go:138-140).
func TestLoadManifestRejectsAdvertisedToolShape(t *testing.T) {
	cases := map[string]string{
		"empty_name":       `{"name":"","description":"x","risk_class":"read"}`,
		"bad_risk_class":   `{"name":"ping","description":"x","risk_class":"nuclear"}`,
		"empty_risk_class": `{"name":"ping","description":"x"}`,
	}
	for name, tool := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			manifest := `{
  "manifest_schema_version": 1,
  "plugin_id": "arms",
  "name": "arms",
  "version": "0.0.1",
  "shape": "mcp-plugin",
  "executable": "bin/plugin",
  "advertised_tools": [` + tool + `]
}`
			if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(manifest), 0o600); err != nil {
				t.Fatalf("write manifest: %v", err)
			}
			if _, err := LoadManifest(dir); !errors.Is(err, ErrManifestInvalid) {
				t.Fatalf("LoadManifest err=%v; want ErrManifestInvalid", err)
			}
		})
	}
}

// TestBuildSubprocessEnvSkipsAlreadySeenCred pins the `seen[k] → continue`
// arm of the needs_user_creds loop (host.go:650-653). PATH is emitted by the
// passthrough loop first, so a manifest that also declares PATH as a
// credential must not emit it twice and must not take the stored secret.
func TestBuildSubprocessEnvSkipsAlreadySeenCred(t *testing.T) {
	t.Setenv("PATH", "/usr/bin")
	out := buildSubprocessEnv(nil, []string{"PATH"}, map[string]string{"PATH": "/attacker/bin"})

	var count int
	for _, e := range out {
		if strings.HasPrefix(e, "PATH=") {
			count++
			if e != "PATH=/usr/bin" {
				t.Errorf("PATH entry=%q; want the passthrough value", e)
			}
		}
	}
	if count != 1 {
		t.Errorf("PATH emitted %d times; want 1", count)
	}
}

// TestPluginCredentialEnvSkipsIncompleteDescriptors pins the
// `d.Env == "" || d.Alias == ""` continue (host.go:678-680). A descriptor
// missing either half cannot key a keychain entry, so it must be skipped
// rather than produce an empty-named env var.
func TestPluginCredentialEnvSkipsIncompleteDescriptors(t *testing.T) {
	kr := newFakeKeyring()
	kr.store[PluginCredentialKey("prof", "p", "good")] = "s3cret"
	h := NewHost(HostConfig{InstallRoot: t.TempDir(), Profile: "prof", Keyring: kr})

	m := &Manifest{}
	m.Requirements.CredentialDescriptors = []CredentialDescriptor{
		{Alias: "no_env"},
		{Env: "NO_ALIAS"},
		{Alias: "good", Env: "PLUGIN_TOKEN"},
	}

	got := h.pluginCredentialEnv("p", m)
	if len(got) != 1 || got["PLUGIN_TOKEN"] != "s3cret" {
		t.Fatalf("pluginCredentialEnv=%v; want only PLUGIN_TOKEN", got)
	}
}

// TestTrustedDigestResolverArms pins the three resolver-aware branches of
// trustedDigest (host.go:699-708): a resolver error, an empty recorded
// digest falling back to the sidecar, and a sidecar error winning over a
// non-empty recorded digest.
func TestTrustedDigestResolverArms(t *testing.T) {
	const digest = "aa11"

	writeSidecar := func(t *testing.T) string {
		t.Helper()
		dir := t.TempDir()
		path := filepath.Join(dir, executableDigestSidecar)
		if err := os.WriteFile(path, []byte(digest+"\n"), 0o600); err != nil {
			t.Fatalf("write sidecar: %v", err)
		}
		return dir
	}

	t.Run("resolver_error_is_untrusted", func(t *testing.T) {
		boom := errors.New("registry unreadable")
		h := NewHost(HostConfig{
			InstallRoot:   t.TempDir(),
			TrustedDigest: func(string) (string, error) { return "", boom },
		})
		_, err := h.trustedDigest("p", writeSidecar(t))
		if !errors.Is(err, ErrExecutableUntrusted) {
			t.Fatalf("err=%v; want ErrExecutableUntrusted", err)
		}
		if !strings.Contains(err.Error(), "registry unreadable") {
			t.Errorf("err=%v; want the resolver cause named", err)
		}
	})

	t.Run("empty_recorded_falls_back_to_sidecar", func(t *testing.T) {
		h := NewHost(HostConfig{
			InstallRoot:   t.TempDir(),
			TrustedDigest: func(string) (string, error) { return "", nil },
		})
		got, err := h.trustedDigest("p", writeSidecar(t))
		if err != nil {
			t.Fatalf("trustedDigest: %v", err)
		}
		if got != digest {
			t.Errorf("digest=%q; want %q", got, digest)
		}
	})

	t.Run("sidecar_error_beats_recorded_digest", func(t *testing.T) {
		h := NewHost(HostConfig{
			InstallRoot:   t.TempDir(),
			TrustedDigest: func(string) (string, error) { return digest, nil },
		})
		// No sidecar written: the read fails, and a recorded digest must not
		// paper over it.
		_, err := h.trustedDigest("p", t.TempDir())
		if !errors.Is(err, ErrExecutableUntrusted) {
			t.Fatalf("err=%v; want ErrExecutableUntrusted", err)
		}
	})
}

// TestValidateFSWritePathResolveFailures pins the three error arms that sit
// between the sandbox root and the Rel check (host.go:786-798), plus the
// non-ErrNotExist arm inside resolvePathForContainment (host.go:811-813).
// A regular file standing in for a directory yields ENOTDIR, which is not
// ErrNotExist, so EvalSymlinks fails outright.
func TestValidateFSWritePathResolveFailures(t *testing.T) {
	m := &Manifest{}

	t.Run("sandbox_root_unresolvable", func(t *testing.T) {
		installRoot := t.TempDir()
		// The plugin directory is a regular file, so <root>/p/data cannot
		// resolve.
		if err := os.WriteFile(filepath.Join(installRoot, "p"), []byte("x"), 0o600); err != nil {
			t.Fatalf("plant file: %v", err)
		}
		err := ValidateFSWritePath(m, installRoot, "p", filepath.Join(installRoot, "p", "data", "x"))
		if !errors.Is(err, ErrFSWriteOutsideSandbox) {
			t.Fatalf("err=%v; want ErrFSWriteOutsideSandbox", err)
		}
		if !strings.Contains(err.Error(), "cannot resolve sandbox root") {
			t.Errorf("err=%v; want the sandbox-root arm", err)
		}
	})

	t.Run("requested_path_unresolvable", func(t *testing.T) {
		installRoot := t.TempDir()
		blocker := filepath.Join(t.TempDir(), "file")
		if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
			t.Fatalf("plant file: %v", err)
		}
		err := ValidateFSWritePath(m, installRoot, "p", filepath.Join(blocker, "under"))
		if !errors.Is(err, ErrFSWriteOutsideSandbox) {
			t.Fatalf("err=%v; want ErrFSWriteOutsideSandbox", err)
		}
		if !strings.Contains(err.Error(), "cannot resolve requested path") {
			t.Errorf("err=%v; want the requested-path arm", err)
		}
	})

	t.Run("relative_request_cannot_relativize", func(t *testing.T) {
		// A relative attempted path resolves to a relative result, and
		// filepath.Rel refuses to relate it to the absolute sandbox root.
		err := ValidateFSWritePath(m, t.TempDir(), "p", filepath.Join("relative", "target"))
		if !errors.Is(err, ErrFSWriteOutsideSandbox) {
			t.Fatalf("err=%v; want ErrFSWriteOutsideSandbox", err)
		}
		if !strings.Contains(err.Error(), "cannot relativize path") {
			t.Errorf("err=%v; want the relativize arm", err)
		}
	})
}

// TestRecordedDigestResolverErrorArms pins the resolver's load failure
// (host.go:860-863) and its non-map row skip (host.go:865-868). A row the
// build does not model must be stepped over, not treated as a digest.
func TestRecordedDigestResolverErrorArms(t *testing.T) {
	t.Run("load_failure_propagates", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(registry.CatalogPath(dir), []byte("{not json"), 0o600); err != nil {
			t.Fatalf("plant catalog: %v", err)
		}
		_, err := RecordedDigestResolver(registry.New(dir))("p")
		if err == nil {
			t.Fatal("resolver err=nil; want the registry load failure")
		}
	})

	t.Run("non_map_row_skipped", func(t *testing.T) {
		dir := t.TempDir()
		lock := `{"plugins_lock_schema_version":1,"plugins":["stringrow",{"name":"p","executable_sha256":"ff01"}]}`
		if err := os.WriteFile(registry.LockPath(dir), []byte(lock), 0o600); err != nil {
			t.Fatalf("plant lock: %v", err)
		}
		got, err := RecordedDigestResolver(registry.New(dir))("p")
		if err != nil {
			t.Fatalf("resolver: %v", err)
		}
		if got != "ff01" {
			t.Errorf("digest=%q; want ff01", got)
		}
	})
}

// TestCopyFileCopyErrorSurfaces pins copyFile's io.Copy arm (host.go:294).
// A directory opens read-only on unix and reports EISDIR on the first read,
// so the failure lands after the destination file is already created.
func TestCopyFileCopyErrorSurfaces(t *testing.T) {
	src := t.TempDir()
	dst := filepath.Join(t.TempDir(), "out.bin")

	err := copyFile(src, dst, 0o600)
	if err == nil {
		t.Fatal("copyFile(dir) err=nil; want read failure")
	}
	if !strings.Contains(err.Error(), "is a directory") {
		t.Errorf("err=%v; want EISDIR from the copy", err)
	}
}

// callToolPlugin returns a Plugin wired to an in-process MCP server that
// serves the two tools the CallTool arms need: "multi" answers with two
// content blocks so the single-text fast path does not apply, and "typed"
// answers with a non-text block.
func callToolPlugin(t *testing.T) (*Plugin, *sdkmcp.ServerSession) {
	t.Helper()
	ctx := context.Background()
	clientTransport, serverTransport := sdkmcp.NewInMemoryTransports()

	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "fixture", Version: "0.0.1"}, nil)
	schema := map[string]any{"type": "object", "additionalProperties": true}
	server.AddTool(&sdkmcp.Tool{Name: "multi", Description: "Two blocks", InputSchema: schema},
		func(_ context.Context, _ *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
			return &sdkmcp.CallToolResult{Content: []sdkmcp.Content{
				&sdkmcp.TextContent{Text: "first"},
				&sdkmcp.TextContent{Text: "second"},
			}}, nil
		})
	ss, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "gum", Version: pluginClientVersion}, nil)
	cs, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	return newPlugin("fixture", cs, nil), ss
}

// TestCallToolMarshalsMultiBlockResult pins the json.Marshal fall-through
// (host.go:552). A result carrying two content blocks cannot take the
// single-text fast path, so the whole CallToolResult is marshalled.
func TestCallToolMarshalsMultiBlockResult(t *testing.T) {
	plug, ss := callToolPlugin(t)
	defer func() { _ = ss.Close() }()

	raw, err := plug.CallTool(context.Background(), "multi", map[string]any{})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("payload is not JSON: %v (%s)", err, raw)
	}
	if _, ok := decoded["content"]; !ok {
		t.Errorf("payload=%s; want the marshalled CallToolResult with a content field", raw)
	}
}

// TestCallToolTransportErrorSurfaces pins the transport-error arm
// (host.go:534). A cancelled context fails the call inside the SDK, before
// any result envelope exists to inspect.
func TestCallToolTransportErrorSurfaces(t *testing.T) {
	plug, ss := callToolPlugin(t)
	defer func() { _ = ss.Close() }()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := plug.CallTool(ctx, "multi", map[string]any{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("CallTool err=%v; want context.Canceled", err)
	}
}

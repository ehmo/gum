package plugins_test

import (
	"context"
	"errors"
	"path/filepath"
	"runtime"
	"testing"

	"go.uber.org/goleak"

	"github.com/ehmo/gum/internal/plugins"
)

// testdataDir returns the absolute path to testdata/ relative to this file.
func testdataDir() string {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		panic("runtime.Caller failed")
	}
	return filepath.Join(filepath.Dir(thisFile), "testdata")
}

// catchPanic calls fn and returns ("panic: not implemented", true) if fn panics,
// else ("", false). Lets tests assert on bodyless stubs without killing the binary.
func catchPanic(fn func()) (msg string, panicked bool) {
	defer func() {
		if r := recover(); r != nil {
			msg = "panic: not implemented"
			panicked = true
		}
	}()
	fn()
	return "", false
}

// TestLoadManifestValid verifies that a well-formed mcp-plugin manifest is
// accepted and fields are populated correctly.
func TestLoadManifestValid(t *testing.T) {
	defer goleak.VerifyNone(t)

	dir := filepath.Join(testdataDir(), "valid-plugin")
	var m *plugins.Manifest
	var err error
	msg, panicked := catchPanic(func() {
		m, err = plugins.LoadManifest(dir)
	})
	if panicked {
		t.Fatalf("LoadManifest panicked: %s (green team must implement)", msg)
	}
	if err != nil {
		t.Fatalf("LoadManifest(%q) returned unexpected error: %v", dir, err)
	}
	if m == nil {
		t.Fatal("LoadManifest returned nil manifest without error")
	}
	if m.Shape != "mcp-plugin" {
		t.Errorf("manifest.Shape = %q, want %q", m.Shape, "mcp-plugin")
	}
	if m.PluginID != "test-plugin" {
		t.Errorf("manifest.PluginID = %q, want %q", m.PluginID, "test-plugin")
	}
	if m.ManifestSchemaVersion != 1 {
		t.Errorf("manifest.ManifestSchemaVersion = %d, want 1", m.ManifestSchemaVersion)
	}
}

// TestLoadManifestInvalidShape verifies that a manifest with shape="grpc-subprocess"
// is rejected with ErrUnsupportedShape before v0.4.0.
func TestLoadManifestInvalidShape(t *testing.T) {
	defer goleak.VerifyNone(t)

	dir := filepath.Join(testdataDir(), "invalid-shape")
	var err error
	msg, panicked := catchPanic(func() {
		_, err = plugins.LoadManifest(dir)
	})
	if panicked {
		t.Fatalf("LoadManifest panicked: %s (green team must implement)", msg)
	}
	if !errors.Is(err, plugins.ErrUnsupportedShape) {
		t.Errorf("LoadManifest returned %v, want ErrUnsupportedShape (%v)", err, plugins.ErrUnsupportedShape)
	}
}

// TestLoadManifestUnsupportedSchemaVersion verifies that manifest_schema_version=999
// is rejected with ErrUnsupportedSchemaVersion.
func TestLoadManifestUnsupportedSchemaVersion(t *testing.T) {
	defer goleak.VerifyNone(t)

	dir := filepath.Join(testdataDir(), "unsupported-schema")
	var err error
	msg, panicked := catchPanic(func() {
		_, err = plugins.LoadManifest(dir)
	})
	if panicked {
		t.Fatalf("LoadManifest panicked: %s (green team must implement)", msg)
	}
	if !errors.Is(err, plugins.ErrUnsupportedSchemaVersion) {
		t.Errorf("LoadManifest returned %v, want ErrUnsupportedSchemaVersion (%v)", err, plugins.ErrUnsupportedSchemaVersion)
	}
}

// TestLoadManifestNotFound verifies that a non-existent directory returns
// ErrManifestNotFound.
func TestLoadManifestNotFound(t *testing.T) {
	defer goleak.VerifyNone(t)

	dir := filepath.Join(testdataDir(), "does-not-exist-xyzzy")
	var err error
	msg, panicked := catchPanic(func() {
		_, err = plugins.LoadManifest(dir)
	})
	if panicked {
		t.Fatalf("LoadManifest panicked: %s (green team must implement)", msg)
	}
	if !errors.Is(err, plugins.ErrManifestNotFound) {
		t.Errorf("LoadManifest returned %v, want ErrManifestNotFound (%v)", err, plugins.ErrManifestNotFound)
	}
}

// TestHostInstallList installs a valid plugin source directory and verifies
// that List returns exactly one manifest with the expected plugin_id.
func TestHostInstallList(t *testing.T) {
	defer goleak.VerifyNone(t)

	installRoot := t.TempDir()
	var h *plugins.Host
	msg, panicked := catchPanic(func() {
		h = plugins.NewHost(plugins.HostConfig{InstallRoot: installRoot})
	})
	if panicked {
		t.Fatalf("NewHost panicked: %s (green team must implement)", msg)
	}
	if h == nil {
		t.Fatal("NewHost returned nil (green team must implement)")
	}

	src := filepath.Join(testdataDir(), "valid-plugin")
	ctx := t.Context()

	var pluginID string
	var installErr error
	{
		msg2, panicked2 := catchPanic(func() {
			pluginID, installErr = h.Install(ctx, src)
		})
		if panicked2 {
			t.Fatalf("Host.Install panicked: %s (green team must implement)", msg2)
		}
	}
	if installErr != nil {
		t.Fatalf("Host.Install returned unexpected error: %v", installErr)
	}
	if pluginID == "" {
		t.Error("Host.Install returned empty plugin_id")
	}

	var manifests []*plugins.Manifest
	var listErr error
	{
		msg2, panicked2 := catchPanic(func() {
			manifests, listErr = h.List()
		})
		if panicked2 {
			t.Fatalf("Host.List panicked: %s (green team must implement)", msg2)
		}
	}
	if listErr != nil {
		t.Fatalf("Host.List returned unexpected error: %v", listErr)
	}
	if len(manifests) != 1 {
		t.Fatalf("Host.List returned %d manifests, want 1", len(manifests))
	}
	if manifests[0].PluginID != pluginID {
		t.Errorf("manifest plugin_id = %q, want %q", manifests[0].PluginID, pluginID)
	}
}

// TestHostInstallRejectsBadManifest verifies that installing a plugin with an
// invalid shape fails before any state is written.
func TestHostInstallRejectsBadManifest(t *testing.T) {
	defer goleak.VerifyNone(t)

	installRoot := t.TempDir()
	var h *plugins.Host
	msg, panicked := catchPanic(func() {
		h = plugins.NewHost(plugins.HostConfig{InstallRoot: installRoot})
	})
	if panicked {
		t.Fatalf("NewHost panicked: %s (green team must implement)", msg)
	}
	if h == nil {
		t.Fatal("NewHost returned nil (green team must implement)")
	}

	src := filepath.Join(testdataDir(), "invalid-shape")
	ctx := t.Context()

	var installErr error
	{
		msg2, panicked2 := catchPanic(func() {
			_, installErr = h.Install(ctx, src)
		})
		if panicked2 {
			t.Fatalf("Host.Install panicked: %s (green team must implement)", msg2)
		}
	}
	if !errors.Is(installErr, plugins.ErrUnsupportedShape) {
		t.Errorf("Host.Install returned %v, want ErrUnsupportedShape (%v)", installErr, plugins.ErrUnsupportedShape)
	}

	// List must return empty — no partial state written.
	var manifests []*plugins.Manifest
	var listErr error
	{
		msg2, panicked2 := catchPanic(func() {
			manifests, listErr = h.List()
		})
		if panicked2 {
			t.Fatalf("Host.List panicked: %s (green team must implement)", msg2)
		}
	}
	if listErr != nil {
		t.Fatalf("Host.List returned unexpected error: %v", listErr)
	}
	if len(manifests) != 0 {
		t.Errorf("Host.List returned %d manifests after failed install, want 0", len(manifests))
	}
}

// TestHostRemovePlugin installs a plugin then removes it; List must return empty.
func TestHostRemovePlugin(t *testing.T) {
	defer goleak.VerifyNone(t)

	installRoot := t.TempDir()
	var h *plugins.Host
	msg, panicked := catchPanic(func() {
		h = plugins.NewHost(plugins.HostConfig{InstallRoot: installRoot})
	})
	if panicked {
		t.Fatalf("NewHost panicked: %s (green team must implement)", msg)
	}
	if h == nil {
		t.Fatal("NewHost returned nil (green team must implement)")
	}

	src := filepath.Join(testdataDir(), "valid-plugin")
	ctx := t.Context()

	var pluginID string
	{
		msg2, panicked2 := catchPanic(func() {
			var err error
			pluginID, err = h.Install(ctx, src)
			if err != nil {
				t.Errorf("Host.Install: %v", err)
			}
		})
		if panicked2 {
			t.Fatalf("Host.Install panicked: %s (green team must implement)", msg2)
		}
	}

	var removeErr error
	msg, panicked = catchPanic(func() {
		removeErr = h.Remove(ctx, pluginID)
	})
	if panicked {
		t.Fatalf("Host.Remove panicked: %s (green team must implement)", msg)
	}
	if removeErr != nil {
		t.Fatalf("Host.Remove returned unexpected error: %v", removeErr)
	}

	var manifests []*plugins.Manifest
	var listErr error
	msg, panicked = catchPanic(func() {
		manifests, listErr = h.List()
	})
	if panicked {
		t.Fatalf("Host.List panicked: %s (green team must implement)", msg)
	}
	if listErr != nil {
		t.Fatalf("Host.List returned unexpected error: %v", listErr)
	}
	if len(manifests) != 0 {
		t.Errorf("Host.List returned %d manifests after remove, want 0", len(manifests))
	}
}

// TestPluginSandboxFSWriteBlocked is a logic-only test (no subprocess spawned)
// that verifies ValidateFSWritePath rejects paths outside the allowed sandbox.
// The green team wires this validation into the pre-spawn gate; this test
// exercises the validation function in isolation per the RED scope note.
func TestPluginSandboxFSWriteBlocked(t *testing.T) {
	defer goleak.VerifyNone(t)

	installRoot := t.TempDir()
	pluginID := "test-plugin"

	// Manifest with no explicit fs_write_dir: allowed root is
	// <install_root>/<plugin_id>/data/.
	m := &plugins.Manifest{
		ManifestSchemaVersion: 1,
		PluginID:              pluginID,
		Shape:                 "mcp-plugin",
		DeclaredCapabilities: plugins.Capabilities{
			FSWriteDir: "", // empty → default sandbox
		},
	}

	// A path inside the allowed data dir must not error.
	allowedPath := filepath.Join(installRoot, pluginID, "data", "output.txt")
	{
		var validateErr error
		msg, panicked := catchPanic(func() {
			validateErr = plugins.ValidateFSWritePath(m, installRoot, pluginID, allowedPath)
		})
		if panicked {
			t.Fatalf("ValidateFSWritePath panicked on allowed path: %s (green team must implement)", msg)
		}
		if validateErr != nil {
			t.Errorf("ValidateFSWritePath(%q) = %v, want nil for allowed path", allowedPath, validateErr)
		}
	}

	// A path that escapes the sandbox must return ErrFSWriteOutsideSandbox.
	escapedPath := filepath.Join(installRoot, "other-plugin", "data", "evil.txt")
	{
		var validateErr error
		msg, panicked := catchPanic(func() {
			validateErr = plugins.ValidateFSWritePath(m, installRoot, pluginID, escapedPath)
		})
		if panicked {
			t.Fatalf("ValidateFSWritePath panicked on escaped path: %s (green team must implement)", msg)
		}
		if !errors.Is(validateErr, plugins.ErrFSWriteOutsideSandbox) {
			t.Errorf("ValidateFSWritePath(%q) = %v, want ErrFSWriteOutsideSandbox", escapedPath, validateErr)
		}
	}

	// A path using .. traversal to escape the sandbox must also be rejected.
	// filepath.Clean resolves ".." before the check, so the cleaned path lands
	// outside the plugin data dir.
	traversalPath := filepath.Join(installRoot, pluginID, "data", "..", "..", "secrets")
	{
		var validateErr error
		msg, panicked := catchPanic(func() {
			validateErr = plugins.ValidateFSWritePath(m, installRoot, pluginID, traversalPath)
		})
		if panicked {
			t.Fatalf("ValidateFSWritePath panicked on traversal path: %s (green team must implement)", msg)
		}
		if !errors.Is(validateErr, plugins.ErrFSWriteOutsideSandbox) {
			t.Errorf("ValidateFSWritePath(%q) = %v, want ErrFSWriteOutsideSandbox for traversal path", traversalPath, validateErr)
		}
	}
}

// Compile-time check: Host.Start signature exists (unused in RED tests, but
// verified so the green team does not accidentally change the signature).
var _ = func() {
	var h *plugins.Host
	var ctx context.Context
	_, _ = h.Start(ctx, "")
}

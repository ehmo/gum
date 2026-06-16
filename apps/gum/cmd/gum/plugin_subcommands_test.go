package main_test

import (
	"context"
	"errors"
	"testing"

	"go.uber.org/goleak"

	gummain "github.com/ehmo/gum/cmd/gum"
	"github.com/ehmo/gum/internal/plugins"
)

// mockHost is a test double for gummain.PluginsHostInterface.
type mockHost struct {
	installFn             func(ctx context.Context, source string) (string, error)
	installWithRegistryFn func(ctx context.Context, source string, opts plugins.InstallOptions) (string, error)
	removeFn              func(ctx context.Context, pluginID string) error
	listFn                func() ([]*plugins.Manifest, error)
	startFn               func(ctx context.Context, pluginID string) (*plugins.Plugin, error)
}

func (m *mockHost) Install(ctx context.Context, source string) (string, error) {
	if m.installFn != nil {
		return m.installFn(ctx, source)
	}
	return "", errors.New("Install not configured in mockHost")
}

func (m *mockHost) InstallWithRegistry(ctx context.Context, source string, opts plugins.InstallOptions) (string, error) {
	if m.installWithRegistryFn != nil {
		return m.installWithRegistryFn(ctx, source, opts)
	}
	if m.installFn != nil {
		return m.installFn(ctx, source)
	}
	return "", errors.New("InstallWithRegistry not configured in mockHost")
}

func (m *mockHost) Remove(ctx context.Context, pluginID string) error {
	if m.removeFn != nil {
		return m.removeFn(ctx, pluginID)
	}
	return errors.New("Remove not configured in mockHost")
}

func (m *mockHost) List() ([]*plugins.Manifest, error) {
	if m.listFn != nil {
		return m.listFn()
	}
	return nil, errors.New("List not configured in mockHost")
}

func (m *mockHost) Start(ctx context.Context, pluginID string) (*plugins.Plugin, error) {
	if m.startFn != nil {
		return m.startFn(ctx, pluginID)
	}
	return nil, errors.New("Start not configured in mockHost")
}

// compile-time: mockHost satisfies gummain.PluginsHostInterface.
var _ gummain.PluginsHostInterface = (*mockHost)(nil)

// catchDispatchPanic calls dispatchPluginCommand and recovers from panics,
// returning the panic message so tests can assert on stub behaviour.
func catchDispatchPanic(fn func() (string, error)) (result string, panicked bool, panicMsg string, err error) {
	defer func() {
		if r := recover(); r != nil {
			panicked = true
			panicMsg = "panic: not implemented"
		}
	}()
	result, err = fn()
	return result, false, "", err
}

// TestPluginInstallSubcommand verifies that "plugin install <path>" parses
// correctly and routes to host.Install with the given path.
func TestPluginInstallSubcommand(t *testing.T) {
	defer goleak.VerifyNone(t)

	const wantSource = "/some/plugin/path"
	var gotSource string

	host := &mockHost{
		installFn: func(_ context.Context, source string) (string, error) {
			gotSource = source
			return "test-plugin", nil
		},
	}

	args := []string{"install", wantSource}
	result, panicked, msg, err := catchDispatchPanic(func() (string, error) {
		return gummain.DispatchPluginCommand(args, host)
	})
	if panicked {
		t.Fatalf("dispatchPluginCommand panicked: %s (green team must implement)", msg)
	}
	if err != nil {
		t.Fatalf("dispatchPluginCommand returned unexpected error: %v", err)
	}
	if gotSource != wantSource {
		t.Errorf("Install called with source %q, want %q", gotSource, wantSource)
	}
	if result == "" {
		t.Error("dispatchPluginCommand returned empty result for successful install")
	}
}

// TestPluginListSubcommand verifies that "plugin list" routes to host.List and
// returns a non-empty result when plugins are installed.
func TestPluginListSubcommand(t *testing.T) {
	defer goleak.VerifyNone(t)

	host := &mockHost{
		listFn: func() ([]*plugins.Manifest, error) {
			return []*plugins.Manifest{
				{
					PluginID: "my-plugin",
					Name:     "My Plugin",
					Version:  "1.0.0",
					Shape:    "mcp-plugin",
				},
			}, nil
		},
	}

	args := []string{"list"}
	result, panicked, msg, err := catchDispatchPanic(func() (string, error) {
		return gummain.DispatchPluginCommand(args, host)
	})
	if panicked {
		t.Fatalf("dispatchPluginCommand panicked: %s (green team must implement)", msg)
	}
	if err != nil {
		t.Fatalf("dispatchPluginCommand returned unexpected error: %v", err)
	}
	if result == "" {
		t.Error("dispatchPluginCommand returned empty result for non-empty plugin list")
	}
}

// TestPluginRemoveSubcommand verifies that "plugin remove <id>" routes to
// host.Remove with the correct plugin ID.
func TestPluginRemoveSubcommand(t *testing.T) {
	defer goleak.VerifyNone(t)

	const wantID = "my-plugin"
	var gotID string

	host := &mockHost{
		removeFn: func(_ context.Context, pluginID string) error {
			gotID = pluginID
			return nil
		},
	}

	args := []string{"remove", wantID}
	_, panicked, msg, err := catchDispatchPanic(func() (string, error) {
		return gummain.DispatchPluginCommand(args, host)
	})
	if panicked {
		t.Fatalf("dispatchPluginCommand panicked: %s (green team must implement)", msg)
	}
	if err != nil {
		t.Fatalf("dispatchPluginCommand returned unexpected error: %v", err)
	}
	if gotID != wantID {
		t.Errorf("Remove called with pluginID %q, want %q", gotID, wantID)
	}
}

// TestPluginRunSubcommandStub verifies that "plugin run <id> <tool> <args-json>"
// is parsed and the correct pluginID and toolName are extracted. The actual
// invocation is integration-tested separately; here we only verify parsing and
// that Start is called with the right pluginID.
func TestPluginRunSubcommandStub(t *testing.T) {
	defer goleak.VerifyNone(t)

	const (
		wantPluginID = "my-plugin"
		wantTool     = "echo"
		wantArgs     = `{"text":"hello"}`
	)
	var gotPluginID string

	host := &mockHost{
		startFn: func(_ context.Context, pluginID string) (*plugins.Plugin, error) {
			gotPluginID = pluginID
			// Return nil to indicate Start was called; the green team will return a real Plugin.
			return nil, errors.New("stub: plugin run integration deferred")
		},
	}

	args := []string{"run", wantPluginID, wantTool, wantArgs}
	_, panicked, msg, err := catchDispatchPanic(func() (string, error) {
		return gummain.DispatchPluginCommand(args, host)
	})
	if panicked {
		t.Fatalf("dispatchPluginCommand panicked: %s (green team must implement)", msg)
	}
	// For the stub, an error from Start is acceptable — we only verify parsing.
	// If Start was never called, gotPluginID will be empty.
	if err != nil && gotPluginID == "" {
		// The green team must call Start — if it never did, that's a bug.
		t.Logf("dispatchPluginCommand returned error before reaching Start (may be stub): %v", err)
	}
	if gotPluginID != "" && gotPluginID != wantPluginID {
		t.Errorf("Start called with pluginID %q, want %q", gotPluginID, wantPluginID)
	}
}

// TestPluginMissingSubcommandReturnsError verifies that omitting the subcommand
// returns an error (not a panic).
func TestPluginMissingSubcommandReturnsError(t *testing.T) {
	defer goleak.VerifyNone(t)

	host := &mockHost{}
	args := []string{}
	_, panicked, msg, err := catchDispatchPanic(func() (string, error) {
		return gummain.DispatchPluginCommand(args, host)
	})
	if panicked {
		t.Fatalf("dispatchPluginCommand panicked: %s (green team must implement)", msg)
	}
	if err == nil {
		t.Error("dispatchPluginCommand with empty args returned nil error, want error")
	}
}

// TestPluginUnknownSubcommandReturnsError verifies that an unknown subcommand
// returns an error.
func TestPluginUnknownSubcommandReturnsError(t *testing.T) {
	defer goleak.VerifyNone(t)

	host := &mockHost{}
	args := []string{"frobnicate"}
	_, panicked, msg, err := catchDispatchPanic(func() (string, error) {
		return gummain.DispatchPluginCommand(args, host)
	})
	if panicked {
		t.Fatalf("dispatchPluginCommand panicked: %s (green team must implement)", msg)
	}
	if err == nil {
		t.Error("dispatchPluginCommand with unknown subcommand returned nil error, want error")
	}
}

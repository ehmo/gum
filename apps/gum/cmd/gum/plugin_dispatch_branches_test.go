package main_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	gummain "github.com/ehmo/gum/cmd/gum"
	"github.com/ehmo/gum/internal/plugins"
)

// TestDispatchPluginInstallMissingArgSurfacesUsage pins the
// `install` len(args) < 2 arm: dispatching just "install" with no
// path/url MUST return the usage error rather than panic on args[1].
func TestDispatchPluginInstallMissingArgSurfacesUsage(t *testing.T) {
	host := &mockHost{}
	_, err := gummain.DispatchPluginCommand([]string{"install"}, host)
	if err == nil {
		t.Fatal("want missing-argument error; got nil")
	}
	if !strings.Contains(err.Error(), "missing <local-dir>") {
		t.Errorf("err=%v; want 'missing <local-dir>' wrap", err)
	}
}

// TestDispatchPluginInstallLegacyErrorPropagates pins the legacy
// (profileDir="") install arm's err != nil branch: host.Install
// returning err MUST bubble unchanged.
func TestDispatchPluginInstallLegacyErrorPropagates(t *testing.T) {
	want := errors.New("synthetic install failure")
	host := &mockHost{
		installFn: func(_ context.Context, _ string) (string, error) {
			return "", want
		},
	}
	_, err := gummain.DispatchPluginCommand([]string{"install", "/p"}, host)
	if !errors.Is(err, want) {
		t.Errorf("err=%v; want sentinel %v", err, want)
	}
}

// TestDispatchPluginInstallRegistryErrorPropagates pins the
// InstallWithRegistry-err arm: when profileDir is set and
// host.InstallWithRegistry returns err, DispatchPluginCommandFull
// MUST bubble it unchanged (vs falling back silently to host.Install).
func TestDispatchPluginInstallRegistryErrorPropagates(t *testing.T) {
	want := errors.New("synthetic registry install failure")
	host := &mockHost{
		installWithRegistryFn: func(_ context.Context, _ string, _ plugins.InstallOptions) (string, error) {
			return "", want
		},
	}
	profileDir := t.TempDir()
	_, err := gummain.DispatchPluginCommandWithOptions(
		[]string{"install", "/p"},
		host,
		profileDir,
		nil,
		gummain.PluginInstallOptions{},
	)
	if !errors.Is(err, want) {
		t.Errorf("err=%v; want sentinel %v", err, want)
	}
}

// TestDispatchPluginRemoveMissingArgSurfacesUsage pins the `remove`
// len(args) < 2 arm: "remove" with no id MUST return the usage error.
func TestDispatchPluginRemoveMissingArgSurfacesUsage(t *testing.T) {
	host := &mockHost{}
	_, err := gummain.DispatchPluginCommand([]string{"remove"}, host)
	if err == nil {
		t.Fatal("want missing-argument error; got nil")
	}
	if !strings.Contains(err.Error(), "missing <id>") {
		t.Errorf("err=%v; want 'missing <id>' wrap", err)
	}
}

// TestDispatchPluginRemoveErrorPropagates pins the host.Remove err
// arm: the sentinel from host.Remove MUST bubble unchanged.
func TestDispatchPluginRemoveErrorPropagates(t *testing.T) {
	want := errors.New("synthetic remove failure")
	host := &mockHost{
		removeFn: func(_ context.Context, _ string) error { return want },
	}
	_, err := gummain.DispatchPluginCommand([]string{"remove", "victim"}, host)
	if !errors.Is(err, want) {
		t.Errorf("err=%v; want sentinel %v", err, want)
	}
}

// TestDispatchPluginRunMissingArgsSurfacesUsage pins the `run`
// len(args) < 3 arm: missing tool name MUST surface usage error.
func TestDispatchPluginRunMissingArgsSurfacesUsage(t *testing.T) {
	host := &mockHost{}
	_, err := gummain.DispatchPluginCommand([]string{"run", "id-only"}, host)
	if err == nil {
		t.Fatal("want missing-args error; got nil")
	}
	if !strings.Contains(err.Error(), "usage: run <id> <tool>") {
		t.Errorf("err=%v; want usage error", err)
	}
}

// TestDispatchPluginRunInvalidArgsJSONSurfacesWrap pins the
// json.Unmarshal err arm: a malformed args-JSON MUST surface a
// "gum plugin run: invalid args JSON" wrap (vs falling through to
// host.Start with garbage args).
func TestDispatchPluginRunInvalidArgsJSONSurfacesWrap(t *testing.T) {
	host := &mockHost{}
	_, err := gummain.DispatchPluginCommand([]string{"run", "id", "tool", "{not json"}, host)
	if err == nil {
		t.Fatal("want JSON parse error; got nil")
	}
	if !strings.Contains(err.Error(), "invalid args JSON") {
		t.Errorf("err=%v; want 'invalid args JSON' wrap", err)
	}
}

// TestDispatchPluginUnquarantineMissingProfileDirSurfacesError pins
// the `unquarantine` openRegistry err arm: when profileDir="" the
// helper returns an "unresolved profile dir" error which MUST bubble.
func TestDispatchPluginUnquarantineMissingProfileDirSurfacesError(t *testing.T) {
	host := &mockHost{}
	_, err := gummain.DispatchPluginCommandWithOptions(
		[]string{"unquarantine", "some-id"},
		host,
		"", // unresolved profile dir → openRegistry returns err
		nil,
		gummain.PluginInstallOptions{},
	)
	if err == nil {
		t.Fatal("want openRegistry error; got nil")
	}
	if !strings.Contains(err.Error(), "profile dir unresolved") {
		t.Errorf("err=%v; want 'profile dir unresolved' wrap", err)
	}
}

// TestDispatchPluginReloadMissingProfileDirSurfacesError pins the
// `reload` openRegistry err arm — same symmetry as unquarantine.
func TestDispatchPluginReloadMissingProfileDirSurfacesError(t *testing.T) {
	host := &mockHost{}
	_, err := gummain.DispatchPluginCommandWithOptions(
		[]string{"reload", "some-id"},
		host,
		"",
		nil,
		gummain.PluginInstallOptions{},
	)
	if err == nil {
		t.Fatal("want openRegistry error; got nil")
	}
	if !strings.Contains(err.Error(), "profile dir unresolved") {
		t.Errorf("err=%v; want 'profile dir unresolved' wrap", err)
	}
}

// TestDispatchPluginSetupMissingArgSurfacesUsage pins the `setup`
// len(args) < 2 arm: dispatching just "setup" with no <name> MUST
// return the usage error.
func TestDispatchPluginSetupMissingArgSurfacesUsage(t *testing.T) {
	host := &mockHost{}
	_, err := gummain.DispatchPluginCommandFull(
		[]string{"setup"},
		host,
		"",
		nil,
		gummain.PluginInstallOptions{},
		gummain.PluginSetupOptions{},
	)
	if err == nil {
		t.Fatal("want missing-argument error; got nil")
	}
	if !strings.Contains(err.Error(), "missing <name>") {
		t.Errorf("err=%v; want 'missing <name>' wrap", err)
	}
}

// TestDispatchPluginSetupMissingProfileDirSurfacesError pins the
// `setup` openRegistry err arm: name provided, profileDir="" →
// openRegistry returns "profile dir unresolved" error.
func TestDispatchPluginSetupMissingProfileDirSurfacesError(t *testing.T) {
	host := &mockHost{}
	_, err := gummain.DispatchPluginCommandFull(
		[]string{"setup", "some-name"},
		host,
		"",
		nil,
		gummain.PluginInstallOptions{},
		gummain.PluginSetupOptions{},
	)
	if err == nil {
		t.Fatal("want openRegistry error; got nil")
	}
	if !strings.Contains(err.Error(), "profile dir unresolved") {
		t.Errorf("err=%v; want 'profile dir unresolved' wrap", err)
	}
}

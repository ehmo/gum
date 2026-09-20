package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/plugins"
)

// armsHost is a plugin host whose every method is overridable. Methods left
// nil fail, so a test that routes to an unexpected method fails loudly instead
// of silently passing on a zero value.
type armsHost struct {
	installFn    func(context.Context, string) (string, error)
	installRegFn func(context.Context, string, plugins.InstallOptions) (string, error)
	removeFn     func(context.Context, string) error
	removeRegFn  func(context.Context, string, plugins.RemoveOptions) error
	listFn       func() ([]*plugins.Manifest, error)
	startFn      func(context.Context, string) (*plugins.Plugin, error)
}

func (h *armsHost) Install(ctx context.Context, src string) (string, error) {
	if h.installFn == nil {
		return "", errors.New("Install not expected")
	}
	return h.installFn(ctx, src)
}

func (h *armsHost) InstallWithRegistry(ctx context.Context, src string, o plugins.InstallOptions) (string, error) {
	if h.installRegFn == nil {
		return "", errors.New("InstallWithRegistry not expected")
	}
	return h.installRegFn(ctx, src, o)
}

func (h *armsHost) Remove(ctx context.Context, id string) error {
	if h.removeFn == nil {
		return errors.New("Remove not expected")
	}
	return h.removeFn(ctx, id)
}

func (h *armsHost) RemoveWithRegistry(ctx context.Context, id string, o plugins.RemoveOptions) error {
	if h.removeRegFn == nil {
		return errors.New("RemoveWithRegistry not expected")
	}
	return h.removeRegFn(ctx, id, o)
}

func (h *armsHost) List() ([]*plugins.Manifest, error) {
	if h.listFn == nil {
		return nil, errors.New("List not expected")
	}
	return h.listFn()
}

func (h *armsHost) Start(ctx context.Context, id string) (*plugins.Plugin, error) {
	if h.startFn == nil {
		return nil, errors.New("Start not expected")
	}
	return h.startFn(ctx, id)
}

// unmakeableProfileDir returns a path under a directory with no write bit, so
// openRegistry's MkdirAll fails. It is the only seam that makes the real
// registry constructor return an error.
func unmakeableProfileDir(t *testing.T) string {
	t.Helper()
	parent := t.TempDir()
	t.Cleanup(func() { _ = os.Chmod(parent, 0o700) })
	if err := os.Chmod(parent, 0o500); err != nil {
		t.Fatalf("chmod %s: %v", parent, err)
	}
	return filepath.Join(parent, "profile")
}

// brokenRegistryDir returns a profile dir whose plugin-state.json is not JSON,
// so registry.Load fails on every read and write transaction.
func brokenRegistryDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "plugin-state.json"), []byte("{ not json"), 0o600); err != nil {
		t.Fatalf("write plugin-state.json: %v", err)
	}
	return dir
}

func TestPluginOpenRegistryFailures(t *testing.T) {
	bad := unmakeableProfileDir(t)
	host := &armsHost{listFn: func() ([]*plugins.Manifest, error) { return nil, nil }}

	cases := []struct {
		name string
		args []string
	}{
		{"install", []string{"install", "/src"}},
		{"list", []string{"list"}},
		{"remove", []string{"remove", "p1"}},
		{"run", []string{"run", "p1", "tool"}},
		{"unquarantine", []string{"unquarantine", "p1"}},
		{"reload", []string{"reload", "p1"}},
		{"setup", []string{"setup", "p1"}},
		{"transfer-namespace", []string{"transfer-namespace", "pfx", "--release", "--yes"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := DispatchPluginCommandWithRegistry(tc.args, host, bad, nil)
			if err == nil {
				t.Fatalf("want a registry error, got output %q", out)
			}
			if !strings.Contains(err.Error(), "mkdir profile dir") {
				t.Fatalf("want the mkdir failure, got %v", err)
			}
		})
	}
}

func TestPluginRegistryReadFailures(t *testing.T) {
	broken := brokenRegistryDir(t)
	host := &armsHost{listFn: func() ([]*plugins.Manifest, error) { return nil, nil }}

	cases := []struct {
		name string
		args []string
		want string
	}{
		{"list inventory", []string{"list"}, ""},
		{"unquarantine", []string{"unquarantine", "p1"}, "gum plugin unquarantine"},
		{"reload", []string{"reload", "p1"}, "gum plugin reload: clear quarantine"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := DispatchPluginCommandWithRegistry(tc.args, host, broken, nil)
			if err == nil {
				t.Fatalf("want a registry load error, got output %q", out)
			}
			if tc.want != "" && !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want %q in the error, got %v", tc.want, err)
			}
		})
	}
}

func TestTransferNamespaceNewOwnerNeedsAValue(t *testing.T) {
	_, err := DispatchPluginCommandWithRegistry(
		[]string{"transfer-namespace", "pfx", "--new-owner"}, &armsHost{}, t.TempDir(), nil)
	if err == nil || !strings.Contains(err.Error(), "--new-owner requires a value") {
		t.Fatalf("want the missing-value error, got %v", err)
	}
}

// A sink with no profile dir has nowhere to write, so Append is a no-op rather
// than an error: the publish it annotates has already committed.
func TestRegistryAuditSinkWithoutProfileDirIsANoOp(t *testing.T) {
	registryAuditSink{}.Append(map[string]any{"event": "ignored"})
}

// An audit sink whose profile dir cannot be created swallows the write: the
// registry transaction it annotates has already committed, so failing here
// would fail an install over a lost warning.
func TestRegistryAuditSinkSwallowsAWriterFailure(t *testing.T) {
	registryAuditSink{profileDir: unmakeableProfileDir(t)}.Append(map[string]any{"event": "dropped"})
}

func TestResolveProfileDirRejectsAnInvalidName(t *testing.T) {
	if _, err := resolveProfileDir("bad/name"); err == nil {
		t.Fatal("want a parse error for a profile name with a separator")
	}
}

// With no home and no XDG_DATA_HOME there is nowhere to put the profile, so
// `gum plugin install` fails before it touches the plugin source.
func TestPluginInstallCmdUnresolvableProfileDir(t *testing.T) {
	t.Setenv("HOME", "")
	t.Setenv("XDG_DATA_HOME", "")
	cmd := newPluginInstallCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--yes", "/nonexistent-source"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "resolve profile dir") {
		t.Fatalf("want the profile-dir resolve error, got %v", err)
	}
}

// An interactive operator gets "No plugins installed." on stderr; stdout stays
// empty so a pipe sees nothing (gum-s985). /dev/null is a character device, so
// it makes isTerminal report true without needing a pty.
func TestPluginListCmdEmptyTellsTheTerminal(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "data"))

	tty, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("open %s: %v", os.DevNull, err)
	}
	t.Cleanup(func() { _ = tty.Close() })
	if !isTerminal(tty) {
		t.Skipf("%s is not a character device on this platform", os.DevNull)
	}

	var stdout bytes.Buffer
	cmd := newPluginListCmd()
	cmd.SetOut(&stdout)
	cmd.SetErr(tty)
	cmd.SetArgs(nil)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("plugin list: %v", err)
	}
	if stdout.String() != "" {
		t.Fatalf("stdout must stay empty for a pipe, got %q", stdout.String())
	}
}

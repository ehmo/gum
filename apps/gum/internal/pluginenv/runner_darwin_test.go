//go:build darwin

package pluginenv

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestDarwinSandboxedCommandWrapsSandboxExec pins the shape of the enforced
// command: sandbox-exec runs the plugin under an inline profile, and the
// plugin's own IO and workdir survive the wrapping.
func TestDarwinSandboxedCommandWrapsSandboxExec(t *testing.T) {
	workDir := t.TempDir()
	cmd, err := NewRunner(RunnerConfig{
		Executable: "/bin/echo",
		WorkDir:    workDir,
		Env:        []string{"PLUG_A=1"},
		Enforce:    true,
	}).Command(context.Background())
	if err != nil {
		t.Fatalf("Command: %v", err)
	}
	if cmd.Path != sandboxExecPath {
		t.Errorf("cmd.Path=%q; want %q", cmd.Path, sandboxExecPath)
	}
	if len(cmd.Args) != 4 || cmd.Args[1] != "-p" || cmd.Args[3] != "/bin/echo" {
		t.Fatalf("cmd.Args=%v; want [sandbox-exec -p <profile> /bin/echo]", cmd.Args)
	}
	if cmd.Dir != workDir {
		t.Errorf("cmd.Dir=%q; want %q", cmd.Dir, workDir)
	}
	if len(cmd.Env) != 1 || cmd.Env[0] != "PLUG_A=1" {
		t.Errorf("cmd.Env=%v; want the filtered allowlist verbatim", cmd.Env)
	}
	if _, err := os.Stat(filepath.Join(workDir, "data")); err != nil {
		t.Errorf("default write root not created: %v", err)
	}
}

// TestDarwinPrepareAllowedWriteRootRejections covers every way a manifest can
// name a write root outside the plugin's workdir. Each one must fail before a
// process is spawned, because the profile would otherwise grant writes there.
func TestDarwinPrepareAllowedWriteRootRejections(t *testing.T) {
	cases := []struct {
		name    string
		workDir func(t *testing.T) string
		writeIn string
		wantErr string
	}{
		{
			name:    "empty_workdir",
			workDir: func(*testing.T) string { return "" },
			wantErr: "empty workdir",
		},
		{
			name:    "workdir_does_not_exist",
			workDir: func(t *testing.T) string { return filepath.Join(t.TempDir(), "gone") },
			wantErr: "resolve workdir symlinks",
		},
		{
			name:    "absolute_write_dir",
			workDir: func(t *testing.T) string { return t.TempDir() },
			writeIn: "/etc",
			wantErr: "must be relative",
		},
		{
			name:    "parent_write_dir",
			workDir: func(t *testing.T) string { return t.TempDir() },
			writeIn: "..",
			wantErr: "escapes workdir",
		},
		{
			name:    "climbing_write_dir",
			workDir: func(t *testing.T) string { return t.TempDir() },
			writeIn: "a/../../outside",
			wantErr: "escapes workdir",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := NewRunner(RunnerConfig{Executable: "/bin/echo", WorkDir: c.workDir(t), FSWriteDir: c.writeIn})
			_, err := r.prepareAllowedWriteRoot()
			if err == nil {
				t.Fatalf("prepareAllowedWriteRoot(%q) err=nil; want %q", c.writeIn, c.wantErr)
			}
			if !strings.Contains(err.Error(), c.wantErr) {
				t.Errorf("err=%v; want it to name %q", err, c.wantErr)
			}
		})
	}
}

// TestDarwinPrepareAllowedWriteRootResolvesSymlinks proves the returned root is
// the resolved path. The profile matches on subpath, so an unresolved symlink
// would name a directory the kernel never sees.
func TestDarwinPrepareAllowedWriteRootResolvesSymlinks(t *testing.T) {
	real := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	got, err := NewRunner(RunnerConfig{Executable: "/bin/echo", WorkDir: link, FSWriteDir: "out"}).prepareAllowedWriteRoot()
	if err != nil {
		t.Fatalf("prepareAllowedWriteRoot: %v", err)
	}
	wantPrefix, err := filepath.EvalSymlinks(real)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	if got != filepath.Join(wantPrefix, "out") {
		t.Errorf("root=%q; want %q", got, filepath.Join(wantPrefix, "out"))
	}
}

// TestDarwinSandboxProfile pins the profile text. A plugin must be able to
// write only under its own root, and network access must be off unless the
// manifest asked for it.
func TestDarwinSandboxProfile(t *testing.T) {
	root := "/tmp/plug/data"

	offline := darwinSandboxProfile(root, false)
	if !strings.Contains(offline, "(deny network*)") {
		t.Errorf("profile %q permits network for a no-network plugin", offline)
	}
	if !strings.Contains(offline, "(deny file-write* (require-not (subpath "+strconv.Quote(root)+")))") {
		t.Errorf("profile %q does not confine writes to %s", offline, root)
	}
	if !strings.HasPrefix(offline, "(version 1)\n") {
		t.Errorf("profile %q lacks the version header sandbox-exec requires", offline)
	}

	if online := darwinSandboxProfile(root, true); strings.Contains(online, "(deny network*)") {
		t.Errorf("profile %q denies network to a network plugin", online)
	}
}

// TestIsPathWithin covers the containment check the write root depends on,
// including the sibling directory whose name merely starts with the root's.
func TestIsPathWithin(t *testing.T) {
	cases := []struct {
		root, path string
		want       bool
	}{
		{root: "/a", path: "/a", want: true},
		{root: "/a", path: "/a/b", want: true},
		{root: "/a", path: "/a/b/c", want: true},
		{root: "/a", path: "/b", want: false},
		{root: "/a/b", path: "/a", want: false},
		{root: "/a", path: "/ab", want: false},
	}
	for _, c := range cases {
		if got := isPathWithin(c.root, c.path); got != c.want {
			t.Errorf("isPathWithin(%q,%q)=%v; want %v", c.root, c.path, got, c.want)
		}
	}
}

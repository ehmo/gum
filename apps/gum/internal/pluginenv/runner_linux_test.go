//go:build linux

package pluginenv

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestLinuxSandboxedCommandCarriesHelperEnv(t *testing.T) {
	workDir := t.TempDir()
	cmd, err := NewRunner(RunnerConfig{
		Executable: "/bin/echo",
		WorkDir:    workDir,
		Enforce:    true,
	}).Command(context.Background())
	if err != nil {
		t.Fatalf("Command: %v", err)
	}
	if cmd.Dir != workDir {
		t.Fatalf("cmd.Dir = %q; want %q", cmd.Dir, workDir)
	}
	for _, want := range []string{
		linuxHelperEnv + "=1",
		linuxTargetEnv + "=/bin/echo",
		linuxWorkDirEnv + "=" + workDir,
	} {
		if !envContains(cmd.Env, want) {
			t.Fatalf("cmd.Env missing %q in %v", want, cmd.Env)
		}
	}
}

func TestLinuxAllowedWriteRootRejectsEscape(t *testing.T) {
	if _, err := linuxAllowedWriteRoot(t.TempDir(), "../outside"); err == nil {
		t.Fatal("linuxAllowedWriteRoot accepted escaping fs_write_dir")
	}
}

func envContains(env []string, want string) bool {
	for _, got := range env {
		if got == want || strings.HasPrefix(got, want) {
			return true
		}
	}
	return false
}

// TestLinuxSandboxedCommandNetworkToggle pins the namespace decision. A
// no-network plugin gets a fresh user and network namespace, so it has no
// route out of the box; a network plugin keeps the host's stack.
func TestLinuxSandboxedCommandNetworkToggle(t *testing.T) {
	offline, err := NewRunner(RunnerConfig{
		Executable: "/bin/echo",
		WorkDir:    t.TempDir(),
		Enforce:    true,
	}).Command(context.Background())
	if err != nil {
		t.Fatalf("Command: %v", err)
	}
	if offline.SysProcAttr == nil {
		t.Fatal("no-network plugin got no SysProcAttr; the net namespace is missing")
	}
	wantFlags := uintptr(syscall.CLONE_NEWUSER | syscall.CLONE_NEWNET)
	if offline.SysProcAttr.Cloneflags != wantFlags {
		t.Errorf("Cloneflags=%#x; want %#x", offline.SysProcAttr.Cloneflags, wantFlags)
	}
	if len(offline.SysProcAttr.UidMappings) != 1 || offline.SysProcAttr.UidMappings[0].HostID != os.Getuid() {
		t.Errorf("UidMappings=%v; want one entry mapping host uid %d", offline.SysProcAttr.UidMappings, os.Getuid())
	}
	if len(offline.SysProcAttr.GidMappings) != 1 || offline.SysProcAttr.GidMappings[0].HostID != os.Getgid() {
		t.Errorf("GidMappings=%v; want one entry mapping host gid %d", offline.SysProcAttr.GidMappings, os.Getgid())
	}
	if offline.SysProcAttr.GidMappingsEnableSetgroups {
		t.Error("setgroups enabled; an unprivileged user namespace must deny it")
	}
	if !envContains(offline.Env, linuxNetworkEnv+"=false") {
		t.Errorf("cmd.Env=%v; want %s=false", offline.Env, linuxNetworkEnv)
	}

	online, err := NewRunner(RunnerConfig{
		Executable: "/bin/echo",
		WorkDir:    t.TempDir(),
		Network:    true,
		Enforce:    true,
	}).Command(context.Background())
	if err != nil {
		t.Fatalf("Command(network): %v", err)
	}
	if online.SysProcAttr != nil {
		t.Errorf("SysProcAttr=%+v; a network plugin must keep the host namespace", online.SysProcAttr)
	}
	if !envContains(online.Env, linuxNetworkEnv+"=true") {
		t.Errorf("cmd.Env=%v; want %s=true", online.Env, linuxNetworkEnv)
	}
}

// TestLinuxAllowedWriteRootRejections covers every way a manifest can name a
// write root the helper must refuse. Each one has to fail before the child is
// spawned, because Landlock would otherwise grant writes there.
func TestLinuxAllowedWriteRootRejections(t *testing.T) {
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
		{
			name:    "unwritable_workdir",
			workDir: unwritableWorkDir,
			wantErr: "create sandbox write root",
		},
		{
			name:    "write_root_symlinked_outside",
			workDir: workDirWithEscapingDataLink,
			wantErr: "sandbox write root escapes workdir",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := linuxAllowedWriteRoot(c.workDir(t), c.writeIn)
			if err == nil {
				t.Fatalf("linuxAllowedWriteRoot(%q) err=nil; want %q", c.writeIn, c.wantErr)
			}
			if !strings.Contains(err.Error(), c.wantErr) {
				t.Errorf("err=%v; want it to name %q", err, c.wantErr)
			}
		})
	}
}

// unwritableWorkDir returns a workdir the caller can traverse but not write,
// so MkdirAll of the write root fails with EACCES.
func unwritableWorkDir(t *testing.T) string {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}

	dir := filepath.Join(t.TempDir(), "locked")
	if err := os.Mkdir(dir, 0o500); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	return dir
}

// workDirWithEscapingDataLink returns a workdir whose default write root is
// already a symlink pointing outside it. MkdirAll accepts the existing
// directory, so only the post-resolve containment check catches it.
func workDirWithEscapingDataLink(t *testing.T) string {
	t.Helper()
	outside := t.TempDir()
	dir := filepath.Join(t.TempDir(), "work")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "data")); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	return dir
}

// TestLinuxAllowedWriteRootResolvesSymlinks proves the returned root is the
// resolved path. Landlock binds a file descriptor, so an unresolved symlink
// would name a directory the kernel never sees.
func TestLinuxAllowedWriteRootResolvesSymlinks(t *testing.T) {
	real := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	got, err := linuxAllowedWriteRoot(link, "out")
	if err != nil {
		t.Fatalf("linuxAllowedWriteRoot: %v", err)
	}
	wantPrefix, err := filepath.EvalSymlinks(real)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	if got != filepath.Join(wantPrefix, "out") {
		t.Errorf("root=%q; want %q", got, filepath.Join(wantPrefix, "out"))
	}
}

// TestLinuxIsPathWithin covers the containment check the write root depends
// on, including the sibling directory whose name merely starts with the
// root's.
func TestLinuxIsPathWithin(t *testing.T) {
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

// TestLinuxCommandSurfacesWriteRootError proves a bad write root stops
// Command before a child exists. The helper trusts the root it is handed, so
// the check has to fail here.
func TestLinuxCommandSurfacesWriteRootError(t *testing.T) {
	cmd, err := NewRunner(RunnerConfig{
		Executable: "/bin/echo",
		WorkDir:    t.TempDir(),
		FSWriteDir: "../outside",
		Enforce:    true,
	}).Command(context.Background())
	if err == nil {
		t.Fatal("Command accepted an escaping fs_write_dir")
	}
	if cmd != nil {
		t.Errorf("cmd=%v; want nil on error", cmd)
	}
	if !strings.Contains(err.Error(), "escapes workdir") {
		t.Errorf("err=%v; want it to name the escape", err)
	}
}

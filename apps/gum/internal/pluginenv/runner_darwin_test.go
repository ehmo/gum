//go:build darwin

package pluginenv

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDarwinSandboxProfileNetworkToggle(t *testing.T) {
	withoutNetwork := darwinSandboxProfile("/tmp/gum-plugin-data", false)
	if !strings.Contains(withoutNetwork, "(deny network*)") {
		t.Fatalf("network=false profile = %q; want network denial", withoutNetwork)
	}
	withNetwork := darwinSandboxProfile("/tmp/gum-plugin-data", true)
	if strings.Contains(withNetwork, "(deny network*)") {
		t.Fatalf("network=true profile = %q; want no network denial", withNetwork)
	}
}

func TestSandboxedCommandUsesSandboxExecAndCreatesDefaultDataRoot(t *testing.T) {
	workDir := t.TempDir()
	r := NewRunner(RunnerConfig{
		Executable: "/bin/echo",
		WorkDir:    workDir,
		Enforce:    true,
	})
	cmd, err := r.Command(context.Background())
	if err != nil {
		t.Fatalf("Command: %v", err)
	}
	if cmd.Path != sandboxExecPath {
		t.Fatalf("cmd.Path = %q; want %q", cmd.Path, sandboxExecPath)
	}
	if len(cmd.Args) < 4 || cmd.Args[1] != "-p" || cmd.Args[len(cmd.Args)-1] != "/bin/echo" {
		t.Fatalf("cmd.Args = %#v; want sandbox-exec -p <profile> /bin/echo", cmd.Args)
	}
	if _, err := os.Stat(filepath.Join(workDir, "data")); err != nil {
		t.Fatalf("default data root not created: %v", err)
	}
}

func TestSandboxedCommandRejectsEscapingFSWriteDir(t *testing.T) {
	r := NewRunner(RunnerConfig{
		Executable: "/bin/echo",
		WorkDir:    t.TempDir(),
		FSWriteDir: "../outside",
		Enforce:    true,
	})
	if _, err := r.Command(context.Background()); err == nil {
		t.Fatal("Command succeeded with escaping fs_write_dir")
	}
}

func TestSandboxedCommandRejectsAbsoluteFSWriteDir(t *testing.T) {
	r := NewRunner(RunnerConfig{
		Executable: "/bin/echo",
		WorkDir:    t.TempDir(),
		FSWriteDir: t.TempDir(),
		Enforce:    true,
	})
	if _, err := r.Command(context.Background()); err == nil {
		t.Fatal("Command succeeded with absolute fs_write_dir")
	}
}

func TestSandboxedCommandSurfacesWorkDirAbsError(t *testing.T) {
	oldDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	deletedCWD := t.TempDir()
	if err := os.Chdir(deletedCWD); err != nil {
		t.Fatalf("chdir temp: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldDir) })
	if err := os.RemoveAll(deletedCWD); err != nil {
		t.Fatalf("remove cwd: %v", err)
	}
	r := NewRunner(RunnerConfig{
		Executable: "/bin/echo",
		WorkDir:    "relative-workdir",
		Enforce:    true,
	})
	if _, err := r.Command(context.Background()); err == nil {
		t.Fatal("Command succeeded when filepath.Abs should fail")
	}
}

func TestSandboxedCommandRejectsEmptyWorkDir(t *testing.T) {
	r := NewRunner(RunnerConfig{
		Executable: "/bin/echo",
		Enforce:    true,
	})
	if _, err := r.Command(context.Background()); err == nil {
		t.Fatal("Command succeeded with empty workdir")
	}
}

func TestSandboxedCommandRejectsMissingWorkDir(t *testing.T) {
	r := NewRunner(RunnerConfig{
		Executable: "/bin/echo",
		WorkDir:    filepath.Join(t.TempDir(), "missing"),
		Enforce:    true,
	})
	if _, err := r.Command(context.Background()); err == nil {
		t.Fatal("Command succeeded with missing workdir")
	}
}

func TestSandboxedCommandSurfacesWriteRootCreateError(t *testing.T) {
	workDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(workDir, "writable"), []byte("not a dir"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}
	r := NewRunner(RunnerConfig{
		Executable: "/bin/echo",
		WorkDir:    workDir,
		FSWriteDir: filepath.Join("writable", "child"),
		Enforce:    true,
	})
	if _, err := r.Command(context.Background()); err == nil {
		t.Fatal("Command succeeded when write root creation should fail")
	}
}

func TestSandboxedCommandRejectsFileWriteRoot(t *testing.T) {
	workDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(workDir, "writable"), []byte("not a dir"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}
	r := NewRunner(RunnerConfig{
		Executable: "/bin/echo",
		WorkDir:    workDir,
		FSWriteDir: "writable",
		Enforce:    true,
	})
	if _, err := r.Command(context.Background()); err == nil {
		t.Fatal("Command succeeded with file write root")
	}
}

func TestSandboxedCommandRejectsDanglingSymlinkWriteRoot(t *testing.T) {
	workDir := t.TempDir()
	if err := os.Symlink(filepath.Join(workDir, "missing"), filepath.Join(workDir, "writable")); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	r := NewRunner(RunnerConfig{
		Executable: "/bin/echo",
		WorkDir:    workDir,
		FSWriteDir: "writable",
		Enforce:    true,
	})
	if _, err := r.Command(context.Background()); err == nil {
		t.Fatal("Command succeeded with dangling symlink write root")
	}
}

func TestSandboxedCommandRejectsSymlinkToFileWriteRoot(t *testing.T) {
	workDir := t.TempDir()
	filePath := filepath.Join(workDir, "file")
	if err := os.WriteFile(filePath, []byte("not a dir"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if err := os.Symlink(filePath, filepath.Join(workDir, "writable")); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	r := NewRunner(RunnerConfig{
		Executable: "/bin/echo",
		WorkDir:    workDir,
		FSWriteDir: "writable",
		Enforce:    true,
	})
	if _, err := r.Command(context.Background()); err == nil {
		t.Fatal("Command succeeded with symlink-to-file write root")
	}
}

func TestSandboxedCommandRejectsSymlinkWriteRootEscape(t *testing.T) {
	workDir := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(workDir, "writable")); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	r := NewRunner(RunnerConfig{
		Executable: "/bin/echo",
		WorkDir:    workDir,
		FSWriteDir: "writable",
		Enforce:    true,
	})
	if _, err := r.Command(context.Background()); err == nil {
		t.Fatal("Command succeeded with symlink write root escaping workdir")
	}
}

func TestPathWithin(t *testing.T) {
	root := filepath.Join(string(filepath.Separator), "tmp", "gum-root")
	if !isPathWithin(root, filepath.Join(root, "child")) {
		t.Fatal("isPathWithin rejected child path")
	}
	if isPathWithin(root, filepath.Join(string(filepath.Separator), "tmp", "other")) {
		t.Fatal("isPathWithin accepted sibling path")
	}
}

func BenchmarkSandboxPolicyBuild(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = darwinSandboxProfile("/tmp/gum-plugin-data", false)
	}
}

func BenchmarkPluginStartRawVsSandboxed(b *testing.B) {
	ctx := context.Background()
	for _, tc := range []struct {
		name    string
		enforce bool
	}{
		{name: "raw", enforce: false},
		{name: "sandboxed", enforce: true},
	} {
		b.Run(tc.name, func(b *testing.B) {
			workDir := b.TempDir()
			for i := 0; i < b.N; i++ {
				var stderr bytes.Buffer
				cmd, err := NewRunner(RunnerConfig{
					Executable: "/usr/bin/true",
					WorkDir:    workDir,
					Stderr:     &stderr,
					Enforce:    tc.enforce,
				}).Command(ctx)
				if err != nil {
					b.Fatalf("Command: %v", err)
				}
				if err := cmd.Start(); err != nil {
					b.Fatalf("Start: %v (stderr=%s)", err, stderr.String())
				}
				if err := cmd.Wait(); err != nil && !errors.Is(err, context.Canceled) {
					b.Fatalf("Wait: %v (stderr=%s)", err, stderr.String())
				}
			}
		})
	}
}

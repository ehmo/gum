//go:build linux

package pluginenv

import (
	"context"
	"strings"
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

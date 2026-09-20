//go:build linux

package pluginenv

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// TestLinuxPluginEnvStripsHelperVars proves the helper's own control
// variables never reach the plugin. A plugin that could read
// GUM_PLUGINENV_TARGET would learn the host layout, and one that could set
// GUM_PLUGINENV_LINUX_HELPER in a child would re-enter the helper.
func TestLinuxPluginEnvStripsHelperVars(t *testing.T) {
	got := linuxPluginEnv([]string{
		"PATH=/usr/bin",
		linuxHelperEnv + "=1",
		"HOME=/home/plug",
		linuxTargetEnv + "=/opt/plug/bin",
		linuxWorkDirEnv + "=/opt/plug",
		linuxFSWriteDirEnv + "=/opt/plug/data",
		linuxNetworkEnv + "=false",
		"GUM_PLUGINENV_FUTURE=x",
		"PLUG_A=1",
	})
	want := []string{"PATH=/usr/bin", "HOME=/home/plug", "PLUG_A=1"}
	if len(got) != len(want) {
		t.Fatalf("linuxPluginEnv=%v; want %v", got, want)
	}
	for i, kv := range want {
		if got[i] != kv {
			t.Errorf("linuxPluginEnv[%d]=%q; want %q", i, got[i], kv)
		}
	}
}

// TestLinuxLandlockWriteAccessNamesEveryWriteVerb pins the handled access
// mask. A verb missing from the mask is a write Landlock never restricts, so
// the sandbox would silently permit it outside the plugin's write root.
func TestLinuxLandlockWriteAccessNamesEveryWriteVerb(t *testing.T) {
	got := linuxLandlockWriteAccess()
	verbs := map[string]uint64{
		"WRITE_FILE":  unix.LANDLOCK_ACCESS_FS_WRITE_FILE,
		"REMOVE_DIR":  unix.LANDLOCK_ACCESS_FS_REMOVE_DIR,
		"REMOVE_FILE": unix.LANDLOCK_ACCESS_FS_REMOVE_FILE,
		"MAKE_CHAR":   unix.LANDLOCK_ACCESS_FS_MAKE_CHAR,
		"MAKE_DIR":    unix.LANDLOCK_ACCESS_FS_MAKE_DIR,
		"MAKE_REG":    unix.LANDLOCK_ACCESS_FS_MAKE_REG,
		"MAKE_SOCK":   unix.LANDLOCK_ACCESS_FS_MAKE_SOCK,
		"MAKE_FIFO":   unix.LANDLOCK_ACCESS_FS_MAKE_FIFO,
		"MAKE_BLOCK":  unix.LANDLOCK_ACCESS_FS_MAKE_BLOCK,
		"MAKE_SYM":    unix.LANDLOCK_ACCESS_FS_MAKE_SYM,
		"REFER":       unix.LANDLOCK_ACCESS_FS_REFER,
		"TRUNCATE":    unix.LANDLOCK_ACCESS_FS_TRUNCATE,
	}
	for name, bit := range verbs {
		if got&bit == 0 {
			t.Errorf("handled mask %#x omits LANDLOCK_ACCESS_FS_%s", got, name)
		}
	}
	if got&unix.LANDLOCK_ACCESS_FS_READ_FILE != 0 {
		t.Errorf("handled mask %#x restricts reads; the sandbox confines writes only", got)
	}
}

// TestRunLinuxSandboxHelperRejectsBadEnv covers the helper's pre-flight
// checks. Each one has to fail before Landlock is applied, because the
// process cannot drop the restriction once it is on.
//
// No case names a workdir the helper can enter: a successful chdir would
// move the test process itself.
func TestRunLinuxSandboxHelperRejectsBadEnv(t *testing.T) {
	cases := []struct {
		name      string
		target    string
		workDir   func(t *testing.T) string
		writeRoot func(t *testing.T) string
		wantErr   string
	}{
		{
			name:    "empty_target",
			wantErr: "empty linux helper target",
		},
		{
			name:    "unreachable_workdir",
			target:  "/bin/true",
			workDir: func(t *testing.T) string { return filepath.Join(t.TempDir(), "gone") },
			wantErr: "chdir workdir",
		},
		{
			name:    "empty_write_root",
			target:  "/bin/true",
			wantErr: "empty linux helper write root",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv(linuxTargetEnv, c.target)
			t.Setenv(linuxWorkDirEnv, "")
			t.Setenv(linuxFSWriteDirEnv, "")
			if c.workDir != nil {
				t.Setenv(linuxWorkDirEnv, c.workDir(t))
			}
			if c.writeRoot != nil {
				t.Setenv(linuxFSWriteDirEnv, c.writeRoot(t))
			}

			err := runLinuxSandboxHelper()
			if err == nil {
				t.Fatalf("runLinuxSandboxHelper err=nil; want %q", c.wantErr)
			}
			if !strings.Contains(err.Error(), c.wantErr) {
				t.Errorf("err=%v; want it to name %q", err, c.wantErr)
			}
		})
	}
}

// TestRunLinuxSandboxHelperSurfacesLandlockFailure proves a Landlock failure
// stops the helper instead of exec'ing the plugin unconfined. A write root
// that does not exist is the cheapest way to make the ruleset fail.
func TestRunLinuxSandboxHelperSurfacesLandlockFailure(t *testing.T) {
	t.Setenv(linuxTargetEnv, "/bin/true")
	t.Setenv(linuxWorkDirEnv, "")
	t.Setenv(linuxFSWriteDirEnv, filepath.Join(t.TempDir(), "gone"))

	err := runLinuxSandboxHelper()
	if err == nil {
		t.Fatal("runLinuxSandboxHelper err=nil; the plugin would have run unconfined")
	}
	requireLandlockOpenFailure(t, err)
}

// TestApplyLinuxLandlockRejectsMissingWriteRoot covers the open arm on its
// own. The success path is deliberately untested in process: Landlock is
// irreversible, so restricting the test binary would break every later test.
func TestApplyLinuxLandlockRejectsMissingWriteRoot(t *testing.T) {
	err := applyLinuxLandlock(filepath.Join(t.TempDir(), "gone"))
	if err == nil {
		t.Fatal("applyLinuxLandlock err=nil for a missing write root")
	}
	requireLandlockOpenFailure(t, err)
}

// requireLandlockOpenFailure asserts the error came from opening the write
// root. A kernel built without Landlock fails one step earlier, which is a
// property of the host rather than of the code under test.
func requireLandlockOpenFailure(t *testing.T, err error) {
	t.Helper()
	if errors.Is(err, ErrUnsupportedSandbox) {
		t.Skipf("kernel has no Landlock support: %v", err)
	}
	if !strings.Contains(err.Error(), "open landlock write root") {
		t.Errorf("err=%v; want it to name the write root open", err)
	}
}

// TestLinuxHelperInitExitsOnFailure drives the package init through a child
// process. A helper that cannot sandbox must exit non-zero, never fall
// through and run the plugin.
func TestLinuxHelperInitExitsOnFailure(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	cmd := exec.Command(self)
	cmd.Env = []string{linuxHelperEnv + "=1"}
	out, err := cmd.CombinedOutput()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("child err=%v, out=%s; want a non-zero exit", err, out)
	}
	if exitErr.ExitCode() != 127 {
		t.Errorf("exit code=%d; want 127", exitErr.ExitCode())
	}
	if !strings.Contains(string(out), "empty linux helper target") {
		t.Errorf("child output=%q; want the helper error on stderr", out)
	}
}

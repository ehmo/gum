//go:build linux

package pluginenv

import (
	"fmt"
	"os"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
)

func init() {
	if os.Getenv(linuxHelperEnv) != "1" {
		return
	}
	if err := runLinuxSandboxHelper(); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(127)
	}
}

func runLinuxSandboxHelper() error {
	target := os.Getenv(linuxTargetEnv)
	if target == "" {
		return fmt.Errorf("pluginenv: empty linux helper target")
	}
	if workDir := os.Getenv(linuxWorkDirEnv); workDir != "" {
		if err := os.Chdir(workDir); err != nil {
			return fmt.Errorf("pluginenv: chdir workdir: %w", err)
		}
	}
	writeRoot := os.Getenv(linuxFSWriteDirEnv)
	if writeRoot == "" {
		return fmt.Errorf("pluginenv: empty linux helper write root")
	}
	if err := applyLinuxLandlock(writeRoot); err != nil {
		return err
	}
	return syscall.Exec(target, []string{target}, linuxPluginEnv(os.Environ()))
}

func linuxPluginEnv(env []string) []string {
	out := env[:0]
	for _, kv := range env {
		if strings.HasPrefix(kv, "GUM_PLUGINENV_") {
			continue
		}
		out = append(out, kv)
	}
	return out
}

func applyLinuxLandlock(writeRoot string) error {
	handled := linuxLandlockWriteAccess()
	ruleset := unix.LandlockRulesetAttr{Access_fs: handled}
	fd, _, errno := unix.Syscall(unix.SYS_LANDLOCK_CREATE_RULESET, uintptr(unsafe.Pointer(&ruleset)), unsafe.Sizeof(ruleset), 0)
	if errno != 0 {
		return fmt.Errorf("%w: landlock create ruleset: %v", ErrUnsupportedSandbox, errno)
	}
	defer unix.Close(int(fd))

	rootFD, err := unix.Open(writeRoot, unix.O_PATH|unix.O_CLOEXEC, 0)
	if err != nil {
		return fmt.Errorf("pluginenv: open landlock write root: %w", err)
	}
	defer unix.Close(rootFD)

	pathRule := unix.LandlockPathBeneathAttr{
		Allowed_access: handled,
		Parent_fd:      int32(rootFD),
	}
	_, _, errno = unix.Syscall6(unix.SYS_LANDLOCK_ADD_RULE, fd, unix.LANDLOCK_RULE_PATH_BENEATH, uintptr(unsafe.Pointer(&pathRule)), 0, 0, 0)
	if errno != 0 {
		return fmt.Errorf("%w: landlock add write root rule: %v", ErrUnsupportedSandbox, errno)
	}
	if err := unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0); err != nil {
		return fmt.Errorf("%w: set no_new_privs: %v", ErrUnsupportedSandbox, err)
	}
	_, _, errno = unix.Syscall(unix.SYS_LANDLOCK_RESTRICT_SELF, fd, 0, 0)
	if errno != 0 {
		return fmt.Errorf("%w: landlock restrict self: %v", ErrUnsupportedSandbox, errno)
	}
	return nil
}

func linuxLandlockWriteAccess() uint64 {
	return unix.LANDLOCK_ACCESS_FS_WRITE_FILE |
		unix.LANDLOCK_ACCESS_FS_REMOVE_DIR |
		unix.LANDLOCK_ACCESS_FS_REMOVE_FILE |
		unix.LANDLOCK_ACCESS_FS_MAKE_CHAR |
		unix.LANDLOCK_ACCESS_FS_MAKE_DIR |
		unix.LANDLOCK_ACCESS_FS_MAKE_REG |
		unix.LANDLOCK_ACCESS_FS_MAKE_SOCK |
		unix.LANDLOCK_ACCESS_FS_MAKE_FIFO |
		unix.LANDLOCK_ACCESS_FS_MAKE_BLOCK |
		unix.LANDLOCK_ACCESS_FS_MAKE_SYM |
		unix.LANDLOCK_ACCESS_FS_REFER |
		unix.LANDLOCK_ACCESS_FS_TRUNCATE
}

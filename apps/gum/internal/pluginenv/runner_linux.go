//go:build linux

package pluginenv

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

const (
	linuxHelperEnv       = "GUM_PLUGINENV_LINUX_HELPER"
	linuxTargetEnv       = "GUM_PLUGINENV_TARGET"
	linuxWorkDirEnv      = "GUM_PLUGINENV_WORKDIR"
	linuxFSWriteDirEnv   = "GUM_PLUGINENV_FS_WRITE_DIR"
	linuxNetworkEnv      = "GUM_PLUGINENV_NETWORK"
	linuxHelperArg       = "__gum_pluginenv_linux_helper"
	linuxHelperNetworkOn = "1"
)

func (r *SandboxedRunner) sandboxedCommand(ctx context.Context) (*exec.Cmd, error) {
	writeRoot, err := linuxAllowedWriteRoot(r.cfg.WorkDir, r.cfg.FSWriteDir)
	if err != nil {
		return nil, err
	}
	self, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("pluginenv: resolve helper executable: %w", err)
	}
	env := append([]string{}, r.cfg.Env...)
	env = append(env,
		linuxHelperEnv+"=1",
		linuxTargetEnv+"="+r.cfg.Executable,
		linuxWorkDirEnv+"="+r.cfg.WorkDir,
		linuxFSWriteDirEnv+"="+writeRoot,
		linuxNetworkEnv+"="+strconv.FormatBool(r.cfg.Network),
	)
	cmd := exec.CommandContext(ctx, self, linuxHelperArg)
	if !r.cfg.Network {
		cmd.SysProcAttr = &syscall.SysProcAttr{
			Cloneflags: syscall.CLONE_NEWUSER | syscall.CLONE_NEWNET,
			UidMappings: []syscall.SysProcIDMap{{
				ContainerID: 0,
				HostID:      os.Getuid(),
				Size:        1,
			}},
			GidMappings: []syscall.SysProcIDMap{{
				ContainerID: 0,
				HostID:      os.Getgid(),
				Size:        1,
			}},
			GidMappingsEnableSetgroups: false,
		}
	}
	applyCommandIO(cmd, r.cfg)
	cmd.Env = env
	return cmd, nil
}

func linuxAllowedWriteRoot(workDir, fsWriteDir string) (string, error) {
	if workDir == "" {
		return "", fmt.Errorf("pluginenv: empty workdir")
	}
	workDir, _ = filepath.Abs(workDir)
	workDirResolved, err := filepath.EvalSymlinks(workDir)
	if err != nil {
		return "", fmt.Errorf("pluginenv: resolve workdir symlinks: %w", err)
	}
	writeRel := fsWriteDir
	if writeRel == "" {
		writeRel = "data"
	}
	if filepath.IsAbs(writeRel) {
		return "", fmt.Errorf("pluginenv: fs_write_dir must be relative")
	}
	writeRel = filepath.Clean(writeRel)
	if writeRel == ".." || strings.HasPrefix(writeRel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("pluginenv: fs_write_dir escapes workdir")
	}
	allowedRoot := filepath.Join(workDir, writeRel)
	if err := os.MkdirAll(allowedRoot, 0o755); err != nil {
		return "", fmt.Errorf("pluginenv: create sandbox write root: %w", err)
	}
	allowedRootResolved, _ := filepath.EvalSymlinks(allowedRoot)
	if !isPathWithin(workDirResolved, allowedRootResolved) {
		return "", fmt.Errorf("pluginenv: sandbox write root escapes workdir")
	}
	return allowedRootResolved, nil
}

func isPathWithin(root, path string) bool {
	rel, _ := filepath.Rel(root, path)
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

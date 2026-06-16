//go:build darwin

package pluginenv

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

const sandboxExecPath = "/usr/bin/sandbox-exec"

func (r *SandboxedRunner) sandboxedCommand(ctx context.Context) (*exec.Cmd, error) {
	allowedWriteRoot, err := r.prepareAllowedWriteRoot()
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, sandboxExecPath, "-p", darwinSandboxProfile(allowedWriteRoot, r.cfg.Network), r.cfg.Executable)
	applyCommandIO(cmd, r.cfg)
	return cmd, nil
}

func (r *SandboxedRunner) prepareAllowedWriteRoot() (string, error) {
	if r.cfg.WorkDir == "" {
		return "", fmt.Errorf("pluginenv: empty workdir")
	}

	workDir, _ := filepath.Abs(r.cfg.WorkDir)
	workDirResolved, err := filepath.EvalSymlinks(workDir)
	if err != nil {
		return "", fmt.Errorf("pluginenv: resolve workdir symlinks: %w", err)
	}

	writeRel := r.cfg.FSWriteDir
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

func darwinSandboxProfile(allowedWriteRoot string, network bool) string {
	var b strings.Builder
	b.WriteString("(version 1)\n")
	b.WriteString("(allow default)\n")
	b.WriteString("(allow process*)\n")
	b.WriteString("(deny file-write* (require-not (subpath ")
	b.WriteString(strconv.Quote(allowedWriteRoot))
	b.WriteString(")))\n")
	if !network {
		b.WriteString("(deny network*)\n")
	}
	return b.String()
}

func isPathWithin(root, path string) bool {
	rel, _ := filepath.Rel(root, path)
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

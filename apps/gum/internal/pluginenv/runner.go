package pluginenv

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
)

var ErrUnsupportedSandbox = errors.New("pluginenv: OS sandbox unsupported")

// SandboxedRunner constructs and spawns a plugin subprocess with restricted env,
// working directory, and optional OS sandbox enforcement.
type SandboxedRunner struct {
	cfg RunnerConfig
}

// RunnerConfig configures a SandboxedRunner.
type RunnerConfig struct {
	Executable string
	WorkDir    string
	Env        []string // pre-filtered allowlist
	Stdin      io.Reader
	Stdout     io.Writer
	Stderr     io.Writer
	Network    bool   // true permits network from inside the OS sandbox
	FSWriteDir string // relative write root under WorkDir; empty means data/
	Enforce    bool   // true requires OS sandboxing or fails closed
}

// NewRunner constructs a runner from cfg.
func NewRunner(cfg RunnerConfig) *SandboxedRunner {
	return &SandboxedRunner{cfg: cfg}
}

// Command returns an unstarted command. CommandTransport owns Start/Wait.
func (r *SandboxedRunner) Command(ctx context.Context) (*exec.Cmd, error) {
	if r.cfg.Executable == "" {
		return nil, fmt.Errorf("pluginenv: empty executable")
	}

	if r.cfg.Enforce {
		return r.sandboxedCommand(ctx)
	}
	return r.rawCommand(ctx), nil
}

// Start launches the subprocess. Returns a *exec.Cmd so callers can Wait.
func (r *SandboxedRunner) Start(ctx context.Context) (*exec.Cmd, error) {
	cmd, err := r.Command(ctx)
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return cmd, nil
}

func (r *SandboxedRunner) rawCommand(ctx context.Context) *exec.Cmd {
	cmd := exec.CommandContext(ctx, r.cfg.Executable)
	applyCommandIO(cmd, r.cfg)
	return cmd
}

func applyCommandIO(cmd *exec.Cmd, cfg RunnerConfig) {
	cmd.Dir = cfg.WorkDir
	cmd.Env = cfg.Env
	cmd.Stdin = cfg.Stdin
	cmd.Stdout = cfg.Stdout
	cmd.Stderr = cfg.Stderr
}

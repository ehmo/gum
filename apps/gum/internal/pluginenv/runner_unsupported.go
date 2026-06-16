//go:build !darwin && !linux

package pluginenv

import (
	"context"
	"os/exec"
)

func (r *SandboxedRunner) sandboxedCommand(context.Context) (*exec.Cmd, error) {
	return nil, ErrUnsupportedSandbox
}

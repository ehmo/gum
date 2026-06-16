//go:build !darwin && !linux

package pluginenv_test

import (
	"context"
	"errors"
	"testing"

	"github.com/ehmo/gum/internal/pluginenv"
)

func TestSandboxedCommandFailsClosedOnUnsupportedPlatform(t *testing.T) {
	r := pluginenv.NewRunner(pluginenv.RunnerConfig{
		Executable: "/bin/echo",
		WorkDir:    t.TempDir(),
		Enforce:    true,
	})
	if _, err := r.Command(context.Background()); !errors.Is(err, pluginenv.ErrUnsupportedSandbox) {
		t.Fatalf("Command err = %v; want ErrUnsupportedSandbox", err)
	}
}

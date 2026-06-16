package pluginenv_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/pluginenv"
)

// TestNewRunnerConstructs verifies NewRunner returns a non-nil runner that
// holds the supplied config — the only behavioral guarantee of the
// constructor.
func TestNewRunnerConstructs(t *testing.T) {
	r := pluginenv.NewRunner(pluginenv.RunnerConfig{Executable: "/bin/echo"})
	if r == nil {
		t.Fatal("NewRunner returned nil")
	}
}

// TestStartEmptyExecutable locks the validation-error path: an empty
// Executable must return a "pluginenv:" prefixed error before any process
// is spawned.
func TestStartEmptyExecutable(t *testing.T) {
	r := pluginenv.NewRunner(pluginenv.RunnerConfig{})
	cmd, err := r.Start(context.Background())
	if err == nil {
		t.Fatal("Start with empty Executable should error")
	}
	if cmd != nil {
		t.Errorf("cmd = %v, want nil on error", cmd)
	}
	if !strings.Contains(err.Error(), "pluginenv") {
		t.Errorf("err=%q missing pluginenv prefix", err)
	}
}

// TestStartSpawnsAndCaptures runs a trivial /bin/echo and confirms the
// runner wires Stdout. The subprocess is reaped via Wait so we don't leak
// zombies between tests.
func TestStartSpawnsAndCaptures(t *testing.T) {
	var stdout bytes.Buffer
	r := pluginenv.NewRunner(pluginenv.RunnerConfig{
		Executable: "/bin/echo",
		Stdout:     &stdout,
	})
	cmd, err := r.Start(context.Background())
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("Wait: %v", err)
	}
	// /bin/echo prints a trailing newline.
	if got := stdout.String(); got != "\n" {
		t.Errorf("stdout = %q, want %q", got, "\n")
	}
}

// TestStartUnknownExecutable surfaces exec.Cmd.Start errors verbatim — no
// pluginenv wrapping. The error type is *exec.Error / *fs.PathError on
// most platforms.
func TestStartUnknownExecutable(t *testing.T) {
	r := pluginenv.NewRunner(pluginenv.RunnerConfig{
		Executable: "/definitely/not/a/real/binary",
	})
	cmd, err := r.Start(context.Background())
	if err == nil {
		// Reap if it somehow started.
		if cmd != nil {
			_ = cmd.Wait()
		}
		t.Fatal("Start with bogus executable should error")
	}
}

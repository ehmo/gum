package plugins

import (
	"errors"
	"testing"
)

// TestIsHarmlessWaitError pins which Wait() errors the host swallows after
// it has driven the subprocess through Stop. SIGTERM-killed children and
// non-zero exits are expected (the CommandTransport closes pipes before
// the process can flush), so the host treats them as success. Anything
// else (genuine OS errors, IO failures) must propagate.
func TestIsHarmlessWaitError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil_is_harmless", err: nil, want: true},
		{name: "signal_terminated", err: errors.New("signal: terminated"), want: true},
		{name: "signal_killed", err: errors.New("signal: killed"), want: true},
		{name: "exit_status_1", err: errors.New("exit status 1"), want: true},
		{name: "exit_status_137", err: errors.New("exit status 137"), want: true},
		{name: "broken_pipe_propagates", err: errors.New("write |1: broken pipe"), want: false},
		{name: "io_error_propagates", err: errors.New("read /dev/null: input/output error"), want: false},
		{name: "arbitrary_error", err: errors.New("disk full"), want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isHarmlessWaitError(tc.err); got != tc.want {
				t.Errorf("isHarmlessWaitError(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

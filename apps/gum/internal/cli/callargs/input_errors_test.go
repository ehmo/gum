package callargs

import (
	"errors"
	"io"
	"strings"
	"testing"
	"testing/iotest"
)

func TestStdinReadFailures(t *testing.T) {
	for _, tc := range []struct {
		name    string
		input   io.Reader
		message string
	}{
		{"read error", iotest.ErrReader(errors.New("input interrupted")), "input interrupted"},
		{"size limit", strings.NewReader(strings.Repeat(" ", maxArgFileBytes+1)), "file exceeds"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, err := ParseArgs([]string{"@-"}, Options{Stdin: tc.input})
			var argErr *Error
			if result != nil || !errors.As(err, &argErr) || argErr.Code != "CLI_ARG_INVALID" || argErr.Arg != "@-" || !strings.Contains(argErr.Reason, tc.message) {
				t.Fatalf("result=%v error=%v; want CLI_ARG_INVALID naming stdin and %q", result, err, tc.message)
			}
		})
	}
}

//go:build !windows

package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/cli/callargs"
)

// TestAdsDefaultsInCallWizard runs `gum call` with /dev/null as stdin. On
// macOS and Linux /dev/null is a character device, so isReaderTerminal treats
// it as a TTY and the wizard runs; every read then returns EOF. Both cases fail
// before dispatch, so no credentials or network are touched.
func TestAdsDefaultsInCallWizard(t *testing.T) {
	cases := []struct {
		name       string
		envID      string
		wantErr    string
		wantPrompt bool
	}{
		{
			name:    "malformed default fails before any prompt",
			envID:   "bad",
			wantErr: envAdsCustomerID,
		},
		{
			name:       "default removes the customerId prompt",
			envID:      "1234567890",
			wantErr:    "keywords is required",
			wantPrompt: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			isolateAdsDefaults(t)
			t.Setenv(envAdsCustomerID, tc.envID)
			tty := openCharDevice(t)

			args := []string{"call", historicalMetricsOp, "--risk=read"}
			root := newRootCmd()
			registerDynamicCallFlags(root, args)
			var stdout, stderr bytes.Buffer
			root.SetOut(&stdout)
			root.SetErr(&stderr)
			root.SetIn(tty)
			root.SetArgs(args)

			err := root.ExecuteContext(context.Background())
			var cerr *callargs.Error
			if !errors.As(err, &cerr) || cerr.Code != "CLI_ARG_INVALID" || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("call error = %v; want CLI_ARG_INVALID containing %q", err, tc.wantErr)
			}
			prompts := stderr.String()
			if strings.Contains(prompts, "customerId") {
				t.Errorf("wizard prompted for customerId although a default is set:\n%s", prompts)
			}
			if got := strings.Contains(prompts, "keywords"); got != tc.wantPrompt {
				t.Errorf("keywords prompt shown = %v; want %v\nstderr:\n%s", got, tc.wantPrompt, prompts)
			}
		})
	}
}

// openCharDevice opens /dev/null and skips the test when it is not a
// character device on this platform.
func openCharDevice(t *testing.T) *os.File {
	t.Helper()
	f, err := os.Open(os.DevNull)
	if err != nil {
		t.Skipf("open %s: %v", os.DevNull, err)
	}
	t.Cleanup(func() { _ = f.Close() })
	info, err := f.Stat()
	if err != nil || info.Mode()&os.ModeCharDevice == 0 {
		t.Skipf("%s is not a character device here", os.DevNull)
	}
	return f
}

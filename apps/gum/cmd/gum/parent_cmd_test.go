package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestParentCommandsRejectUnknownArgs(t *testing.T) {
	for _, name := range []string{"catalog", "auth", "plugin", "profile", "config", "cache"} {
		t.Run(name, func(t *testing.T) {
			cmd := newRootCmd()
			cmd.SetArgs([]string{name, "nonsense"})
			cmd.SetOut(&bytes.Buffer{})
			cmd.SetErr(&bytes.Buffer{})
			err := cmd.Execute()
			if err == nil {
				t.Fatalf("gum %s nonsense returned nil; want unknown command/arg error", name)
			}
			if got := err.Error(); !strings.Contains(got, "nonsense") {
				t.Fatalf("err=%q; want rejected arg in message", got)
			}
		})
	}
}

func TestParentCommandsHelpWithoutArgs(t *testing.T) {
	for _, name := range []string{"catalog", "auth", "plugin", "profile", "config", "cache"} {
		t.Run(name, func(t *testing.T) {
			cmd := newRootCmd()
			var out bytes.Buffer
			cmd.SetArgs([]string{name})
			cmd.SetOut(&out)
			cmd.SetErr(&bytes.Buffer{})
			if err := cmd.Execute(); err != nil {
				t.Fatalf("gum %s = %v, want help success", name, err)
			}
			if !strings.Contains(out.String(), "Usage:") {
				t.Fatalf("gum %s output = %q, want help usage", name, out.String())
			}
		})
	}
}

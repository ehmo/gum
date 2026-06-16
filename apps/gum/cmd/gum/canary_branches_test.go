package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// TestRunCanaryEmptyPluginIDBypassesCobraGate pins the runCanary
// `pluginID == ""` arm. Cobra's MarkFlagRequired blocks the empty
// flag at the cmd.Execute layer, but runCanary is exported in-package
// so library embedders (and the green-team integration harness) can
// call it directly. The defensive empty-string check MUST surface a
// "--plugin is required" error rather than fall through to a NewHost
// call with no pluginID.
func TestRunCanaryEmptyPluginIDBypassesCobraGate(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})

	err := runCanary(cmd, "", false)
	if err == nil {
		t.Fatal("want '--plugin is required' error; got nil")
	}
	if !strings.Contains(err.Error(), "--plugin is required") {
		t.Errorf("err=%v; want '--plugin is required' wrap", err)
	}
}

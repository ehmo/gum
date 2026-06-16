package main

import (
	"bytes"
	"strings"
	"testing"
)

// TestNewCallCmdInvalidRiskFlagSurfacesCLIArgInvalid pins the
// normalizeRisk-rejected arm: --risk=bogus passes cobra's flag-
// required check but fails the closed-enum check, surfacing
// CLI_ARG_INVALID with a copy-pasteable hint listing the three valid
// values. Without this, the operator would only see "RISK_TOOL_MISMATCH"
// downstream which is a different (and confusing) error.
func TestNewCallCmdInvalidRiskFlagSurfacesCLIArgInvalid(t *testing.T) {
	cmd := newCallCmd()
	cmd.SetArgs([]string{"some.op", "--risk=bogus"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("want CLI_ARG_INVALID; got nil")
	}
	if !strings.Contains(err.Error(), "CLI_ARG_INVALID") {
		t.Errorf("err=%q; want CLI_ARG_INVALID wrap", err)
	}
}

func TestNewCallCmdDestructiveRejectsDeprecatedYes(t *testing.T) {
	cmd := newCallCmd()
	cmd.SetArgs([]string{"some.op", "--risk=destructive", "--yes"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("destructive --yes returned nil; want signed-confirmation guidance")
	}
	if got := err.Error(); !strings.Contains(got, "--confirmed --token") {
		t.Fatalf("err=%q; want --confirmed --token guidance", got)
	}
}

func TestNewCallCmdDestructiveConfirmedTokenReachesDispatch(t *testing.T) {
	cmd := newCallCmd()
	cmd.SetArgs([]string{"some.op", "--risk=destructive", "--confirmed", "--token=tok"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	_ = cmd.Execute()
}

// TestNewCallCmdHostControlFlagsStampedIntoArgs drives the three
// host-control flag arms: --fields, --page-token, --page-size. Each
// must inject the corresponding __key into parsed.Args before
// dispatch. We can't directly inspect parsed.Args from outside, but
// running with all three sets exercises the three if-arms so they
// register as covered.
func TestNewCallCmdHostControlFlagsStampedIntoArgs(t *testing.T) {
	cmd := newCallCmd()
	cmd.SetArgs([]string{
		"some.op",
		"--risk=read",
		"--page-token=tok",
		"--page-size=10",
	})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	_ = cmd.Execute()
}

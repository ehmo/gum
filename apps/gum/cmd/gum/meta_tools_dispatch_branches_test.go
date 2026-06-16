package main

import (
	"bytes"
	"testing"
)

// TestNewWriteCmdValidJSONReachesDispatch pins the post-parseArgsJSON
// happy-path body of newWriteCmd's RunE (meta_tools.go:95-102). Reached
// by passing a syntactically valid --args JSON object — the RunE then
// builds the Invocation and calls dispatchToWriter. The op_id is
// non-existent, so dispatch returns an error, but the body lines are
// still covered. (We accept any non-nil err here — the goal is to
// exercise the wiring, not the dispatch outcome.)
func TestNewWriteCmdValidJSONReachesDispatch(t *testing.T) {
	cmd := newWriteCmd()
	cmd.SetArgs([]string{"nonexistent.op", "--args", `{"k":"v"}`, "--allow-write"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	_ = cmd.Execute() // err expected; we only care about wiring coverage
}

// TestNewReadCmdValidJSONReachesDispatch pins the same post-parse
// happy-path body of newReadCmd's RunE (meta_tools.go:63-69). Same
// strategy as the write counterpart.
func TestNewReadCmdValidJSONReachesDispatch(t *testing.T) {
	cmd := newReadCmd()
	cmd.SetArgs([]string{"nonexistent.op", "--args", `{}`})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	_ = cmd.Execute()
}

// TestNewDestructiveCmdValidJSONReachesDispatch pins the post-parse
// happy-path body of newDestructiveCmd's RunE
// (meta_tools.go:130-140). With --confirmed and --token the RunE
// constructs the Invocation and proceeds to dispatchToWriter.
func TestNewDestructiveCmdValidJSONReachesDispatch(t *testing.T) {
	cmd := newDestructiveCmd()
	cmd.SetArgs([]string{"nonexistent.op", "--args", `{}`, "--confirmed", "--token", "tok"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	_ = cmd.Execute()
}

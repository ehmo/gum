package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestAuthManagedStatusNotRegisteredForV1(t *testing.T) {
	root := newRootCmd()
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"auth", "managed-status"})

	err := root.Execute()
	if err == nil {
		t.Fatal("gum auth managed-status succeeded; want unknown command in v1 public CLI")
	}
	if !strings.Contains(err.Error(), "unknown command") {
		t.Fatalf("err=%v; want unknown command", err)
	}
}

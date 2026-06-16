package main

import (
	"bytes"
	"testing"

	"github.com/ehmo/gum/internal/dispatch"
)

func TestCodeCmdUsesCodeDispatcherFactory(t *testing.T) {
	origMeta := newMetaToolDispatcher
	origCode := newCodeToolDispatcher
	t.Cleanup(func() {
		newMetaToolDispatcher = origMeta
		newCodeToolDispatcher = origCode
	})

	newMetaToolDispatcher = func(string) dispatch.Dispatcher {
		t.Fatal("gum code must not use the eager meta-tool dispatcher")
		return nil
	}
	newCodeToolDispatcher = func(string) dispatch.Dispatcher {
		return bodyDispatcher{body: "3"}
	}

	cmd := newCodeCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"gum_print(1 + 2)"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if out.String() != "3\n" {
		t.Fatalf("stdout = %q, want %q", out.String(), "3\n")
	}
}

func TestCodeCmdAcceptsOutputFlag(t *testing.T) {
	origCode := newCodeToolDispatcher
	t.Cleanup(func() { newCodeToolDispatcher = origCode })
	newCodeToolDispatcher = func(string) dispatch.Dispatcher {
		return bodyDispatcher{body: "1"}
	}

	cmd := newCodeCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"gum_print(1)", "--output", "json"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute with --output json: %v", err)
	}
	if out.String() != "1\n" {
		t.Fatalf("stdout = %q, want %q", out.String(), "1\n")
	}
}

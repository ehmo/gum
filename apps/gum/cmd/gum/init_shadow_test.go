package main

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestWarnIfShadowedOnPath covers the PATH-collision detector that makes the
// README's charmbracelet-conflict claim true (review gum-zpm3).
func TestWarnIfShadowedOnPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("PATH executable resolution differs on Windows")
	}

	t.Run("no gum on PATH is silent", func(t *testing.T) {
		t.Setenv("PATH", t.TempDir()) // empty dir → LookPath("gum") fails
		var buf bytes.Buffer
		warnIfShadowedOnPath(&buf)
		if buf.Len() != 0 {
			t.Errorf("expected no warning when gum is absent from PATH; got %q", buf.String())
		}
	})

	t.Run("different gum on PATH warns", func(t *testing.T) {
		dir := t.TempDir()
		fake := filepath.Join(dir, "gum")
		if err := os.WriteFile(fake, []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		t.Setenv("PATH", dir)
		var buf bytes.Buffer
		warnIfShadowedOnPath(&buf)
		if !strings.Contains(buf.String(), "first `gum` on your PATH") {
			t.Errorf("expected shadow warning; got %q", buf.String())
		}
	})
}

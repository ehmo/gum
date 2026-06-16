package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestProfileValidateReadFileError pins the
// `os.ReadFile err → "read profile:"` arm. A nonexistent path MUST
// surface a clear "read profile:" wrap so CI operators can distinguish
// "I gave the wrong path" from "the DSL parser rejected the contents."
func TestProfileValidateReadFileError(t *testing.T) {
	tmp := t.TempDir()
	missing := filepath.Join(tmp, "does", "not", "exist.gum")

	_, err := runCLI(t, "profile", "validate", missing)
	if err == nil {
		t.Fatal("want ReadFile err; got nil")
	}
	if !strings.Contains(err.Error(), "read profile") {
		t.Errorf("err=%v; want 'read profile' wrap", err)
	}
}

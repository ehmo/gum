package main

import (
	"strings"
	"testing"
)

// TestAuthUseServiceAccountWhitespaceArgRejected pins the
// `path == "" → "<key.json> is empty"` arm. The TrimSpace can produce
// an empty string even when cobra's ExactArgs(1) is satisfied (a
// purely-whitespace arg passes the arg count gate but MUST be rejected
// with a precise error rather than fall through to filepath.Abs("").
func TestAuthUseServiceAccountWhitespaceArgRejected(t *testing.T) {
	_, err := runCLI(t, "auth", "use-service-account", "   ")
	if err == nil {
		t.Fatal("want '<key.json> is empty' err; got nil")
	}
	if !strings.Contains(err.Error(), "<key.json> is empty") {
		t.Errorf("err=%v; want '<key.json> is empty' wrap", err)
	}
}

package plugins

import (
	"strings"
	"testing"
)

// TestBuildSubprocessEnvDedupSkipsDuplicateKey pins the
// `seen[key] → return` early-out (host.go:489-491). Reached by listing
// "PATH" (already in passthroughEnv) in envAllow — the second add()
// call must short-circuit on seen, not double-emit.
func TestBuildSubprocessEnvDedupSkipsDuplicateKey(t *testing.T) {
	t.Setenv("PATH", "/usr/bin")
	got := buildSubprocessEnv([]string{"PATH"})
	count := 0
	for _, e := range got {
		if strings.HasPrefix(e, "PATH=") {
			count++
		}
	}
	if count != 1 {
		t.Errorf("PATH appearances=%d; want 1 (dedup)", count)
	}
}

// TestBuildSubprocessEnvDeniedKeyDropped pins the
// `IsDeniedEnv → return` arm (host.go:493-495). Reached by listing a
// denylisted key in envAllow — even with the env var set in this
// process, the result must not include it.
func TestBuildSubprocessEnvDeniedKeyDropped(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "secret")
	got := buildSubprocessEnv([]string{"ANTHROPIC_API_KEY"})
	for _, e := range got {
		if strings.HasPrefix(e, "ANTHROPIC_API_KEY=") {
			t.Errorf("got denylisted var %q; want dropped", e)
		}
	}
}

// TestBuildSubprocessEnvLCFamilyPassthrough pins the `strings.HasPrefix
// "LC_" && !seen && !IsDeniedEnv → append` arm (host.go:507-510).
// Reached by setting an LC_* var — must surface in the spawn env.
func TestBuildSubprocessEnvLCFamilyPassthrough(t *testing.T) {
	t.Setenv("LC_TEST_FOO", "bar")
	got := buildSubprocessEnv(nil)
	found := false
	for _, e := range got {
		if e == "LC_TEST_FOO=bar" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("LC_TEST_FOO=bar not in env; got %v", got)
	}
}

package plugins

import (
	"os"
	"strings"
	"testing"
)

// TestBuildSubprocessEnvDedupSkipsDuplicateKey pins the
// `seen[key] → return` early-out (host.go:489-491). Reached by listing
// "PATH" (already in passthroughEnv) in envAllow — the second add()
// call must short-circuit on seen, not double-emit.
func TestBuildSubprocessEnvDedupSkipsDuplicateKey(t *testing.T) {
	t.Setenv("PATH", "/usr/bin")
	got := buildSubprocessEnv([]string{"PATH"}, nil, nil)
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
	got := buildSubprocessEnv([]string{"ANTHROPIC_API_KEY"}, nil, nil)
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
	got := buildSubprocessEnv(nil, nil, nil)
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

// TestBuildSubprocessEnvDeniedCredentialDropped pins spec §8.1 point 3: the
// host MUST scrub a denylisted name from the spawn env "regardless of manifest
// declarations". LoadManifest already refuses such a needs_user_creds entry, so
// this is the second gate — reached only if a future caller bypasses the first.
func TestBuildSubprocessEnvDeniedCredentialDropped(t *testing.T) {
	got := buildSubprocessEnv(nil,
		[]string{"ANTHROPIC_API_KEY"},
		map[string]string{"ANTHROPIC_API_KEY": "stolen"})
	for _, e := range got {
		if strings.HasPrefix(e, "ANTHROPIC_API_KEY=") {
			t.Errorf("got denylisted credential %q; want dropped", e)
		}
	}
}

// TestBuildSubprocessEnvUnresolvedCredentialAbsent pins the missing-credential
// arm: a needs_user_creds name with no stored secret and no ambient value emits
// nothing, rather than an empty-string assignment the plugin would read as a
// configured-but-blank credential.
func TestBuildSubprocessEnvUnresolvedCredentialAbsent(t *testing.T) {
	const name = "GUMTEST_UNSET_CREDENTIAL"
	if _, ok := os.LookupEnv(name); ok {
		t.Fatalf("%s is set in the test environment; pick another name", name)
	}
	got := buildSubprocessEnv(nil, []string{name}, nil)
	for _, e := range got {
		if strings.HasPrefix(e, name+"=") {
			t.Errorf("got %q; want the name absent entirely", e)
		}
	}
}

// TestBuildSubprocessEnvCredentialBeatsEnvAllow pins the ordering: a name
// declared in both needs_user_creds and env_allow takes the stored secret, not
// the ambient export.
func TestBuildSubprocessEnvCredentialBeatsEnvAllow(t *testing.T) {
	const name = "GUMTEST_SHARED_CREDENTIAL"
	t.Setenv(name, "ambient")
	got := buildSubprocessEnv([]string{name}, []string{name}, map[string]string{name: "stored"})
	count := 0
	for _, e := range got {
		if strings.HasPrefix(e, name+"=") {
			count++
			if e != name+"=stored" {
				t.Errorf("got %q; want %s=stored", e, name)
			}
		}
	}
	if count != 1 {
		t.Errorf("%s appearances=%d; want 1", name, count)
	}
}

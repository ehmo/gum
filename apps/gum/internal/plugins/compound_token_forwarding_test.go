package plugins

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// reservedCompoundEnvNames are the two spellings of the spec §7 "Compound auth
// token forwarding" reserved name: the lowercase form a manifest declares and
// the uppercase env var §7 says the host injects at spawn.
var reservedCompoundEnvNames = []string{"google_access_token", "GOOGLE_ACCESS_TOKEN"}

// TestCompoundAuthTokenNotForwarded is the real proof behind the compound-auth
// row of docs/test-matrix.md (gum-7noq). The row used to name
// TestCompoundAuthTokenForwarding, a shim in internal/testmatrix whose whole
// body grepped the repo for an unrelated OAuth refresh test.
//
// v0.1.0 forwards nothing. buildSubprocessEnv is the only producer of a plugin
// spawn environment, and its inputs are the manifest's two name lists plus the
// credentials `gum plugin setup` stored. Neither the host's active access token
// nor any other auth material reaches it, so a manifest that declares the
// reserved name gets an env without it.
func TestCompoundAuthTokenNotForwarded(t *testing.T) {
	for _, name := range reservedCompoundEnvNames {
		if _, ok := os.LookupEnv(name); ok {
			t.Fatalf("%s is set in the test environment; the ambient fallback would mask the assertion", name)
		}
	}

	got := buildSubprocessEnv(nil, []string{"google_access_token"}, nil)

	for _, e := range got {
		for _, name := range reservedCompoundEnvNames {
			if strings.HasPrefix(e, name+"=") {
				t.Errorf("spawn env carries %q; v0.1.0 forwards no access token", e)
			}
		}
	}
}

// TestCompoundAuthTokenOnlyFromStoredCredential records the one path that can
// give the reserved name a value today: a secret the operator typed into
// `gum plugin setup`, which lands in the same creds map as any other plugin
// credential. That value is operator-supplied, not the host's OAuth token, so
// it carries none of the §7 guarantees (no audit entry, no scope list, no
// host-managed lifetime).
func TestCompoundAuthTokenOnlyFromStoredCredential(t *testing.T) {
	const stored = "operator-supplied"
	got := buildSubprocessEnv(nil, []string{"google_access_token"}, map[string]string{"google_access_token": stored})

	want := "google_access_token=" + stored
	found := false
	for _, e := range got {
		if e == want {
			found = true
		}
	}
	if !found {
		t.Errorf("spawn env = %v; want it to carry %q from the stored credential", got, want)
	}
}

// TestNoProductionCodeEmitsPluginTokenForwarded is the tripwire for the other
// half of §7: the audit entry {"event":"plugin_token_forwarded",...} that a
// forwarding host MUST write. No production file mentions it, which is how the
// matrix row can claim the negative. The day forwarding lands, this test fails
// and the row has to be rewritten in the same change.
func TestNoProductionCodeEmitsPluginTokenForwarded(t *testing.T) {
	root := moduleRootDir(t)

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "testdata" || d.Name() == "gen" {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		body, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		if strings.Contains(string(body), "plugin_token_forwarded") {
			rel, _ := filepath.Rel(root, path)
			t.Errorf("%s emits plugin_token_forwarded; compound auth token forwarding is unimplemented in v0.1.0 and the docs/test-matrix.md row says so", rel)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
}

// moduleRootDir walks up from the test's working directory to the go.mod that
// owns it.
func moduleRootDir(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			return dir
		}
		next := filepath.Dir(dir)
		if next == dir {
			t.Fatal("go.mod not found above the test working directory")
		}
		dir = next
	}
}

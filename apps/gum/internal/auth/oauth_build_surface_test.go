package auth

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// oauthInjectionTokens are the strings that would reintroduce a gum-owned
// OAuth client into a build. The env-var names are what a CI job, a HASP
// target, or a build script would reference; the ldflags symbols are what a
// linker flag would set.
var oauthInjectionTokens = []string{
	"GUM_OAUTH_CLIENT_ID",
	"GUM_OAUTH_CLIENT_SECRET",
	"GumOAuthClientID=",
	"GumOAuthClientSecret=",
}

// oauthBuildSurfaces are the paths, relative to a root, that decide what a
// release binary is built with. A file or directory that is absent is skipped,
// so the same list works from the module root and the repository root.
var oauthBuildSurfaces = []string{
	".goreleaser.yaml",
	".goreleaser.yml",
	".hasp.manifest.json",
	".github/workflows",
	"Makefile",
	"install.sh",
	"scripts",
}

// scanOAuthInjection reports every "<path>: <token>" hit under root. Only the
// build surfaces are scanned: documentation is allowed to name these variables
// in order to prohibit them, and internal/pluginenv/denylist.txt has to name
// the secret to deny it.
func scanOAuthInjection(t *testing.T, root string) []string {
	t.Helper()

	hits := []string{}
	for _, rel := range oauthBuildSurfaces {
		target := filepath.Join(root, rel)
		if _, err := os.Stat(target); err != nil {
			continue
		}
		err := filepath.WalkDir(target, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			body, rerr := os.ReadFile(path)
			if rerr != nil {
				return nil
			}
			for _, token := range oauthInjectionTokens {
				if strings.Contains(string(body), token) {
					hits = append(hits, path+": "+token)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", target, err)
		}
	}

	return hits
}

// TestNoManagedOAuthInjectionInBuildSurfaces enforces the v1 BYO-OAuth
// invariant on the build itself, which TestAuthHappyPathNoUserClientSecret does
// not cover: that test scans .go files for a committed secret literal, while a
// secret can also reach the binary through GoReleaser ldflags, a workflow env
// block, a HASP target, or a build script, with nothing committed. Path B is
// withdrawn (spec §7), so no build surface may name either variable.
func TestNoManagedOAuthInjectionInBuildSurfaces(t *testing.T) {
	t.Run("repository build surfaces are clean", func(t *testing.T) {
		moduleRoot := findRepoRootForTest(t)
		roots := []string{moduleRoot}
		if outer := findGitRoot(moduleRoot); outer != "" {
			roots = append(roots, outer)
		}

		for _, root := range roots {
			if hits := scanOAuthInjection(t, root); len(hits) > 0 {
				t.Errorf("an OAuth client credential reached a build surface:\n%s",
					strings.Join(hits, "\n"))
			}
		}
	})

	t.Run("scanner detects an injected secret", func(t *testing.T) {
		root := t.TempDir()
		if err := os.MkdirAll(filepath.Join(root, ".github", "workflows"), 0o755); err != nil {
			t.Fatal(err)
		}
		planted := filepath.Join(root, ".github", "workflows", "release.yml")
		body := "env:\n  GUM_OAUTH_CLIENT_SECRET: ${{ secrets.GUM_OAUTH_CLIENT_SECRET }}\n"
		if err := os.WriteFile(planted, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}

		hits := scanOAuthInjection(t, root)
		if len(hits) == 0 {
			t.Fatal("scanner found nothing in a workflow that injects the secret")
		}
		if !strings.Contains(hits[0], "GUM_OAUTH_CLIENT_SECRET") {
			t.Errorf("hit = %q; want the offending token named", hits[0])
		}
	})
}

// findGitRoot walks up from start to the first directory holding .git and
// returns "" when there is none, so the test still runs from a source drop
// that has no repository around it.
func findGitRoot(start string) string {
	dir := start
	for i := 0; i < 10; i++ {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
	return ""
}

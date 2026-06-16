package pluginenv_test

// Red-team failing tests for gum-b5b: PluginEnvDenylist single-source invariant.
//
// Spec anchors:
//   - §8.1 "needs_user_creds denylist enforcement (normative)"
//   - §14 pluginenv row: "The denylist data lives in a single source-of-truth file
//     internal/pluginenv/denylist.txt … TestPluginEnvDenylistSingleSource MUST hash
//     both embedding sites and fail if they diverge."
//   - test-matrix.md row 167: TestPluginEnvDenylistSingleSource
//   - test-matrix.md row 102: TestPluginEnvProhibited / TestPluginEnvExactDenylist

import (
	"crypto/sha256"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	// These imports will fail until Green team creates the package exports.
	"github.com/ehmo/gum/internal/pluginenv"
)

// repoRoot walks up from the test binary's source file to find the go.mod root.
func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	// file is …/apps/gum/internal/pluginenv/denylist_test.go
	// go two levels up from internal/pluginenv to apps/gum
	dir := filepath.Dir(file)          // …/pluginenv
	dir = filepath.Dir(dir)            // …/internal
	dir = filepath.Dir(dir)            // …/apps/gum
	return dir
}

// parseDenylistBytes splits a denylist text file into trimmed, non-empty, non-comment lines.
func parseDenylistBytes(b []byte) []string {
	var entries []string
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		entries = append(entries, line)
	}
	return entries
}

// ---------------------------------------------------------------------------
// TestPluginEnvDenylistEmbeddedFilePresent
// ---------------------------------------------------------------------------

// TestPluginEnvDenylistEmbeddedFilePresent verifies that the canonical denylist
// source file exists on disk at internal/pluginenv/denylist.txt and is non-empty.
//
// Spec §14 pluginenv row: "The denylist data lives in a single source-of-truth
// file internal/pluginenv/denylist.txt".
func TestPluginEnvDenylistEmbeddedFilePresent(t *testing.T) {
	root := repoRoot(t)
	path := filepath.Join(root, "internal", "pluginenv", "denylist.txt")

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("denylist.txt not found at %s: %v", path, err)
	}
	if info.Size() == 0 {
		t.Fatalf("denylist.txt at %s is empty", path)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("cannot read denylist.txt: %v", err)
	}
	entries := parseDenylistBytes(raw)
	if len(entries) == 0 {
		t.Fatal("denylist.txt has no non-comment, non-blank entries")
	}
	t.Logf("denylist.txt has %d entries", len(entries))
}

// ---------------------------------------------------------------------------
// TestPluginEnvDenylistSingleSource
// ---------------------------------------------------------------------------

// TestPluginEnvDenylistSingleSource verifies the single-source invariant from
// spec §14 pluginenv row:
//
//  1. The embedded denylist material in internal/pluginenv matches the on-disk file.
//  2. cmd/gen-catalog embeds the SAME file (same SHA-256) via its own go:embed directive.
//  3. The exported PluginEnvDenylist slice has at least 10 entries.
//  4. Entries are sorted and deduplicated.
func TestPluginEnvDenylistSingleSource(t *testing.T) {
	root := repoRoot(t)

	// 1. On-disk canonical file.
	diskPath := filepath.Join(root, "internal", "pluginenv", "denylist.txt")
	diskRaw, err := os.ReadFile(diskPath)
	if err != nil {
		t.Fatalf("cannot read canonical denylist.txt: %v", err)
	}
	diskHash := sha256.Sum256(diskRaw)

	// 2. The package's embedded material (via PluginEnvDenylistRaw, the exported
	//    embed bytes — Green team must export this for testability, or we hash
	//    the parsed list against the disk file deterministically).
	//
	//    The exported var is pluginenv.DenylistRaw []byte (go:embed denylist.txt).
	embeddedRaw := pluginenv.DenylistRaw
	embeddedHash := sha256.Sum256(embeddedRaw)

	if diskHash != embeddedHash {
		t.Errorf("SHA-256 mismatch: on-disk denylist.txt (%x) != pluginenv.DenylistRaw (%x)",
			diskHash, embeddedHash)
	}

	// 3. cmd/gen-catalog must embed the same file.
	//    Spec §14: "cmd/gen-catalog embeds the same file directly via its own
	//    go:embed ../../internal/pluginenv/denylist.txt directive".
	//    Green team must export gencatalog.DenylistRaw (or equivalent).
	//    For now we test by hashing what gen-catalog's source references
	//    at the known relative path from cmd/gen-catalog.
	genCatalogEmbed := filepath.Join(root, "cmd", "gen-catalog", "denylist.txt")
	gcRaw, err := os.ReadFile(genCatalogEmbed)
	if err != nil {
		// Accept a symlink or a go:embed of the relative path; verify the file
		// exists in the gen-catalog directory (either as a copy or symlink).
		t.Errorf("cmd/gen-catalog/denylist.txt not found (must be a copy or symlink of "+
			"internal/pluginenv/denylist.txt for go:embed; spec §14): %v", err)
	} else {
		gcHash := sha256.Sum256(gcRaw)
		if diskHash != gcHash {
			t.Errorf("SHA-256 mismatch: pluginenv/denylist.txt (%x) != cmd/gen-catalog/denylist.txt (%x); "+
				"these must be identical per spec §14 single-source invariant", diskHash, gcHash)
		}
	}

	// 4. PluginEnvDenylist must have at least 10 entries.
	const minEntries = 10
	list := pluginenv.PluginEnvDenylist
	if len(list) < minEntries {
		t.Errorf("PluginEnvDenylist has %d entries; want >= %d", len(list), minEntries)
	}

	// 5. Entries must be sorted and deduplicated.
	sorted := make([]string, len(list))
	copy(sorted, list)
	sort.Strings(sorted)

	for i, got := range list {
		if got != sorted[i] {
			t.Errorf("PluginEnvDenylist is not sorted: element %d is %q but sorted order has %q; "+
				"list must be sorted for deterministic comparison", i, got, sorted[i])
			break
		}
	}

	seen := map[string]int{}
	for _, entry := range list {
		seen[entry]++
	}
	for entry, count := range seen {
		if count > 1 {
			t.Errorf("PluginEnvDenylist has duplicate entry %q (appears %d times)", entry, count)
		}
	}
}

// ---------------------------------------------------------------------------
// TestPluginEnvDenylistMembers
// ---------------------------------------------------------------------------

// TestPluginEnvDenylistMembers asserts that specific credential-bearing and
// runtime-sensitive env var names are present in PluginEnvDenylist.
//
// Spec §8.1 lists prohibited env names; §14 test-matrix row 102 names
// TestPluginEnvExactDenylist as the enforcement test.
func TestPluginEnvDenylistMembers(t *testing.T) {
	list := pluginenv.PluginEnvDenylist

	// Build a lookup set for exact entries.
	exactSet := make(map[string]bool, len(list))
	for _, e := range list {
		exactSet[e] = true
	}

	// Exact entries required by spec §8.1 (credential-bearing vars).
	requiredExact := []string{
		"GOOGLE_APPLICATION_CREDENTIALS",
		"OPENAI_API_KEY",
		"ANTHROPIC_API_KEY",
		"AWS_ACCESS_KEY_ID",
		"AWS_SECRET_ACCESS_KEY",
		"AWS_SESSION_TOKEN",
		"AZURE_CLIENT_SECRET",
		"GUM_OAUTH_CLIENT_SECRET",
	}
	for _, name := range requiredExact {
		if !exactSet[name] {
			t.Errorf("PluginEnvDenylist missing required entry %q", name)
		}
	}

	// IsDeniedEnv must also return true for each.
	for _, name := range requiredExact {
		if !pluginenv.IsDeniedEnv(name) {
			t.Errorf("IsDeniedEnv(%q) = false; want true", name)
		}
	}

	// Prefix-glob entries: spec §8.1 says GUM_* and _GUM* prefixes are always denied.
	// Verify at least one prefix-pattern entry exists (e.g. "GUM_AUTH_*" or "AWS_*").
	hasPrefixGlob := false
	for _, e := range list {
		if strings.HasSuffix(e, "*") {
			hasPrefixGlob = true
			break
		}
	}
	if !hasPrefixGlob {
		t.Errorf("PluginEnvDenylist has no prefix-glob entries (e.g. AWS_* or GUM_AUTH_*); "+
			"spec §8.1 requires GUM_* and _GUM* to be denied — at minimum a prefix pattern for GUM_AUTH_*")
	}

	// IsDeniedEnv must handle prefix matching for GUM_AUTH_ variables.
	prefixCases := []struct {
		key  string
		want bool
	}{
		{"GUM_AUTH_TOKEN", true},
		{"GUM_AUTH_REFRESH", true},
		{"GUM_OAUTH_CLIENT_SECRET", true},
		{"PATH", false},
		{"FOO", false},
	}
	for _, tc := range prefixCases {
		got := pluginenv.IsDeniedEnv(tc.key)
		if got != tc.want {
			t.Errorf("IsDeniedEnv(%q) = %v; want %v", tc.key, got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// TestPluginEnvDenylistApplied
// ---------------------------------------------------------------------------

// TestPluginEnvDenylistApplied verifies that FilterEnvForPlugin strips all
// denied env vars from an os.Environ()-style slice while preserving safe ones.
//
// Spec §14 pluginenv row: "runtime subprocess env scrubber … import or embed
// the same source of truth."
func TestPluginEnvDenylistApplied(t *testing.T) {
	input := []string{
		"FOO=bar",
		"GOOGLE_APPLICATION_CREDENTIALS=/path/to/key.json",
		"GUM_AUTH_TOKEN=secret-token-xyz",
		"PATH=/usr/bin:/bin",
	}

	got := pluginenv.FilterEnvForPlugin(input)

	// Build result set.
	gotSet := make(map[string]bool, len(got))
	for _, kv := range got {
		gotSet[kv] = true
	}

	// These must survive.
	mustKeep := []string{"FOO=bar", "PATH=/usr/bin:/bin"}
	for _, kv := range mustKeep {
		if !gotSet[kv] {
			t.Errorf("FilterEnvForPlugin stripped %q; it should be kept (not a denied var)", kv)
		}
	}

	// These must be stripped.
	mustStrip := []string{
		"GOOGLE_APPLICATION_CREDENTIALS=/path/to/key.json",
		"GUM_AUTH_TOKEN=secret-token-xyz",
	}
	for _, kv := range mustStrip {
		if gotSet[kv] {
			t.Errorf("FilterEnvForPlugin kept %q; it must be stripped (denied env var)", kv)
		}
	}

	// Length check: exactly the safe entries survive.
	if len(got) != len(mustKeep) {
		t.Errorf("FilterEnvForPlugin returned %d entries; want %d: %v",
			len(got), len(mustKeep), got)
	}
}

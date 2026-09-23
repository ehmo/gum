package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/output/profile"
)

// captureShadowStderr redirects the runtime loader's writer, which has no
// command to take a stderr from, and resets the printed-line set so one test
// does not swallow another's warning.
func captureShadowStderr(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer

	shadowState.mu.Lock()
	prevOut, prevSeen, prevSuppressed := shadowState.out, shadowState.seen, shadowState.suppressed
	shadowState.out, shadowState.seen, shadowState.suppressed = &buf, nil, false
	shadowState.mu.Unlock()

	t.Cleanup(func() {
		shadowState.mu.Lock()
		shadowState.out, shadowState.seen, shadowState.suppressed = prevOut, prevSeen, prevSuppressed
		shadowState.mu.Unlock()
	})
	return &buf
}

// chdirWithProfile writes body to <tmp>/.gum/profiles/<file>, chdirs there, and
// isolates the user-global layer so a real ~/.config/gum/profiles cannot change
// what resolves.
func chdirWithProfile(t *testing.T, file, body string) string {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	root := t.TempDir()
	dir := filepath.Join(root, ".gum", "profiles")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, file), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	return root
}

// builtinLossyProfile picks a builtin profile that warns against an override
// keeping none of its loss-driving fields.
func builtinLossyProfile(t *testing.T) string {
	t.Helper()
	for _, name := range profile.BuiltinNames() {
		bp, ok := profile.BuiltinLookup(name)
		if !ok {
			continue
		}
		if len(profile.DetectShadowing("x", bp, &profile.Profile{Name: name})) > 0 {
			return name
		}
	}
	t.Skip("no builtin profile sets a loss-driving field")
	return ""
}

// TestRuntimeLoaderWarnsOnSameNameShadow pins the §9.2 runtime firing point:
// profileHierarchyLookup is what dispatch resolves through, so a project-local
// file that displaces a catalog-embedded profile warns on a real call, not only
// under `gum profile validate`.
func TestRuntimeLoaderWarnsOnSameNameShadow(t *testing.T) {
	name := builtinLossyProfile(t)
	chdirWithProfile(t, name+".toml", "format = \"toon\"\n")
	buf := captureShadowStderr(t)

	if _, ok := profileHierarchyLookup(name); !ok {
		t.Fatalf("profile %q did not resolve from the project layer", name)
	}

	got := buf.String()
	if !strings.Contains(got, profile.WarnOverrideDisablesLossyStage) {
		t.Fatalf("stderr = %q; want the %s class", got, profile.WarnOverrideDisablesLossyStage)
	}
	if !strings.Contains(got, "removing a lossy-compression stage") {
		t.Errorf("stderr = %q; want the §9.2 wire text", got)
	}
}

// TestRuntimeLoaderSilentOnCatalogResolution pins the negative case: resolving
// straight from the catalog displaces nothing, so it warns about nothing.
func TestRuntimeLoaderSilentOnCatalogResolution(t *testing.T) {
	name := builtinLossyProfile(t)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Chdir(t.TempDir())
	buf := captureShadowStderr(t)

	if _, ok := profileHierarchyLookup(name); !ok {
		t.Fatalf("profile %q did not resolve from the catalog", name)
	}

	if got := buf.String(); got != "" {
		t.Errorf("stderr = %q; want silence for a catalog-embedded resolution", got)
	}
}

// TestRuntimeLoaderWarnsOnBindingShadow pins the binding form at the runtime
// loader: profileOverrideBindings is where dispatch reads the table, so the
// warning has to name the bound op there too.
func TestRuntimeLoaderWarnsOnBindingShadow(t *testing.T) {
	target, _ := shadowableProfile(t)
	chdirWithProfile(t, "local.toml", ""+
		"[output_profiles.\"local_weak\"]\nformat = \"toon\"\n\n"+
		"[override_bindings]\n\""+target+"\" = \"local_weak\"\n")
	buf := captureShadowStderr(t)

	bindings := profileOverrideBindings()
	if bindings[target] != "local_weak" {
		t.Fatalf("bindings = %v; want %q bound to local_weak", bindings, target)
	}

	got := buf.String()
	if !strings.Contains(got, profile.WarnOverrideDisablesLossyStage) {
		t.Fatalf("stderr = %q; want the %s class", got, profile.WarnOverrideDisablesLossyStage)
	}
	if !strings.Contains(got, target) {
		t.Errorf("stderr = %q; want the bound target %q named", got, target)
	}
}

// TestRuntimeLoaderWarnsOncePerLine pins the seen set: a long-lived `gum mcp`
// session resolves the same override on every call, and §9.2 asks for operator
// awareness, not one line per invocation.
func TestRuntimeLoaderWarnsOncePerLine(t *testing.T) {
	name := builtinLossyProfile(t)
	chdirWithProfile(t, name+".toml", "format = \"toon\"\n")
	buf := captureShadowStderr(t)

	for range 3 {
		if _, ok := profileHierarchyLookup(name); !ok {
			t.Fatalf("profile %q did not resolve", name)
		}
	}

	lines := strings.Count(buf.String(), profile.WarnOverrideDisablesLossyStage)
	first := strings.Count(strings.SplitN(buf.String(), "\n", 2)[0], profile.WarnOverrideDisablesLossyStage)
	if first != 1 {
		t.Fatalf("first line = %q; want one warning", strings.SplitN(buf.String(), "\n", 2)[0])
	}
	distinct := map[string]bool{}
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		distinct[line] = true
	}
	if lines != len(distinct) {
		t.Errorf("printed %d warnings but only %d are distinct", lines, len(distinct))
	}
}

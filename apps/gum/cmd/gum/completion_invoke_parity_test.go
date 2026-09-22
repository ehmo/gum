package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCLICompletionInvokeSurfaceParity pins the spec §13 CLI-parity list:
// cobra completions for the invoke surfaces cover op_id, --variant-id,
// --profile, plugin names and help topics, plus the `gum call` boolean output
// flags --json/--toon/--csv/--markdown. It also pins the two reserved flags
// that MUST stay absent in v1: `gum call --format` and `gum code --lang`.
//
// TestCLICompletionsAllShells proves the generated shell scripts exist;
// nothing before this proved the completion candidates those scripts request.
func TestCLICompletionInvokeSurfaceParity(t *testing.T) {
	t.Run("op_id completes on every invoke command", func(t *testing.T) {
		cases := map[string]string{
			"read":        "gmail.users.messages.list",
			"write":       "gmail.users.messages.send",
			"destructive": "gmail.users.messages.trash",
			"describe":    "gmail.users.messages.list",
			"call":        "gmail.users.messages.list",
		}
		for parent, want := range cases {
			if got := complete(t, parent, ""); !contains(got, want) {
				t.Errorf("__complete %s: missing %q among %d candidates", parent, want, len(got))
			}
		}
	})

	t.Run("--variant-id completes the op's variants", func(t *testing.T) {
		got := complete(t, "call", "gmail.users.messages.list", "--variant-id", "")
		if len(got) == 0 {
			t.Fatal("__complete call --variant-id: no candidates")
		}
	})

	t.Run("--profile completes the profiles on disk", func(t *testing.T) {
		cfg := t.TempDir()
		for _, name := range []string{"default", "work", "staging"} {
			dir := filepath.Join(cfg, "gum", name)
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte("\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		// A stray file beside the profile dirs must not become a candidate.
		if err := os.WriteFile(filepath.Join(cfg, "gum", "notes.txt"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Setenv("XDG_CONFIG_HOME", cfg)

		got := complete(t, "call", "--profile", "")
		for _, want := range []string{"default", "work", "staging"} {
			if !contains(got, want) {
				t.Errorf("__complete call --profile: missing %q; got %v", want, got)
			}
		}
		if contains(got, "notes.txt") {
			t.Errorf("__complete call --profile: offered a non-directory %q; got %v", "notes.txt", got)
		}

		if pfx := complete(t, "call", "--profile", "st"); !contains(pfx, "staging") || contains(pfx, "work") {
			t.Errorf("__complete call --profile st: want staging only; got %v", pfx)
		}
	})

	t.Run("plugin names complete on the plugin subcommands", func(t *testing.T) {
		data := t.TempDir()
		writePluginState(t, data, "default", []map[string]any{
			{"name": "acme-search", "status": "active"},
			{"name": "zeta-notes", "status": "active"},
		})
		t.Setenv("XDG_DATA_HOME", data)

		for _, sub := range []string{"remove", "reload", "unquarantine", "setup", "run"} {
			got := complete(t, "plugin", sub, "")
			for _, want := range []string{"acme-search", "zeta-notes"} {
				if !contains(got, want) {
					t.Errorf("__complete plugin %s: missing %q; got %v", sub, want, got)
				}
			}
		}

		if pfx := complete(t, "plugin", "remove", "ac"); !contains(pfx, "acme-search") || contains(pfx, "zeta-notes") {
			t.Errorf("__complete plugin remove ac: want acme-search only; got %v", pfx)
		}
	})

	t.Run("help topics complete", func(t *testing.T) {
		got := complete(t, "help", "")
		for _, want := range []string{"call", "code", "plugin", "auth"} {
			if !contains(got, want) {
				t.Errorf("__complete help: missing topic %q; got %v", want, got)
			}
		}
	})

	t.Run("gum call offers the boolean output flags", func(t *testing.T) {
		// --risk is required, so cobra offers only required flags until it is
		// supplied; the boolean output flags appear on the next completion.
		got := complete(t, "call", "--risk", "read", "-")
		for _, want := range []string{"--json", "--toon", "--csv", "--markdown"} {
			if !contains(got, want) {
				t.Errorf("__complete call -: missing %q; got %v", want, got)
			}
		}
	})

	t.Run("the reserved flags stay absent", func(t *testing.T) {
		callFlags := complete(t, "call", "--risk", "read", "-")
		if contains(callFlags, "--format") {
			t.Error("gum call must not expose --format in v1 (spec §13 CLI parity)")
		}
		codeFlags := complete(t, "code", "-")
		if contains(codeFlags, "--lang") {
			t.Error("gum code must not expose --lang in v1; the v1 flag is --language")
		}
		if !contains(codeFlags, "--language") {
			t.Errorf("gum code should still expose --language; got %v", codeFlags)
		}
	})
}

// complete runs cobra's hidden __complete subcommand and returns the candidate
// values with their descriptions and the trailing directive lines stripped.
func complete(t *testing.T, args ...string) []string {
	t.Helper()
	cmd := newRootCmd()
	cmd.SetArgs(append([]string{"__complete"}, args...))
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("__complete %v: %v\nout=%s", args, err, out.String())
	}

	var vals []string
	for _, line := range strings.Split(out.String(), "\n") {
		if line == "" || strings.HasPrefix(line, ":") || strings.HasPrefix(line, "Completion ended") {
			continue
		}
		vals = append(vals, strings.SplitN(line, "\t", 2)[0])
	}
	return vals
}

func contains(vals []string, want string) bool {
	for _, v := range vals {
		if v == want {
			return true
		}
	}
	return false
}

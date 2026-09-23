package plugins

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/catalog"
)

// reservedCompoundEnvNames are the two spellings of the spec §7 "Compound auth
// token forwarding" reserved name: the lowercase form a manifest declares and
// the uppercase env var §7 says the host injects at spawn.
var reservedCompoundEnvNames = []string{"google_access_token", "GOOGLE_ACCESS_TOKEN"}

// fakeTokenResolver is a GoogleTokenResolver whose answer the test controls.
type fakeTokenResolver struct {
	token GoogleToken
	err   error
	calls int
}

func (f *fakeTokenResolver) ResolveGoogleToken(context.Context) (GoogleToken, error) {
	f.calls++
	return f.token, f.err
}

func requireCleanCompoundEnv(t *testing.T) {
	t.Helper()
	for _, name := range reservedCompoundEnvNames {
		if _, ok := os.LookupEnv(name); ok {
			t.Fatalf("%s is set in the test environment; the ambient fallback would mask the assertion", name)
		}
	}
}

func envValue(env []string, key string) (string, bool) {
	for _, e := range env {
		if v, ok := strings.CutPrefix(e, key+"="); ok {
			return v, true
		}
	}
	return "", false
}

// TestCompoundAuthTokenForwarded is the §7 positive: a manifest that declares
// the reserved lowercase name gets the host's active access token under the
// uppercase env var, and never under the lowercase one.
func TestCompoundAuthTokenForwarded(t *testing.T) {
	requireCleanCompoundEnv(t)
	res := &fakeTokenResolver{token: GoogleToken{
		AccessToken:        "ya29.host-active",
		SubjectFingerprint: "sha256:abc",
		Scopes:             []string{"https://www.googleapis.com/auth/adwords"},
	}}

	env, _, err := buildSubprocessEnv(context.Background(), subprocessEnvInput{
		NeedsUserCreds: []string{"google_access_token"},
		TokenResolver:  res,
	})
	if err != nil {
		t.Fatalf("buildSubprocessEnv: %v", err)
	}

	got, ok := envValue(env, "GOOGLE_ACCESS_TOKEN")
	if !ok {
		t.Fatalf("spawn env = %v; want GOOGLE_ACCESS_TOKEN", env)
	}
	if got != "ya29.host-active" {
		t.Errorf("GOOGLE_ACCESS_TOKEN = %q; want the host's active token", got)
	}
	if _, ok := envValue(env, "google_access_token"); ok {
		t.Error("spawn env carries the lowercase name; §7 injects the uppercase env var only")
	}
	if res.calls != 1 {
		t.Errorf("resolver called %d times; want 1", res.calls)
	}
}

// TestCompoundAuthTokenBeatsStoredCredential pins which value wins. §7 makes the
// reserved name the host's own token; a secret an operator typed into
// `gum plugin setup` under that name must not shadow it, because the plugin
// cannot tell the two apart and the audit entry would claim a scope list the
// stored value never had.
func TestCompoundAuthTokenBeatsStoredCredential(t *testing.T) {
	requireCleanCompoundEnv(t)
	res := &fakeTokenResolver{token: GoogleToken{AccessToken: "ya29.host-active"}}

	env, _, err := buildSubprocessEnv(context.Background(), subprocessEnvInput{
		NeedsUserCreds: []string{"google_access_token"},
		Creds:          map[string]string{"google_access_token": "operator-supplied"},
		TokenResolver:  res,
	})
	if err != nil {
		t.Fatalf("buildSubprocessEnv: %v", err)
	}

	if got, _ := envValue(env, "GOOGLE_ACCESS_TOKEN"); got != "ya29.host-active" {
		t.Errorf("GOOGLE_ACCESS_TOKEN = %q; want the host token to win", got)
	}
	for _, e := range env {
		if strings.Contains(e, "operator-supplied") {
			t.Errorf("spawn env carries %q; the stored secret must not reach the reserved name", e)
		}
	}
}

// TestCompoundAuthTokenAbsentWithoutResolver covers the host that has no OAuth
// session. The reserved name then carries nothing at all: neither the stored
// secret nor an ambient value. A plugin reading GOOGLE_ACCESS_TOKEN is promised
// the host's live token, so an operator-typed string under that name is worse
// than absence -- the plugin would spend it as if it were the real thing.
func TestCompoundAuthTokenAbsentWithoutResolver(t *testing.T) {
	requireCleanCompoundEnv(t)

	env, entry, err := buildSubprocessEnv(context.Background(), subprocessEnvInput{
		NeedsUserCreds: []string{"google_access_token"},
		Creds:          map[string]string{"google_access_token": "operator-supplied"},
	})
	if err != nil {
		t.Fatalf("buildSubprocessEnv: %v", err)
	}

	for _, name := range reservedCompoundEnvNames {
		if v, ok := envValue(env, name); ok {
			t.Errorf("spawn env carries %s=%q; want the reserved name absent", name, v)
		}
	}
	if entry != nil {
		t.Errorf("audit entry = %v; want none when no token is forwarded", entry)
	}
}

// TestCompoundAuthTokenEmptyResultForwardsNothing: a resolver that reports
// success but hands back no token means no active session. Emitting
// GOOGLE_ACCESS_TOKEN= would tell the plugin it was configured, and it would
// send an empty Bearer header instead of reporting a missing credential.
func TestCompoundAuthTokenEmptyResultForwardsNothing(t *testing.T) {
	requireCleanCompoundEnv(t)
	res := &fakeTokenResolver{token: GoogleToken{SubjectFingerprint: "sha256:abc"}}

	env, entry, err := buildSubprocessEnv(context.Background(), subprocessEnvInput{
		NeedsUserCreds: []string{"google_access_token"},
		TokenResolver:  res,
	})
	if err != nil {
		t.Fatalf("buildSubprocessEnv: %v", err)
	}

	for _, name := range reservedCompoundEnvNames {
		if _, ok := envValue(env, name); ok {
			t.Errorf("spawn env carries %s; want it absent", name)
		}
	}
	if entry != nil {
		t.Errorf("audit entry = %v; want none when nothing was forwarded", entry)
	}
}

// TestCompoundAuthTokenIgnoresAmbientValue: the uppercase name is burned at
// spawn whatever the resolver answers, so an operator who exported
// GOOGLE_ACCESS_TOKEN in their shell cannot stand in for the host's own token
// on a machine with no active session.
func TestCompoundAuthTokenIgnoresAmbientValue(t *testing.T) {
	t.Setenv("GOOGLE_ACCESS_TOKEN", "ambient-token")

	env, _, err := buildSubprocessEnv(context.Background(), subprocessEnvInput{
		EnvAllow:       []string{"GOOGLE_ACCESS_TOKEN"},
		NeedsUserCreds: []string{"google_access_token"},
	})
	if err != nil {
		t.Fatalf("buildSubprocessEnv: %v", err)
	}

	if v, ok := envValue(env, "GOOGLE_ACCESS_TOKEN"); ok {
		t.Errorf("GOOGLE_ACCESS_TOKEN = %q; want the ambient value refused", v)
	}
}

// TestCompoundAuthTokenResolverErrorFailsSpawn: a resolver that cannot produce
// the token stops the spawn. Starting the plugin anyway would run it against a
// missing credential and surface as an opaque upstream 401.
func TestCompoundAuthTokenResolverErrorFailsSpawn(t *testing.T) {
	requireCleanCompoundEnv(t)
	res := &fakeTokenResolver{err: errors.New("no active session")}

	_, _, err := buildSubprocessEnv(context.Background(), subprocessEnvInput{
		NeedsUserCreds: []string{"google_access_token"},
		TokenResolver:  res,
	})
	if err == nil {
		t.Fatal("buildSubprocessEnv = nil error; want the resolver failure surfaced")
	}
	if !strings.Contains(err.Error(), "no active session") {
		t.Errorf("err = %v; want it to name the resolver failure", err)
	}
}

// TestCompoundAuthTokenNotForwardedWhenUndeclared: the token reaches only a
// manifest that asked for it. A plugin that never declared the reserved name
// gets no Google credential.
func TestCompoundAuthTokenNotForwardedWhenUndeclared(t *testing.T) {
	requireCleanCompoundEnv(t)
	res := &fakeTokenResolver{token: GoogleToken{AccessToken: "ya29.host-active"}}

	env, entry, err := buildSubprocessEnv(context.Background(), subprocessEnvInput{
		NeedsUserCreds: []string{"SOME_OTHER_SECRET"},
		TokenResolver:  res,
	})
	if err != nil {
		t.Fatalf("buildSubprocessEnv: %v", err)
	}

	for _, name := range reservedCompoundEnvNames {
		if _, ok := envValue(env, name); ok {
			t.Errorf("spawn env carries %s; the manifest never declared it", name)
		}
	}
	if entry != nil {
		t.Errorf("audit entry = %v; want none", entry)
	}
	if res.calls != 0 {
		t.Errorf("resolver called %d times; want 0 when the name is undeclared", res.calls)
	}
}

// TestCompoundAuditEntryShape checks the exact §7 audit record and, more
// importantly, that it carries no token material. The entry names the subject
// and the scopes so an operator can see which account a plugin was handed.
func TestCompoundAuditEntryShape(t *testing.T) {
	requireCleanCompoundEnv(t)
	scopes := []string{"https://www.googleapis.com/auth/adwords"}
	res := &fakeTokenResolver{token: GoogleToken{
		AccessToken:        "ya29.host-active",
		SubjectFingerprint: "sha256:abc",
		Scopes:             scopes,
	}}

	_, entry, err := buildSubprocessEnv(context.Background(), subprocessEnvInput{
		PluginID:       "keyword-planner",
		NeedsUserCreds: []string{"google_access_token"},
		TokenResolver:  res,
	})
	if err != nil {
		t.Fatalf("buildSubprocessEnv: %v", err)
	}
	if entry == nil {
		t.Fatal("audit entry = nil; §7 requires one per forwarded token")
	}

	want := map[string]any{
		"event":               "plugin_token_forwarded",
		"plugin":              "keyword-planner",
		"subject_fingerprint": "sha256:abc",
	}
	for k, v := range want {
		if entry[k] != v {
			t.Errorf("entry[%q] = %v; want %v", k, entry[k], v)
		}
	}
	gotScopes, ok := entry["scopes"].([]string)
	if !ok || len(gotScopes) != 1 || gotScopes[0] != scopes[0] {
		t.Errorf("entry[\"scopes\"] = %v; want %v", entry["scopes"], scopes)
	}
	for k, v := range entry {
		if s, isStr := v.(string); isStr && strings.Contains(s, "ya29.") {
			t.Errorf("entry[%q] leaks the access token", k)
		}
	}
}

// TestCompoundAuditEntryScopesAreACopy: the entry must not alias the resolver's
// slice, or a later token refresh would silently rewrite an audit row that has
// already been appended.
func TestCompoundAuditEntryScopesAreACopy(t *testing.T) {
	requireCleanCompoundEnv(t)
	scopes := []string{"https://www.googleapis.com/auth/adwords"}
	res := &fakeTokenResolver{token: GoogleToken{AccessToken: "t", Scopes: scopes}}

	_, entry, err := buildSubprocessEnv(context.Background(), subprocessEnvInput{
		NeedsUserCreds: []string{"google_access_token"},
		TokenResolver:  res,
	})
	if err != nil {
		t.Fatalf("buildSubprocessEnv: %v", err)
	}

	scopes[0] = "mutated"
	if got := entry["scopes"].([]string); got[0] == "mutated" {
		t.Error("audit entry aliases the caller's scope slice")
	}
}

// TestGoogleAccessTokenRequiresCompoundTool is §7 item 3. A manifest may declare
// the reserved name only when every advertised tool is compound: the env is per
// subprocess, so one non-compound tool in the same process would read a token
// §7 says it must never receive.
func TestGoogleAccessTokenRequiresCompoundTool(t *testing.T) {
	cases := []struct {
		name    string
		tools   []ToolDecl
		wantErr bool
	}{
		{
			name:  "every tool compound",
			tools: []ToolDecl{{AuthStrategy: catalog.AuthStrategyCompound}},
		},
		{
			name: "two compound tools",
			tools: []ToolDecl{
				{AuthStrategy: catalog.AuthStrategyCompound},
				{AuthStrategy: catalog.AuthStrategyCompound},
			},
		},
		{
			name:    "one non-compound tool in the same subprocess",
			tools:   []ToolDecl{{AuthStrategy: catalog.AuthStrategyCompound}, {AuthStrategy: catalog.AuthStrategyGUMOAuth}},
			wantErr: true,
		},
		{
			name:    "undeclared strategy",
			tools:   []ToolDecl{{AuthStrategy: ""}},
			wantErr: true,
		},
		{
			name:    "plugin_managed owns its own auth",
			tools:   []ToolDecl{{AuthStrategy: catalog.AuthStrategyPluginManaged}},
			wantErr: true,
		},
		{
			name:    "no tools at all",
			tools:   nil,
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateCompoundTokenDeclaration("demo-plug",
				[]string{"google_access_token"}, tc.tools)
			if tc.wantErr {
				if !errors.Is(err, ErrPluginEnvProhibited) {
					t.Fatalf("err = %v; want ErrPluginEnvProhibited", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("err = %v; want nil", err)
			}
		})
	}
}

// TestUndeclaredReservedNameIgnoresStrategy: the strategy check applies only to
// a manifest that asked for the token. Every other plugin keeps working with no
// auth_strategy field, which is what keeps manifest_schema_version at 1.
func TestUndeclaredReservedNameIgnoresStrategy(t *testing.T) {
	err := ValidateCompoundTokenDeclaration("demo-plug", []string{"SOME_SECRET"},
		[]ToolDecl{{AuthStrategy: ""}})
	if err != nil {
		t.Fatalf("err = %v; want nil for a plugin that never asked for the token", err)
	}
}

// TestLoadManifestRefusesNonCompoundTokenDeclaration wires the gate to the one
// place every install and every spawn passes through.
func TestLoadManifestRefusesNonCompoundTokenDeclaration(t *testing.T) {
	dir := t.TempDir()
	const manifest = `{
  "manifest_schema_version": 1,
  "plugin_id": "demo-plug",
  "name": "Demo",
  "version": "1.0.0",
  "shape": "mcp-plugin",
  "executable": "bin/demo",
  "advertised_tools": [{"name": "search", "description": "d", "risk_class": "read"}],
  "declared_capabilities": {"network": true, "fs_write_dir": ""},
  "requirements": {"needs_user_creds": ["google_access_token"]}
}`
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadManifest(dir)
	if !errors.Is(err, ErrPluginEnvProhibited) {
		t.Fatalf("LoadManifest = %v; want ErrPluginEnvProhibited", err)
	}
}

// TestLoadManifestAcceptsCompoundTokenDeclaration is the matching positive, so
// the gate above cannot pass by refusing everything.
func TestLoadManifestAcceptsCompoundTokenDeclaration(t *testing.T) {
	dir := t.TempDir()
	const manifest = `{
  "manifest_schema_version": 1,
  "plugin_id": "demo-plug",
  "name": "Demo",
  "version": "1.0.0",
  "shape": "mcp-plugin",
  "executable": "bin/demo",
  "advertised_tools": [{"name": "search", "description": "d", "risk_class": "read", "auth_strategy": "compound"}],
  "declared_capabilities": {"network": true, "fs_write_dir": ""},
  "requirements": {"needs_user_creds": ["google_access_token"]}
}`
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}

	m, err := LoadManifest(dir)
	if err != nil {
		t.Fatalf("LoadManifest = %v; want nil", err)
	}
	if got := m.AdvertisedTools[0].AuthStrategy; got != catalog.AuthStrategyCompound {
		t.Errorf("auth_strategy = %q; want compound", got)
	}
}

// TestLoadManifestRejectsUnknownAuthStrategy keeps the field on the closed §7
// enum instead of letting a manifest invent a strategy name.
func TestLoadManifestRejectsUnknownAuthStrategy(t *testing.T) {
	dir := t.TempDir()
	const manifest = `{
  "manifest_schema_version": 1,
  "plugin_id": "demo-plug",
  "name": "Demo",
  "version": "1.0.0",
  "shape": "mcp-plugin",
  "executable": "bin/demo",
  "advertised_tools": [{"name": "search", "description": "d", "risk_class": "read", "auth_strategy": "workload_identity"}],
  "declared_capabilities": {"network": true, "fs_write_dir": ""}
}`
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := LoadManifest(dir); !errors.Is(err, ErrManifestInvalid) {
		t.Fatalf("LoadManifest = %v; want ErrManifestInvalid", err)
	}
}

// TestExactlyOneProductionSiteEmitsPluginTokenForwarded replaces the original
// tripwire that asserted no production file mentioned the event. The event now
// exists, and the check that matters is that one file owns it: a second emitter
// would mean a second forwarding path that skipped the strategy gate.
func TestExactlyOneProductionSiteEmitsPluginTokenForwarded(t *testing.T) {
	root := moduleRootDir(t)
	var found []string

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
		if strings.Contains(string(body), `"plugin_token_forwarded"`) {
			rel, _ := filepath.Rel(root, path)
			found = append(found, rel)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}

	want := []string{filepath.Join("internal", "plugins", "compound_token.go")}
	if len(found) != 1 || found[0] != want[0] {
		t.Errorf("plugin_token_forwarded emitted from %v; want exactly %v", found, want)
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

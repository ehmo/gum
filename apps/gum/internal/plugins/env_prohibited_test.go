package plugins_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/plugins"
	"github.com/ehmo/gum/internal/plugins/registry"
)

// TestPluginEnvProhibited is the test docs/plugin-contract.md line 71 names: a
// manifest listing GUM_PROFILE in needs_user_creds is rejected with
// PLUGIN_ENV_PROHIBITED and no subprocess is started.
func TestPluginEnvProhibited(t *testing.T) {
	cases := []struct {
		name  string
		needs []string
		allow []string
		want  string
	}{
		{"gum prefix in needs", []string{"GUM_PROFILE"}, nil, "GUM_PROFILE"},
		{"exact denylist entry", []string{"GOOGLE_APPLICATION_CREDENTIALS"}, nil, "GOOGLE_APPLICATION_CREDENTIALS"},
		{"oauth client secret", []string{"GUM_OAUTH_CLIENT_SECRET"}, nil, "GUM_OAUTH_CLIENT_SECRET"},
		{"underscore gum prefix", []string{"_GUM_INTERNAL"}, nil, "_GUM_INTERNAL"},
		{"third party key", []string{"ANTHROPIC_API_KEY"}, nil, "ANTHROPIC_API_KEY"},
		{"env_allow entry", nil, []string{"OPENAI_API_KEY"}, "OPENAI_API_KEY"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := writeEnvManifest(t, tc.needs, tc.allow)
			host := plugins.NewHost(plugins.HostConfig{InstallRoot: t.TempDir()})

			_, err := host.InstallWithRegistry(context.Background(), src, plugins.InstallOptions{
				Registry: registry.New(t.TempDir()),
			})
			if !errors.Is(err, plugins.ErrPluginEnvProhibited) {
				t.Fatalf("install err = %v; want ErrPluginEnvProhibited", err)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %q; want it to name %q", err, tc.want)
			}
			if !strings.Contains(err.Error(), "is a prohibited env var name") {
				t.Errorf("err = %q; want the canonical contract message form", err)
			}
		})
	}
}

// TestPluginEnvAllowsNonDenylistedNames keeps the gate from rejecting the
// declarations spec §8.1 explicitly permits: an ordinary third-party name and
// the reserved compound-auth token component.
func TestPluginEnvAllowsNonDenylistedNames(t *testing.T) {
	for _, needs := range [][]string{{"GCLOUD_PROJECT"}, {"google_access_token"}, {"PLUG_SESSION"}} {
		src := writeEnvManifest(t, needs, nil)
		if err := plugins.ValidatePluginEnvNames("demo-plug", needs, nil); err != nil {
			t.Errorf("ValidatePluginEnvNames(%v) = %v; want nil", needs, err)
		}
		if _, err := plugins.LoadManifest(src); errors.Is(err, plugins.ErrPluginEnvProhibited) {
			t.Errorf("LoadManifest(%v) rejected a permitted name: %v", needs, err)
		}
	}
}

// writeEnvManifest builds a source dir whose manifest declares the given env
// names. It stops at the manifest: the env gate runs before any file copy, so
// no executable is needed.
func writeEnvManifest(t *testing.T, needs, envAllow []string) string {
	t.Helper()
	dir := t.TempDir()
	descs := make([]map[string]any, 0, len(needs))
	for i, env := range needs {
		descs = append(descs, map[string]any{
			"alias":        string(rune('a'+i)) + "_cred",
			"env":          env,
			"kind":         "api_key",
			"display_name": "Test credential",
		})
	}
	m := map[string]any{
		"manifest_schema_version": 1,
		"plugin_id":               "demo-plug",
		"name":                    "Demo",
		"namespace_owner":         "io.example.demo",
		"version":                 "0.1.0",
		"shape":                   "mcp-plugin",
		"executable":              "executable",
		"advertised_tools": []map[string]any{
			{"name": "echo", "description": "echo", "risk_class": "read"},
		},
		"declared_capabilities": map[string]any{"network": false, "env_allow": envAllow},
		"requirements": map[string]any{
			"needs_user_creds":       needs,
			"credential_descriptors": descs,
		},
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), data, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "executable"), []byte("#!/usr/bin/env true\n"), 0o755); err != nil {
		t.Fatalf("write executable: %v", err)
	}
	return dir
}

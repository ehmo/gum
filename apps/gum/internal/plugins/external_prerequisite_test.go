package plugins

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/plugins/registry"
)

// writeManifestWithComponents writes a manifest carrying both credential
// descriptors and §7 prerequisite components.
func writeManifestWithComponents(t *testing.T, installRoot, pluginID string, needs []string, descs []CredentialDescriptor, comps []catalog.AuthComponent) {
	t.Helper()
	pluginDir := filepath.Join(installRoot, pluginID)
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	man := map[string]any{
		"manifest_schema_version": 1,
		"plugin_id":               pluginID,
		"name":                    pluginID,
		"version":                 "0.0.1",
		"shape":                   "mcp-plugin",
		"executable":              "./bin",
		"requirements": map[string]any{
			"needs_user_creds":       needs,
			"credential_descriptors": descs,
			"auth_components":        comps,
		},
	}
	b, err := json.Marshal(man)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "manifest.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestPluginExternalPrerequisiteChecklist pins docs/test-matrix.md row 97:
// `gum plugin setup` displays the prerequisites gum cannot complete as
// checklist items, using the §7 kind and the author's setup hint. Secret
// components stay out of the checklist because setup collects those itself,
// and no raw env var name reaches the output (spec §7).
func TestPluginExternalPrerequisiteChecklist(t *testing.T) {
	installRoot := t.TempDir()
	descs := []CredentialDescriptor{{
		Alias: "devtoken", Env: "PLUG_DEV_TOKEN", Kind: "api_key",
		DisplayName: "Developer token", SetupHint: "copy it from the API Center",
	}}
	comps := []catalog.AuthComponent{
		{Kind: "api_key", Secret: true, SetupHint: "collected below"},
		{Kind: "developer_token", External: true, SetupHint: "apply for standard access in the Ads API Center"},
		{Kind: "billing_enabled", External: true, SetupHint: "enable billing on the Ads account"},
		{Kind: "login_customer_id", External: true, Optional: true, SetupHint: "only for manager accounts"},
	}
	writeManifestWithComponents(t, installRoot, "ads", []string{"PLUG_DEV_TOKEN"}, descs, comps)

	var out bytes.Buffer
	err := SetupCredentials(context.Background(), "ads", SetupOptions{
		Registry:    registry.New(t.TempDir()),
		Profile:     "prof",
		InstallRoot: installRoot,
		Keyring:     newFakeKeyring(),
		In:          strings.NewReader("sekret\n"),
		Out:         &out,
		RunCanary:   func(context.Context, string) error { return nil },
	})
	if err != nil {
		t.Fatalf("SetupCredentials: %v", err)
	}

	got := out.String()
	for _, want := range []string{
		"[ ] developer_token: apply for standard access in the Ads API Center",
		"[ ] billing_enabled: enable billing on the Ads account",
		"[ ] login_customer_id (optional): only for manager accounts",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("setup output missing checklist item %q\ngot:\n%s", want, got)
		}
	}
	if strings.Contains(got, "[ ] api_key") {
		t.Errorf("secret component listed as an external checklist item:\n%s", got)
	}
	if strings.Contains(got, "PLUG_DEV_TOKEN") {
		t.Errorf("raw env var name leaked into setup output:\n%s", got)
	}

	// The checklist precedes the first secret prompt so the user sees every
	// remaining step before typing anything.
	checklist := strings.Index(got, "[ ] developer_token")
	prompt := strings.Index(got, "Enter value for")
	if checklist < 0 || prompt < 0 || checklist > prompt {
		t.Errorf("checklist index %d must precede prompt index %d\ngot:\n%s", checklist, prompt, got)
	}
}

// TestPluginSetupNoExternalComponentsPrintsNoChecklist keeps the existing
// setup output unchanged for plugins that declare only secrets.
func TestPluginSetupNoExternalComponentsPrintsNoChecklist(t *testing.T) {
	installRoot := t.TempDir()
	descs := []CredentialDescriptor{{
		Alias: "session", Env: "PLUG_SESSION", Kind: "session",
		DisplayName: "Session", SetupHint: "see docs",
	}}
	writeManifestWithComponents(t, installRoot, "p", []string{"PLUG_SESSION"}, descs, nil)

	var out bytes.Buffer
	err := SetupCredentials(context.Background(), "p", SetupOptions{
		Registry:    registry.New(t.TempDir()),
		Profile:     "prof",
		InstallRoot: installRoot,
		Keyring:     newFakeKeyring(),
		In:          strings.NewReader("sekret\n"),
		Out:         &out,
		RunCanary:   func(context.Context, string) error { return nil },
	})
	if err != nil {
		t.Fatalf("SetupCredentials: %v", err)
	}
	if strings.Contains(out.String(), "Prerequisites gum cannot complete") {
		t.Errorf("checklist header printed with no components declared:\n%s", out.String())
	}
}

// TestPluginSetupChecklistWithoutSecrets proves a plugin whose only
// prerequisites are external still gets its checklist. The early return for
// an empty descriptor set must not swallow it.
func TestPluginSetupChecklistWithoutSecrets(t *testing.T) {
	installRoot := t.TempDir()
	comps := []catalog.AuthComponent{
		{Kind: "workspace_admin_trust", External: true, SetupHint: "ask a Workspace admin to trust the app"},
	}
	writeManifestWithComponents(t, installRoot, "p", nil, nil, comps)

	var out bytes.Buffer
	err := SetupCredentials(context.Background(), "p", SetupOptions{
		Registry:    registry.New(t.TempDir()),
		Profile:     "prof",
		InstallRoot: installRoot,
		Keyring:     newFakeKeyring(),
		Out:         &out,
		RunCanary:   func(context.Context, string) error { return nil },
	})
	if err != nil {
		t.Fatalf("SetupCredentials: %v", err)
	}
	if !strings.Contains(out.String(), "[ ] workspace_admin_trust: ask a Workspace admin to trust the app") {
		t.Errorf("external-only plugin printed no checklist:\n%s", out.String())
	}
}

// TestManifestRejectsUnknownAuthComponent pins the spec §8.2 gate: an
// invented component kind fails manifest load with AUTH_COMPONENT_UNKNOWN,
// while an `x-` prefixed informational kind is accepted.
func TestManifestRejectsUnknownAuthComponent(t *testing.T) {
	cases := []struct {
		name    string
		kind    string
		wantErr bool
	}{
		{"known kind", "developer_token", false},
		{"informational x- kind", "x-internal-review", false},
		{"invented kind", "definitely_not_a_kind", true},
		{"empty kind", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeManifestWithComponents(t, dir, "p", nil, nil, []catalog.AuthComponent{
				{Kind: catalog.AuthComponentKind(tc.kind), External: true},
			})

			_, err := LoadManifest(filepath.Join(dir, "p"))
			if tc.wantErr {
				if !errors.Is(err, catalog.ErrUnknownAuthComponent) {
					t.Fatalf("LoadManifest err = %v; want AUTH_COMPONENT_UNKNOWN", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("LoadManifest: %v", err)
			}
		})
	}
}

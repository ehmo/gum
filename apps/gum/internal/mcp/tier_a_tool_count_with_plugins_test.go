// docs/test-matrix.md, plugin half. TestTierARosterManifest pins the
// roster on a server built with no plugins on disk; this pins the clause that
// test cannot reach: active plugins present before Server.Run do not grow
// tools/list.
//
// Spec §4.2 is the normative sentence: "tools/list MUST therefore
// remain exactly the 27 Tier A tools plus the two embedded-skill helpers
// skills_list and skills_get (29 entries) even when active plugins are
// installed." §4.1 forbids dynamic Tier B materialization, so the
// roster is closed whatever the profile holds.

package mcp_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"go.uber.org/goleak"

	gummcp "github.com/ehmo/gum/internal/mcp"
)

func TestTierAToolCountWithPlugins(t *testing.T) {
	defer goleak.VerifyNone(t)

	dataHome := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dataHome)
	profileDir := filepath.Join(dataHome, "gum", "default")

	// Seed before NewServer: the row is about plugins that are already active
	// when the server builds its roster, not about a mid-session install.
	pluginNames := []string{"acme-search", "zeta-notes"}
	seedActivePlugins(t, profileDir, pluginNames...)

	ctx, cs, cleanup := connectSeededServer(t)
	defer cleanup()

	var got []string
	for tool, err := range cs.Tools(ctx, nil) {
		if err != nil {
			t.Fatalf("tools/list: %v", err)
		}
		got = append(got, tool.Name)
	}
	slices.Sort(got)

	want := wireToolNames(t)
	if !slices.Equal(got, want) {
		t.Errorf("tools/list with %d active plugins = %d tools; want %d (spec §4.2)\n got=%v\nwant=%v",
			len(pluginNames), len(got), len(want), got, want)
	}
	for _, name := range got {
		if strings.HasPrefix(name, "plug.") {
			t.Errorf("plugin tool %q reached tools/list; spec §4.1 forbids Tier B materialization", name)
		}
		for _, plugin := range pluginNames {
			if strings.Contains(name, plugin) {
				t.Errorf("tool %q names plugin %q", name, plugin)
			}
		}
	}

	// The other half of the row, and what keeps the assertion above from
	// passing vacuously on an empty registry: the same session does see both
	// plugins, through resources rather than through tools.
	res, err := cs.ReadResource(ctx, &sdkmcp.ReadResourceParams{URI: "gum://plugins"})
	if err != nil {
		t.Fatalf("ReadResource(gum://plugins): %v", err)
	}
	if len(res.Contents) != 1 {
		t.Fatalf("gum://plugins Contents=%d; want 1", len(res.Contents))
	}
	body := res.Contents[0].Text
	for _, plugin := range pluginNames {
		if !strings.Contains(body, plugin) {
			t.Errorf("gum://plugins does not list active plugin %q:\n%s", plugin, body)
		}
	}

	// The row's other clause, that an active plugin's variants stay reachable
	// through gum://op/{id} and search, is NOT asserted here: they are not.
	// Nothing merges plugin-catalog.json into the session snapshot, so those
	// reads answer RESOURCE_NOT_FOUND. Tracked under gum-26nz.
}

// connectSeededServer builds a Server against whatever the caller already
// wrote under XDG_DATA_HOME and connects a client over an in-memory
// transport. It differs from connectResourceClient only in that the caller
// owns XDG_DATA_HOME, so plugin files can exist before NewServer runs.
func connectSeededServer(t *testing.T) (context.Context, *sdkmcp.ClientSession, func()) {
	t.Helper()
	srv := gummcp.NewServer(stubDispatcher{})
	srvTransport, clientTransport := sdkmcp.NewInMemoryTransports()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- srv.Run(ctx, srvTransport) }()

	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "row26-client", Version: "0.0.1"}, nil)
	cs, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		cancel()
		t.Fatalf("client.Connect: %v", err)
	}
	cleanup := func() {
		_ = cs.Close()
		cancel()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Error("server.Run did not stop within 2s after cancel")
		}
	}
	return ctx, cs, cleanup
}

// seedActivePlugins writes one active plugin per name, each owning a single
// read-class variant, into the profile registry the server reads.
func seedActivePlugins(t *testing.T, profileDir string, names ...string) {
	t.Helper()
	variants := make([]map[string]any, 0, len(names))
	lockRows := make([]map[string]any, 0, len(names))
	stateRows := make([]map[string]any, 0, len(names))
	for _, name := range names {
		opID := "plug." + name + ".do_thing"
		variants = append(variants, map[string]any{
			"variant_id":   opID + ".v1",
			"op_id":        opID,
			"owner_plugin": name,
			"risk_class":   "read",
			"binding": map[string]any{
				"binding_schema_version": 1,
				"adapter_key":            "plugin.mcp",
				"operation_key":          opID,
				"plugin_name":            name,
				"tool_name":              "do_thing",
			},
		})
		lockRows = append(lockRows, map[string]any{"name": name, "version": "1.0.0", "shape": "mcp-plugin"})
		stateRows = append(stateRows, map[string]any{"name": name, "status": "active"})
	}
	writePluginRegistryFiles(t, profileDir,
		map[string]any{"plugin_catalog_schema_version": 1, "variants": variants},
		map[string]any{"plugins_lock_schema_version": 1, "plugins": lockRows},
		map[string]any{"plugin_state_schema_version": 1, "plugins": stateRows},
	)
}

// skillHelperTools are the two embedded-skill tools spec §4.2 puts on
// the wire beside the roster. They are outside the §4.1 Tier A roster, so
// docs/tier-a-roster.v1.json does not list them.
var skillHelperTools = []string{"skills_get", "skills_list"}

// wireToolNames returns the sorted 29 names tools/list must carry: the
// docs/tier-a-roster.v1.json union that TestTierARosterManifest pins the bare
// server against, plus the two skill helpers.
func wireToolNames(t *testing.T) []string {
	t.Helper()
	const rosterPath = "../../docs/tier-a-roster.v1.json"
	data, err := os.ReadFile(rosterPath)
	if err != nil {
		t.Fatalf("read %s: %v", rosterPath, err)
	}
	var roster struct {
		MetaTools        []string `json:"meta_tools"`
		ConvenienceTools []string `json:"convenience_tools"`
	}
	if err := json.Unmarshal(data, &roster); err != nil {
		t.Fatalf("parse %s: %v", rosterPath, err)
	}
	names := append(append([]string{}, roster.MetaTools...), roster.ConvenienceTools...)
	if len(names) != 27 {
		t.Fatalf("roster has %d names; want 27 (9 meta + 18 convenience)", len(names))
	}
	names = append(names, skillHelperTools...)
	slices.Sort(names)
	return names
}

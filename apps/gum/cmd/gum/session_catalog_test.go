package main

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/spf13/cobra"

	"github.com/ehmo/gum/internal/dispatch"
	"github.com/ehmo/gum/internal/plugins"
	"github.com/ehmo/gum/internal/plugins/registry"
	profilepkg "github.com/ehmo/gum/internal/profile"
)

// recordingAdapter stands in for the real plugin.mcp executor: it records the
// variant the dispatcher routed to it and returns a fixed body.
type recordingAdapter struct {
	got *dispatch.ResolvedVariant
}

func (a *recordingAdapter) Execute(_ context.Context, _ *dispatch.Invocation,
	rv *dispatch.ResolvedVariant, _ *dispatch.Credentials,
) (*dispatch.Response, error) {
	a.got = rv
	return &dispatch.Response{Body: []byte(`{"ok":true}`), Format: "json", StatusCode: 200}, nil
}

// seedProfilePlugin writes one plugin's catalog row and state row into the
// profile directory XDG_DATA_HOME resolves to, then returns that directory.
func seedProfilePlugin(t *testing.T, profile, plugin, status string) string {
	t.Helper()

	name, err := profilepkg.Parse(profile)
	if err != nil {
		t.Fatalf("parse profile: %v", err)
	}
	dir, err := name.DataDir()
	if err != nil {
		t.Fatalf("data dir: %v", err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir profile: %v", err)
	}

	opID := "plug." + plugin + ".do_thing"
	err = writableRegistry(dir).WriteTransaction(context.Background(), func(f *registry.Files) error {
		f.Catalog.Variants = append(f.Catalog.Variants, map[string]any{
			"variant_id":   opID + ".v1",
			"op_id":        opID,
			"owner_plugin": plugin,
			"risk_class":   "read",
			"binding": map[string]any{
				"binding_schema_version": 1,
				"adapter_key":            "plugin.mcp",
				"operation_key":          opID,
				"plugin_name":            plugin,
				"tool_name":              "do_thing",
			},
		})
		f.State.Plugins = append(f.State.Plugins, map[string]any{
			"name": plugin, "status": status, "quarantined": false,
		})
		return nil
	})
	if err != nil {
		t.Fatalf("seed registry: %v", err)
	}
	return dir
}

// smallEmbeddedCatalog replaces the generated blob for the duration of one
// test so the assertions do not depend on the shipped op set.
func smallEmbeddedCatalog(t *testing.T) {
	t.Helper()

	setCatalogBlob(t, []byte(`{
		"catalog_schema_version": 1,
		"ops": [{
			"op_id": "test.read", "op_schema_version": 1,
			"title": "Read", "summary": "s", "default_variant_id": "test.read.v1",
			"variants": [{
				"variant_id": "test.read.v1", "variant_schema_version": 1,
				"stability": "stable", "interface_kind": "discovery-rest",
				"backend_kind": "discovery-rest", "risk_class": "read"
			}]
		}]
	}`))
}

// rootCmdWithProfile returns a root command whose --profile flag is already
// set, the state PersistentPreRunE sees.
func rootCmdWithProfile(t *testing.T, profile string) *cobra.Command {
	t.Helper()

	root := newRootCmd()
	if err := root.PersistentFlags().Set("profile", profile); err != nil {
		t.Fatalf("set --profile: %v", err)
	}
	return root
}

// TestSessionCatalogDispatchesActivePluginOp is the gum-26nz end-to-end
// proof: after the boot-time merge, `gum call plug.<plugin>.<tool>` resolves
// and reaches the plugin.mcp adapter instead of failing OP_NOT_FOUND.
func TestSessionCatalogDispatchesActivePluginOp(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	smallEmbeddedCatalog(t)
	seedProfilePlugin(t, "e2e", "acme", plugins.StatusActive)

	initSessionCatalog(rootCmdWithProfile(t, "e2e"))

	snapshot := loadCatalog()
	if snapshot == nil {
		t.Fatal("loadCatalog: nil session snapshot")
	}

	adapter := &recordingAdapter{}
	disp := dispatch.NewDispatcherWithConfig(snapshot,
		map[string]dispatch.Adapter{"plugin.mcp": adapter},
		dispatch.DispatcherConfig{})

	shaped, err := disp.Dispatch(context.Background(),
		&dispatch.Invocation{OpID: "plug.acme.do_thing"})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if shaped == nil {
		t.Fatal("Dispatch returned no response")
	}
	if adapter.got == nil {
		t.Fatal("the plugin.mcp adapter was never called")
	}
	if adapter.got.AdapterKey != "plugin.mcp" {
		t.Errorf("adapter_key=%q; want plugin.mcp", adapter.got.AdapterKey)
	}
	b := adapter.got.Variant.Binding
	if b == nil || b.PluginName != "acme" || b.ToolName != "do_thing" {
		t.Fatalf("binding=%+v; want acme/do_thing", b)
	}
}

// TestSessionCatalogPendingPluginNotDispatchable is the other half: a plugin
// still waiting for a restart is not routable in this session.
func TestSessionCatalogPendingPluginNotDispatchable(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	smallEmbeddedCatalog(t)
	seedProfilePlugin(t, "e2e", "acme", plugins.StatusInstalledPendingRestart)

	initSessionCatalog(rootCmdWithProfile(t, "e2e"))

	adapter := &recordingAdapter{}
	disp := dispatch.NewDispatcherWithConfig(loadCatalog(),
		map[string]dispatch.Adapter{"plugin.mcp": adapter},
		dispatch.DispatcherConfig{})

	_, err := disp.Dispatch(context.Background(),
		&dispatch.Invocation{OpID: "plug.acme.do_thing"})
	var se *dispatch.StructuredError
	if !errors.As(err, &se) || se.ErrCode != dispatch.ErrCodeOpNotFound {
		t.Fatalf("err=%v; want OP_NOT_FOUND for a pending-restart plugin op", err)
	}
	if adapter.got != nil {
		t.Error("a pending-restart plugin reached its adapter")
	}
}

// TestSessionCatalogFallsBackToEmbedded covers the no-profile-dir path: the
// snapshot is still published so loadCatalog never returns nil mid-session.
func TestSessionCatalogFallsBackToEmbedded(t *testing.T) {
	smallEmbeddedCatalog(t)

	initSessionCatalog(nil)

	got := loadCatalog()
	if got == nil || len(got.Ops) != 1 || got.Ops[0].OpID != "test.read" {
		t.Fatalf("snapshot=%+v; want the embedded catalog alone", got)
	}
}

// TestSessionCatalogEmptyEmbedPublishesNothing keeps the nil-embed branch
// from storing a nil snapshot, which would make loadCatalog panic.
func TestSessionCatalogEmptyEmbedPublishesNothing(t *testing.T) {
	setCatalogBlob(t, nil)

	initSessionCatalog(nil)

	if got := loadCatalog(); got != nil {
		t.Errorf("loadCatalog()=%+v; want nil on an empty embed", got)
	}
}

// TestSessionCatalogReportsTornGeneration proves a half-published install
// leaves the CLI on the embedded catalog rather than refusing to start.
func TestSessionCatalogReportsTornGeneration(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	smallEmbeddedCatalog(t)
	dir := seedProfilePlugin(t, "e2e", "acme", plugins.StatusActive)

	if err := os.Remove(registry.LockPath(dir)); err != nil {
		t.Fatalf("remove plugins.lock: %v", err)
	}

	initSessionCatalog(rootCmdWithProfile(t, "e2e"))

	got := loadCatalog()
	if got == nil || len(got.Ops) != 1 || got.Ops[0].OpID != "test.read" {
		t.Fatalf("snapshot=%+v; want the embedded catalog alone", got)
	}
}

// compile-time guard: recordingAdapter satisfies the executor interface the
// dispatcher calls.
var _ dispatch.Adapter = (*recordingAdapter)(nil)

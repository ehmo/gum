package plugins_test

import (
	"context"
	"errors"
	"os"
	"testing"

	"go.uber.org/goleak"

	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/plugins"
	"github.com/ehmo/gum/internal/plugins/registry"
)

// sessionBase is a one-op stand-in for the embedded catalog.
func sessionBase() *catalog.Catalog {
	return &catalog.Catalog{
		CatalogSchemaVersion: 1,
		Ops: []catalog.Op{{
			OpID: "gmail.users.messages.list", OpSchemaVersion: 1,
			Title: "List", Summary: "s", DefaultVariantID: "v",
			Variants: []catalog.Variant{{
				VariantID: "v", VariantSchemaVersion: 1,
				Stability: catalog.StabilityStable, InterfaceKind: catalog.InterfaceKindDiscoveryREST,
				BackendKind: catalog.BackendKindDiscoveryREST, RiskClass: catalog.RiskClassRead,
			}},
		}},
	}
}

// seedSessionPlugin publishes one plugin: a plugin-catalog.json variant row
// plus the plugin-state.json row that decides whether it is dispatchable.
// The shapes mirror what install_registry.go writes.
func seedSessionPlugin(t *testing.T, reg *registry.Registry, name, status string, quarantined bool) {
	t.Helper()
	opID := "plug." + name + ".do_thing"
	err := reg.WriteTransaction(context.Background(), func(f *registry.Files) error {
		f.Catalog.Variants = append(f.Catalog.Variants, map[string]any{
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
		f.State.Plugins = append(f.State.Plugins, map[string]any{
			"name":        name,
			"status":      status,
			"quarantined": quarantined,
		})
		return nil
	})
	if err != nil {
		t.Fatalf("seed plugin %s: %v", name, err)
	}
}

func hasOp(c *catalog.Catalog, opID string) bool {
	for i := range c.Ops {
		if c.Ops[i].OpID == opID {
			return true
		}
	}
	return false
}

// TestSessionCatalogMergesActivePlugin is the happy path: an active plugin's
// tool is dispatchable in this session's snapshot.
func TestSessionCatalogMergesActivePlugin(t *testing.T) {
	defer goleak.VerifyNone(t)

	reg := registry.New(t.TempDir())
	seedSessionPlugin(t, reg, "acme", plugins.StatusActive, false)

	got, refused, err := plugins.SessionCatalog(sessionBase(), reg)
	if err != nil {
		t.Fatalf("SessionCatalog: %v", err)
	}
	if len(refused) != 0 {
		t.Fatalf("refused=%v; want none", refused)
	}
	if !hasOp(got, "plug.acme.do_thing") {
		t.Error("active plugin op missing from the session snapshot")
	}
	if !hasOp(got, "gmail.users.messages.list") {
		t.Error("base op dropped from the session snapshot")
	}
}

// TestSessionCatalogExcludesNonActive proves criterion 4 end to end: every
// non-dispatchable status keeps its rows out of the snapshot even though the
// variant rows are present in plugin-catalog.json.
func TestSessionCatalogExcludesNonActive(t *testing.T) {
	defer goleak.VerifyNone(t)

	cases := map[string]struct {
		status      string
		quarantined bool
	}{
		"pending restart":     {plugins.StatusInstalledPendingRestart, false},
		"needs configuration": {plugins.StatusNeedsConfiguration, false},
		"quarantined":         {plugins.StatusActive, true},
		"unknown status":      {"retired", false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			reg := registry.New(t.TempDir())
			seedSessionPlugin(t, reg, "acme", tc.status, tc.quarantined)

			got, refused, err := plugins.SessionCatalog(sessionBase(), reg)
			if err != nil {
				t.Fatalf("SessionCatalog: %v", err)
			}
			if len(refused) != 0 {
				t.Fatalf("refused=%v; want none", refused)
			}
			if hasOp(got, "plug.acme.do_thing") {
				t.Errorf("status %q reached the session snapshot", tc.status)
			}
		})
	}
}

// TestSessionCatalogRefusesTornGeneration pins spec §8.7 step 5: a profile
// whose three files disagree dispatches the built-in catalog alone rather
// than half of an interrupted install.
func TestSessionCatalogRefusesTornGeneration(t *testing.T) {
	defer goleak.VerifyNone(t)

	dir := t.TempDir()
	reg := registry.New(dir)
	seedSessionPlugin(t, reg, "acme", plugins.StatusActive, false)

	// Tear the generation the way a crash between renames does.
	if err := os.Remove(registry.LockPath(dir)); err != nil {
		t.Fatalf("remove plugins.lock: %v", err)
	}

	got, refused, err := plugins.SessionCatalog(sessionBase(), reg)
	if !errors.Is(err, plugins.ErrIncompleteGeneration) {
		t.Fatalf("err=%v; want ErrIncompleteGeneration", err)
	}
	if len(refused) != 0 {
		t.Errorf("refused=%v; want none", refused)
	}
	if got == nil || hasOp(got, "plug.acme.do_thing") || !hasOp(got, "gmail.users.messages.list") {
		t.Error("a torn generation must return the base catalog unchanged")
	}
}

// TestSessionCatalogUnreadableRegistry covers the parse-failure path: the
// profile still boots on the embedded catalog.
func TestSessionCatalogUnreadableRegistry(t *testing.T) {
	defer goleak.VerifyNone(t)

	dir := t.TempDir()
	if err := os.WriteFile(registry.CatalogPath(dir), []byte("{ not json"), 0o600); err != nil {
		t.Fatalf("write catalog: %v", err)
	}
	base := sessionBase()

	got, refused, err := plugins.SessionCatalog(base, registry.New(dir))
	if err == nil {
		t.Fatal("SessionCatalog: want an error for an unparseable plugin-catalog.json")
	}
	if len(refused) != 0 {
		t.Errorf("refused=%v; want none", refused)
	}
	if got != base {
		t.Error("an unreadable registry must return the base catalog unchanged")
	}
}

// TestSessionCatalogSurfacesRefusedRows proves a malformed row is reported
// rather than silently dropped, so the boot log can name it.
func TestSessionCatalogSurfacesRefusedRows(t *testing.T) {
	defer goleak.VerifyNone(t)

	reg := registry.New(t.TempDir())
	err := reg.WriteTransaction(context.Background(), func(f *registry.Files) error {
		f.Catalog.Variants = append(f.Catalog.Variants, map[string]any{
			"variant_id":   "plug.acme.do_thing.v1",
			"op_id":        "plug.acme.do_thing",
			"owner_plugin": "acme",
			"risk_class":   "read",
			// No binding: nothing to route the call to.
		})
		f.State.Plugins = append(f.State.Plugins, map[string]any{
			"name": "acme", "status": plugins.StatusActive, "quarantined": false,
		})
		return nil
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}

	got, refused, err := plugins.SessionCatalog(sessionBase(), reg)
	if err != nil {
		t.Fatalf("SessionCatalog: %v", err)
	}
	if len(refused) != 1 || !errors.Is(refused[0], catalog.ErrPluginRowMalformed) {
		t.Fatalf("refused=%v; want one ErrPluginRowMalformed", refused)
	}
	if hasOp(got, "plug.acme.do_thing") {
		t.Error("malformed row reached the session snapshot")
	}
}

// TestSessionCatalogEmptyProfile covers a profile that never installed a
// plugin: no registry files, no error, snapshot unchanged.
func TestSessionCatalogEmptyProfile(t *testing.T) {
	defer goleak.VerifyNone(t)

	base := sessionBase()
	got, refused, err := plugins.SessionCatalog(base, registry.New(t.TempDir()))
	if err != nil || len(refused) != 0 {
		t.Fatalf("err=%v refused=%v; want neither", err, refused)
	}
	if len(got.Ops) != len(base.Ops) {
		t.Errorf("ops=%d; want %d", len(got.Ops), len(base.Ops))
	}
}

// TestSessionCatalogNilInputs covers the guard clauses.
func TestSessionCatalogNilInputs(t *testing.T) {
	defer goleak.VerifyNone(t)

	if got, _, err := plugins.SessionCatalog(nil, registry.New(t.TempDir())); got != nil || err != nil {
		t.Errorf("nil base: got=%v err=%v; want nil, nil", got, err)
	}
	base := sessionBase()
	if got, _, err := plugins.SessionCatalog(base, nil); got != base || err != nil {
		t.Errorf("nil registry: got=%v err=%v; want base, nil", got, err)
	}
}

// TestActivePluginNamesIgnoresJunkRows keeps a hand-edited plugin-state.json
// from crashing the boot path.
func TestActivePluginNamesIgnoresJunkRows(t *testing.T) {
	defer goleak.VerifyNone(t)

	files := &registry.Files{State: &catalog.PluginState{Plugins: []any{
		"not an object",
		map[string]any{"status": plugins.StatusActive}, // no name
		map[string]any{"name": "acme", "status": plugins.StatusActive},
	}}}
	got := plugins.ActivePluginNames(files)
	if len(got) != 1 || !got["acme"] {
		t.Errorf("active=%v; want only acme", got)
	}
	if plugins.ActivePluginNames(nil) != nil {
		t.Error("nil files: want nil")
	}
	if plugins.ActivePluginNames(&registry.Files{}) != nil {
		t.Error("nil state: want nil")
	}
}

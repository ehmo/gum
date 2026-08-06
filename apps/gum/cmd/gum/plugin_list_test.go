package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ehmo/gum/internal/plugins"
	"github.com/ehmo/gum/internal/plugins/registry"
)

// quarantineStateFile is the gum-mmzr repro: two plugins quarantined with
// PLUGIN_MANIFEST_NOT_FOUND. Neither has a loadable manifest, so Host.List
// skips both install dirs and a manifest-only listing prints nothing.
const quarantineStateFile = `{
  "plugin_state_schema_version": 1,
  "install_generation": 4,
  "install_txid": "76e15842",
  "plugins": [
    {
      "name": "google-trends",
      "quarantined": true,
      "permanent_quarantine": false,
      "last_error_code": "PLUGIN_MANIFEST_NOT_FOUND",
      "quarantined_at": "2026-08-06T05:23:25Z",
      "next_retry_at": "2026-08-06T05:23:55Z",
      "retry_count": 1,
      "backoff_step": 1
    },
    {
      "name": "google-scholar",
      "quarantined": true,
      "permanent_quarantine": false,
      "last_error_code": "PLUGIN_MANIFEST_NOT_FOUND",
      "quarantined_at": "2026-08-02T06:19:21Z",
      "next_retry_at": "2026-08-02T06:19:51Z",
      "retry_count": 1,
      "backoff_step": 1
    }
  ]
}`

// plantProfileDir writes plugin-state.json into a fresh profile dir and returns
// the dir plus a factory that points DispatchPluginCommandWithRegistry at it.
func plantProfileDir(t *testing.T, body string) (string, PluginRegistryFactory) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "plugin-state.json"), []byte(body), 0o600); err != nil {
		t.Fatalf("plant state: %v", err)
	}
	return dir, func(string) *registry.Registry { return registry.New(dir) }
}

// TestPluginListReportsQuarantineWithoutManifests is the whole bead in one
// test: the state file has two quarantined plugins, host.List returns nothing
// because neither manifest loads, and the listing must still name both.
func TestPluginListReportsQuarantineWithoutManifests(t *testing.T) {
	dir, factory := plantProfileDir(t, quarantineStateFile)
	host := &listOnlyHost{manifests: nil}

	out, err := DispatchPluginCommandWithRegistry([]string{"list"}, host, dir, factory)
	if err != nil {
		t.Fatalf("plugin list: %v", err)
	}
	if out == "" {
		t.Fatal("plugin list printed nothing while two plugins are quarantined (gum-mmzr)")
	}
	for _, want := range []string{
		"google-trends",
		"google-scholar",
		"quarantined",
		"PLUGIN_MANIFEST_NOT_FOUND",
		"2026-08-02T06:19:51Z",
		"gum plugin reload",
		"gum plugin unquarantine",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("listing missing %q:\n%s", want, out)
		}
	}
}

// TestPluginListMergesManifestAndState: an installed plugin that is also
// quarantined gets one row, not two, and the row keeps the manifest's version.
func TestPluginListMergesManifestAndState(t *testing.T) {
	dir, factory := plantProfileDir(t, `{
		"plugin_state_schema_version": 1,
		"plugins": [
			{"name": "my-plugin", "quarantined": true, "last_error_code": "PLUGIN_CRASH_LOOP", "retry_count": 3}
		]
	}`)
	host := &listOnlyHost{manifests: []*plugins.Manifest{
		{PluginID: "my-plugin", Name: "My Plugin", Version: "1.2.3", Shape: "mcp-plugin"},
	}}

	out, err := DispatchPluginCommandWithRegistry([]string{"list"}, host, dir, factory)
	if err != nil {
		t.Fatalf("plugin list: %v", err)
	}
	if n := strings.Count(out, "my-plugin"); n != 1 {
		t.Errorf("my-plugin appears %d times; want 1 merged row:\n%s", n, out)
	}
	for _, want := range []string{"1.2.3", "quarantined", "PLUGIN_CRASH_LOOP", "3"} {
		if !strings.Contains(out, want) {
			t.Errorf("listing missing %q:\n%s", want, out)
		}
	}
}

// TestPluginListWithoutProfileDirFallsBackToManifests: `gum plugin list` with
// no resolvable profile has no state file to read. It must still list what the
// manifests say instead of failing.
func TestPluginListWithoutProfileDirFallsBackToManifests(t *testing.T) {
	host := &listOnlyHost{manifests: []*plugins.Manifest{
		{PluginID: "solo", Name: "Solo", Version: "0.1.0", Shape: "mcp-plugin"},
	}}
	out, err := DispatchPluginCommandWithRegistry([]string{"list"}, host, "", nil)
	if err != nil {
		t.Fatalf("plugin list: %v", err)
	}
	if !strings.Contains(out, "solo") || !strings.Contains(out, pluginStatusActive) {
		t.Errorf("listing = %q; want the active manifest row", out)
	}
}

// TestFormatPluginListEmpty: nothing installed and nothing in state returns the
// empty string, so the caller decides what a human sees on an empty listing.
func TestFormatPluginListEmpty(t *testing.T) {
	t.Parallel()
	if got := formatPluginList(nil, nil); got != "" {
		t.Errorf("formatPluginList(nil, nil) = %q; want empty", got)
	}
}

// TestFormatPluginListFooterSingular: the footer counts rows, so it has to
// agree with itself on one plugin.
func TestFormatPluginListFooterSingular(t *testing.T) {
	t.Parallel()
	out := formatPluginList(nil, []plugins.InventoryRow{
		{Name: "one", State: plugins.SupervisorState{Quarantined: true}},
	})
	if !strings.Contains(out, "1 plugin is quarantined") {
		t.Errorf("footer = %q; want the singular form", out)
	}
	if strings.Contains(out, "plugins are quarantined") {
		t.Errorf("footer used the plural for one plugin:\n%s", out)
	}
}

// TestPluginEntryStatusPrecedence pins the one-word verdict per row. A
// permanent quarantine outranks a retryable one, both outrank pending-restart,
// and a state row with no manifest is unloadable rather than active.
func TestPluginEntryStatusPrecedence(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		entry pluginListEntry
		want  string
	}{
		{
			name: "permanent beats retryable",
			entry: pluginListEntry{id: "a", hasManifest: true, state: &plugins.InventoryRow{
				State: plugins.SupervisorState{Quarantined: true, Permanent: true},
			}},
			want: pluginStatusQuarantinedPermanent,
		},
		{
			name: "quarantine beats pending restart",
			entry: pluginListEntry{id: "b", hasManifest: true, state: &plugins.InventoryRow{
				Status: plugins.StatusInstalledPendingRestart,
				State:  plugins.SupervisorState{Quarantined: true},
			}},
			want: pluginStatusQuarantined,
		},
		{
			name: "pending restart",
			entry: pluginListEntry{id: "c", hasManifest: true, state: &plugins.InventoryRow{
				Status: plugins.StatusInstalledPendingRestart,
			}},
			want: pluginStatusPendingRestart,
		},
		{
			name:  "state row with no manifest is unloadable",
			entry: pluginListEntry{id: "d", state: &plugins.InventoryRow{Status: plugins.StatusActive}},
			want:  pluginStatusUnloadable,
		},
		{
			name:  "manifest with no state row is active",
			entry: pluginListEntry{id: "e", hasManifest: true},
			want:  pluginStatusActive,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := pluginEntryStatus(tc.entry); got != tc.want {
				t.Errorf("pluginEntryStatus = %q; want %q", got, tc.want)
			}
		})
	}
}

// TestFormatPluginListDashesEmptyCells: a plugin with no supervisor history
// must not render blank columns that shift the tab alignment.
func TestFormatPluginListDashesEmptyCells(t *testing.T) {
	t.Parallel()
	out := formatPluginList([]*plugins.Manifest{{PluginID: "bare"}}, nil)
	line := ""
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, "bare") {
			line = l
		}
	}
	if line == "" {
		t.Fatalf("no row for bare:\n%s", out)
	}
	if strings.Count(line, "-") < 4 {
		t.Errorf("row = %q; want dashes for version, retries, next-retry, last-error, name", line)
	}
}

// TestFormatPluginListRendersNextRetryUTC: next_retry_at is the field that
// tells an operator whether waiting will fix it, so it must survive rendering
// in a comparable form.
func TestFormatPluginListRendersNextRetryUTC(t *testing.T) {
	t.Parallel()
	when := time.Date(2026, 8, 2, 6, 19, 51, 0, time.UTC)
	out := formatPluginList(nil, []plugins.InventoryRow{
		{Name: "late", State: plugins.SupervisorState{Quarantined: true, NextRetryAt: when}},
	})
	if !strings.Contains(out, "2026-08-02T06:19:51Z") {
		t.Errorf("listing missing the next-retry timestamp:\n%s", out)
	}
}

// listOnlyHost implements the plugin host surface `plugin list` needs and
// panics on anything else, so a routing regression fails loudly.
type listOnlyHost struct {
	manifests []*plugins.Manifest
	err       error
}

func (h *listOnlyHost) List() ([]*plugins.Manifest, error) { return h.manifests, h.err }

func (h *listOnlyHost) Install(context.Context, string) (string, error) {
	panic("Install not expected in a list test")
}

func (h *listOnlyHost) InstallWithRegistry(context.Context, string, plugins.InstallOptions) (string, error) {
	panic("InstallWithRegistry not expected in a list test")
}

func (h *listOnlyHost) Remove(context.Context, string) error {
	panic("Remove not expected in a list test")
}

func (h *listOnlyHost) Start(context.Context, string) (*plugins.Plugin, error) {
	panic("Start not expected in a list test")
}

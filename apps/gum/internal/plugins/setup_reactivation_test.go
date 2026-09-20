// Regression test for gum-davu: a passing post-setup canary reported
// "configured and activated" while the supervisor still refused the plugin.
package plugins

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ehmo/gum/internal/plugins/registry"
)

// setPluginActive wrote status=active and deleted reason, but left
// quarantined, quarantined_at, last_error_code, retry_count, backoff_step and
// permanent_quarantine exactly as the failed attempt had written them.
// Supervisor.Start reads those fields, not status, so the next call refused to
// spawn and told the user to run `gum plugin unquarantine` on a plugin the CLI
// had just called activated.
func TestSetupSuccessClearsQuarantine(t *testing.T) {
	now := time.Date(2026, 9, 19, 10, 0, 0, 0, time.UTC)
	descs := []CredentialDescriptor{{
		Alias: "session", Env: "PLUG_SESSION", Kind: "session",
		DisplayName: "Session", SetupHint: "see docs",
	}}

	for _, tc := range []struct {
		name string
		seed func(*registry.Registry) error
	}{
		{"after a failed setup canary", func(reg *registry.Registry) error {
			return setPluginQuarantinedCANARYFailed(context.Background(), reg, "p", now)
		}},
		{"after a runtime crash inside the backoff window", func(reg *registry.Registry) error {
			_, err := RecordCrash(context.Background(), reg, "p", "PROTOCOL_VIOLATION", now)
			return err
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			installRoot := t.TempDir()
			writeTestManifest(t, installRoot, "p", []string{"PLUG_SESSION"}, descs)
			reg := registry.New(t.TempDir())
			if err := tc.seed(reg); err != nil {
				t.Fatalf("seed: %v", err)
			}

			err := SetupCredentials(context.Background(), "p", SetupOptions{
				Registry:    reg,
				Profile:     "prof",
				InstallRoot: installRoot,
				Keyring:     newFakeKeyring(),
				In:          strings.NewReader("sekret\n"),
				Out:         &bytes.Buffer{},
				RunCanary:   func(context.Context, string) error { return nil },
			})
			if err != nil {
				t.Fatalf("SetupCredentials: %v", err)
			}

			files, err := reg.Load()
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if len(files.State.Plugins) != 1 {
				t.Fatalf("plugin rows = %d; want 1", len(files.State.Plugins))
			}
			row := files.State.Plugins[0].(map[string]any)
			if row["status"] != "active" {
				t.Errorf("status = %v; want active", row["status"])
			}
			if row["quarantined"] != false {
				t.Errorf("quarantined = %v; want false", row["quarantined"])
			}
			if v, has := row["quarantined_at"]; has {
				t.Errorf("quarantined_at = %v; want absent", v)
			}
			if v, has := row["last_error_code"]; has {
				t.Errorf("last_error_code = %v; want absent", v)
			}
			if v, has := row["next_retry_at"]; has {
				t.Errorf("next_retry_at = %v; want absent", v)
			}
			if got := intOf(row["retry_count"]); got != 0 {
				t.Errorf("retry_count = %d; want 0", got)
			}
			if got := intOf(row["backoff_step"]); got != 0 {
				t.Errorf("backoff_step = %d; want 0", got)
			}
			if row["permanent_quarantine"] != false {
				t.Errorf("permanent_quarantine = %v; want false", row["permanent_quarantine"])
			}

			// The consequence the user sees: the very next spawn must reach the
			// spawner instead of returning ErrPluginQuarantined.
			var called int
			sup := NewSupervisor(reg, func(context.Context, string) (*Plugin, error) {
				called++
				return &Plugin{pluginID: "p"}, nil
			}, func() time.Time { return now.Add(time.Second) })
			if _, err := sup.Start(context.Background(), "p"); err != nil {
				t.Fatalf("Supervisor.Start after successful setup: %v", err)
			}
			if called != 1 {
				t.Errorf("spawner called %d time(s); want 1", called)
			}
		})
	}
}

// Regression test for the quarantine-gate defect found in the 2026-09 review.
package plugins

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ehmo/gum/internal/plugins/registry"
)

// supervisor.go: the spawn refusal required either permanent quarantine or a
// future next_retry_at, but the install-time canary
// (setPluginQuarantinedCANARYFailed) writes quarantined=true with no
// next_retry_at at all. Those rows passed the gate, so the next
// `gum call plug.<name>.<tool>` spawned a plugin that had just failed its
// mandatory trust gate. There is no second gate: plugin variants are not
// merged into the dispatch snapshot, so filterQuarantined never sees this
// state.
//
// The canary is now the only writer that can produce this row shape.
// RecordCrash, the runtime quarantine path, always sets next_retry_at or
// Permanent, and both of those arms are covered elsewhere.
func TestSupervisorRefusesQuarantineWithoutRetryTime(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	reg := registry.New(t.TempDir())
	if err := setPluginQuarantinedCANARYFailed(context.Background(), reg, "flights", now); err != nil {
		t.Fatalf("seed: %v", err)
	}

	state, err := ReadSupervisorState(reg, "flights")
	if err != nil {
		t.Fatalf("ReadSupervisorState: %v", err)
	}
	if !state.Quarantined {
		t.Fatal("seed did not record quarantined=true")
	}
	if !state.NextRetryAt.IsZero() {
		t.Fatalf("seed wrote next_retry_at=%s; this test needs the zero case", state.NextRetryAt)
	}
	if state.Permanent {
		t.Fatal("seed marked the row permanent; the Permanent arm is covered elsewhere")
	}

	var called int
	sup := NewSupervisor(reg, func(context.Context, string) (*Plugin, error) {
		called++
		return &Plugin{pluginID: "flights"}, nil
	}, func() time.Time { return now.Add(24 * time.Hour) })

	_, err = sup.Start(context.Background(), "flights")
	if !errors.Is(err, ErrPluginQuarantined) {
		t.Errorf("Start err = %v; want ErrPluginQuarantined", err)
	}
	if called != 0 {
		t.Errorf("spawner called %d time(s); want 0", called)
	}
}

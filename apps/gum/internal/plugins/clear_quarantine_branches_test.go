package plugins_test

import (
	"context"
	"testing"

	"github.com/ehmo/gum/internal/plugins"
	"github.com/ehmo/gum/internal/plugins/registry"
)

// TestClearQuarantineMissingRowIsNoOp pins ClearQuarantine's
// `loop finishes without match → return nil` arm (supervisor.go:134).
// The docstring explicitly promises "missing rows are a no-op", which
// matters because `gum plugin unquarantine` is idempotent — running
// it twice (or against a name the operator typo'd) must NOT error.
func TestClearQuarantineMissingRowIsNoOp(t *testing.T) {
	t.Parallel()
	reg := registry.New(t.TempDir())
	if err := plugins.ClearQuarantine(context.Background(), reg, "nonexistent"); err != nil {
		t.Errorf("ClearQuarantine(missing): %v; want nil (no-op)", err)
	}
}

// TestClearQuarantineSkipsNonMapAndUnrelatedRows pins two continue
// arms in the State.Plugins loop:
//   - non-map raw entry skipped (supervisor.go:118-119)
//   - row whose name doesn't match skipped (supervisor.go:121-122)
//
// We plant the bad row + an unrelated valid row via a direct
// WriteTransaction, then a real matching "target" row. ClearQuarantine
// must walk past both and only zero the target row's supervisor fields.
func TestClearQuarantineSkipsNonMapAndUnrelatedRows(t *testing.T) {
	t.Parallel()
	reg := registry.New(t.TempDir())
	ctx := context.Background()
	err := reg.WriteTransaction(ctx, func(f *registry.Files) error {
		f.State.Plugins = []any{
			"not-a-map", // exercises 118-119 continue
			map[string]any{
				"name":            "other",
				"quarantined":     true,
				"quarantined_at":  "2026-01-01T00:00:00Z",
				"retry_count":     7,
				"backoff_step":    3,
			}, // exercises 121-122 continue (name != "target")
			map[string]any{
				"name":            "target",
				"quarantined":     true,
				"quarantined_at":  "2026-01-01T00:00:00Z",
				"last_error_code": "SERVICE_DOWN",
				"next_retry_at":   "2026-01-01T00:05:00Z",
				"retry_count":     5,
				"backoff_step":    2,
			},
		}
		return nil
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}

	if err := plugins.ClearQuarantine(ctx, reg, "target"); err != nil {
		t.Fatalf("ClearQuarantine: %v", err)
	}

	files, err := reg.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	// Walk by name; ordering may be canonicalised on commit.
	var target, other map[string]any
	for _, raw := range files.State.Plugins {
		row, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		switch row["name"] {
		case "target":
			target = row
		case "other":
			other = row
		}
	}
	if target == nil {
		t.Fatal("target row gone after ClearQuarantine; want kept + reset")
	}
	if q, _ := target["quarantined"].(bool); q {
		t.Errorf("target.quarantined = true; want false (cleared)")
	}
	if _, has := target["quarantined_at"]; has {
		t.Errorf("target.quarantined_at present; want deleted")
	}
	if other == nil {
		t.Fatal("unrelated 'other' row dropped; want untouched")
	}
	if q, _ := other["quarantined"].(bool); !q {
		t.Errorf("other.quarantined mutated to %v; want true (untouched)", q)
	}
}

package main

import (
	"encoding/json"
	"testing"
)

// TestCacheClearNoFlagsReturnsV01Placeholder pins the
// `!bakFlag && !expiredFlag → v0.1.0 placeholder` arm of
// newCacheClearCmd. The bare `gum cache clear` invocation MUST emit
// the legacy "cleared: true" envelope with the process-local no-op
// note — operators relying on the v0.1.0 contract (CI scripts that
// just call `cache clear`) must not see a behavior change.
func TestCacheClearNoFlagsReturnsV01Placeholder(t *testing.T) {
	_ = withTempCacheRootCLI(t)

	out, err := runCLI(t, "cache", "clear")
	if err != nil {
		t.Fatalf("gum cache clear (no flags): %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("stdout not JSON: %v\nstdout=%q", err, out)
	}
	if cleared, _ := got["cleared"].(bool); !cleared {
		t.Errorf("cleared=%v; want true", got["cleared"])
	}
	if note, _ := got["note"].(string); note == "" {
		t.Error("note empty; want the 'process-local; clearing is a no-op' v0.1.0 placeholder string")
	}
}

package mcp

import (
	"testing"
)

// allTierAToolNames is the canonical list of 27 Tier A tools from
// docs/tier-a-roster.v1.json (9 meta + 18 convenience).
var allTierAToolNames = []string{
	// meta tools (9)
	"gum.search_apis",
	"gum.describe_op",
	"gum.read",
	"gum.write",
	"gum.destructive",
	"gum.code",
	"gum.poll",
	"gum.cache_stats",
	"gum.gain",
	// convenience tools (18)
	"gmail_search",
	"gmail_get_message",
	"gmail_send",
	"gmail_create_draft",
	"drive_find",
	"drive_get_file",
	"drive_share",
	"calendar_upcoming",
	"calendar_create_event",
	"calendar_update_event",
	"docs_get",
	"docs_create",
	"sheets_read",
	"sheets_write",
	"slides_get",
	"tasks_list",
	"tasks_create",
	"flights_search",
}

// readClassTools: readOnlyHint=true, destructiveHint=false
// (6 meta + 10 convenience per spec §13 and risk_class mapping)
var readClassTools = []string{
	// meta
	"gum.search_apis",
	"gum.describe_op",
	"gum.read",
	"gum.poll",
	"gum.cache_stats",
	"gum.gain",
	// convenience
	"gmail_search",
	"gmail_get_message",
	"drive_find",
	"drive_get_file",
	"calendar_upcoming",
	"docs_get",
	"sheets_read",
	"slides_get",
	"tasks_list",
	"flights_search",
}

// writeClassTools: readOnlyHint=false, destructiveHint=false
// (gum.write + 8 convenience)
var writeClassTools = []string{
	"gum.write",
	"gmail_send",
	"gmail_create_draft",
	"drive_share",
	"calendar_create_event",
	"calendar_update_event",
	"docs_create",
	"sheets_write",
	"tasks_create",
}

// destructiveStaticTools: readOnlyHint=false, destructiveHint=true
var destructiveStaticTools = []string{
	"gum.destructive",
	"gum.code",
}

// TestTierAAnnotationsHasAll27Tools verifies TierAMetaToolAnnotations returns
// exactly the 27 names from docs/tier-a-roster.v1.json.
func TestTierAAnnotationsHasAll27Tools(t *testing.T) {
	t.Helper()
	anns := TierAMetaToolAnnotations()

	for _, name := range allTierAToolNames {
		if _, ok := anns[name]; !ok {
			t.Errorf("missing tool annotation: %s", name)
		}
	}

	if len(anns) != len(allTierAToolNames) {
		t.Errorf("annotation map has %d entries, want %d", len(anns), len(allTierAToolNames))
	}
}

// TestTierAAnnotationsReadClassValues verifies read-class tools have
// ReadOnlyHint==true and DestructiveHint pointing to false.
func TestTierAAnnotationsReadClassValues(t *testing.T) {
	anns := TierAMetaToolAnnotations()

	for _, name := range readClassTools {
		a, ok := anns[name]
		if !ok {
			t.Errorf("read-class tool missing from map: %s", name)
			continue
		}
		if !a.ReadOnlyHint {
			t.Errorf("tool %s: ReadOnlyHint=false, want true", name)
		}
		if a.DestructiveHint == nil {
			t.Errorf("tool %s: DestructiveHint is nil, want pointer to false", name)
		} else if *a.DestructiveHint != false {
			t.Errorf("tool %s: DestructiveHint=%v, want false", name, *a.DestructiveHint)
		}
	}
}

// TestTierAAnnotationsWriteClassValues verifies write-class tools have
// ReadOnlyHint==false and DestructiveHint pointing to false.
func TestTierAAnnotationsWriteClassValues(t *testing.T) {
	anns := TierAMetaToolAnnotations()

	for _, name := range writeClassTools {
		a, ok := anns[name]
		if !ok {
			t.Errorf("write-class tool missing from map: %s", name)
			continue
		}
		if a.ReadOnlyHint {
			t.Errorf("tool %s: ReadOnlyHint=true, want false", name)
		}
		if a.DestructiveHint == nil {
			t.Errorf("tool %s: DestructiveHint is nil, want pointer to false", name)
		} else if *a.DestructiveHint != false {
			t.Errorf("tool %s: DestructiveHint=%v, want false", name, *a.DestructiveHint)
		}
	}
}

// TestTierAAnnotationsDestructiveStaticValues verifies gum.destructive and
// gum.code have ReadOnlyHint==false and DestructiveHint pointing to true.
func TestTierAAnnotationsDestructiveStaticValues(t *testing.T) {
	anns := TierAMetaToolAnnotations()

	for _, name := range destructiveStaticTools {
		a, ok := anns[name]
		if !ok {
			t.Errorf("destructive-static tool missing from map: %s", name)
			continue
		}
		if a.ReadOnlyHint {
			t.Errorf("tool %s: ReadOnlyHint=true, want false", name)
		}
		if a.DestructiveHint == nil {
			t.Errorf("tool %s: DestructiveHint is nil, want pointer to true", name)
		} else if *a.DestructiveHint != true {
			t.Errorf("tool %s: DestructiveHint=%v, want true", name, *a.DestructiveHint)
		}
	}
}

// TestTierAAnnotationsDestructiveHintAlwaysPointer verifies that all 27 Tier A
// tools have a non-nil DestructiveHint per spec §13 line ~3210:
// "destructiveHint is pointer-backed in v1.6.0 and MUST be set to an explicit
// pointer value (false or true) for every Tier A tool."
func TestTierAAnnotationsDestructiveHintAlwaysPointer(t *testing.T) {
	anns := TierAMetaToolAnnotations()

	for _, name := range allTierAToolNames {
		a, ok := anns[name]
		if !ok {
			t.Errorf("tool missing from annotation map: %s", name)
			continue
		}
		if a.DestructiveHint == nil {
			t.Errorf("tool %s: DestructiveHint is nil; spec §13 requires explicit pointer for every Tier A tool", name)
		}
	}
}

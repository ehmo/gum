package mcp

import (
	"encoding/json"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// boolPtr returns a pointer to a bool value.
func boolPtr(b bool) *bool { return &b }

// writeConfirmationToolSet is the set of convenience tools that require the
// confirmation_passthrough=yes contract (spec.md §4.1).
// Derived at init time from convenienceABITable (single source of truth).
var writeConfirmationToolSet = func() map[string]bool {
	m := make(map[string]bool)
	for name, row := range convenienceABITable {
		if row.ConfirmationPassthrough {
			m[name] = true
		}
	}
	return m
}()

// isWriteConfirmationTool reports whether toolName requires the confirmation gate.
func isWriteConfirmationTool(toolName string) bool {
	return writeConfirmationToolSet[toolName]
}

// TierAMetaToolAnnotations returns static MCP annotations for all 27 Tier A
// tools (9 meta + 18 convenience) per spec §4.1 / §13.
//
// Convenience-tool read/write bucketing is derived from
// convenienceABITable.ConfirmationPassthrough so the ABI table remains the
// single source of truth: ConfirmationPassthrough=true ⇒ write-class
// annotation, false ⇒ read-class. Meta-tool entries stay hardcoded because
// they are not represented in convenienceABITable.
func TierAMetaToolAnnotations() map[string]*sdkmcp.ToolAnnotations {
	readAnn := &sdkmcp.ToolAnnotations{
		ReadOnlyHint:    true,
		DestructiveHint: boolPtr(false),
	}
	writeAnn := &sdkmcp.ToolAnnotations{
		ReadOnlyHint:    false,
		DestructiveHint: boolPtr(false),
	}
	out := map[string]*sdkmcp.ToolAnnotations{
		// --- meta tools (spec §13 / §4.1) ---
		"gum.search_apis": readAnn,
		"gum.read":        readAnn,
		"gum.describe_op": readAnn,
		"gum.poll":        readAnn,
		"gum.cache_stats": readAnn,
		"gum.gain":        readAnn,
		"gum.write":       writeAnn,
		"gum.destructive": {
			ReadOnlyHint:    false,
			DestructiveHint: boolPtr(true),
		},
		"gum.code": {
			ReadOnlyHint:    false,
			DestructiveHint: boolPtr(true),
		},
	}
	for name, row := range convenienceABITable {
		if row.ConfirmationPassthrough {
			out[name] = writeAnn
		} else {
			out[name] = readAnn
		}
	}
	return out
}

// TierAConvenienceToolAnnotations returns annotations for the 4 write
// convenience tools (spec.md §4.1): a filtered subset of TierAMetaToolAnnotations.
func TierAConvenienceToolAnnotations() map[string]*sdkmcp.ToolAnnotations {
	all := TierAMetaToolAnnotations()
	result := make(map[string]*sdkmcp.ToolAnnotations, len(writeConfirmationToolSet))
	for name := range writeConfirmationToolSet {
		result[name] = all[name]
	}
	return result
}

// ToolDef carries a tool's registered name and its JSON Schema.
// Exported so tests can inspect schemas without spinning up a full server.
type ToolDef struct {
	Name   string
	Schema json.RawMessage
}

// tierAConvenienceToolNamesList is the canonical ordered list of 18
// convenience tools per spec.md §4.1 table (snake_case names).
var tierAConvenienceToolNamesList = []string{
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

// TierAConvenienceToolDefs returns all 18 Tier A convenience tool definitions
// with their registered inputSchemas. Does not require a running server.
func TierAConvenienceToolDefs() []ToolDef {
	defs := make([]ToolDef, 0, len(tierAConvenienceToolNamesList))
	for _, name := range tierAConvenienceToolNamesList {
		defs = append(defs, ToolDef{
			Name:   name,
			Schema: convenienceToolSchema(name),
		})
	}
	return defs
}

// MetaToolDefs returns all 9 Tier A meta-tool definitions with their
// registered inputSchemas. Does not require a running server.
func MetaToolDefs() []ToolDef {
	defs := make([]ToolDef, 0, len(metaToolNames))
	for _, name := range metaToolNames {
		defs = append(defs, ToolDef{
			Name:   name,
			Schema: metaToolSchema(name),
		})
	}
	return defs
}

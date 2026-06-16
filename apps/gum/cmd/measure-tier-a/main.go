// Command measure-tier-a measures the cl100k_base token budget for Tier A tools.
//
// Phase 1 skeleton: exposes the Report type and Measure() contract for the green team.
package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/tiktoken-go/tokenizer"
)

func main() {
	os.Exit(0)
}

// Report holds token counts for the Tier A surface.
type Report struct {
	// TotalTokens is the total cl100k_base token count for all Tier A tool definitions.
	TotalTokens int `json:"total_tokens"`

	// PerTool maps each tool name to its individual cl100k_base token count.
	PerTool map[string]int `json:"per_tool"`

	// DefsOverhead is the shared $defs outputSchema overhead in cl100k_base tokens.
	DefsOverhead int `json:"defs_overhead"`

	// FramingReserve is the fixed framing/reserve token budget.
	FramingReserve int `json:"framing_reserve"`
}

// toolDef is the wire-form JSON shape for a Tier A tool.
type toolDef struct {
	Name         string      `json:"name"`
	Description  string      `json:"description"`
	InputSchema  interface{} `json:"inputSchema"`
	OutputSchema interface{} `json:"outputSchema"`
}

// sharedDefs is the $defs block shared across all tools.
var sharedDefs = map[string]interface{}{
	"ToonResult": map[string]interface{}{
		"type":        "object",
		"description": "Compact tabular result (TOON format).",
		"properties": map[string]interface{}{
			"headers":       map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
			"rows":          map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "array"}},
			"omitted_count": map[string]interface{}{"type": "integer"},
		},
	},
	"SingleObjectResult": map[string]interface{}{
		"type":        "object",
		"description": "Single structured object.",
		"properties": map[string]interface{}{
			"data": map[string]interface{}{"type": "object"},
		},
	},
	"RawJsonResult": map[string]interface{}{
		"type":        "object",
		"description": "Raw JSON response.",
		"properties": map[string]interface{}{
			"body": map[string]interface{}{"type": "object"},
		},
	},
}

// ref returns a JSON Schema $ref to a named $defs entry.
func ref(name string) map[string]interface{} {
	return map[string]interface{}{"$ref": "#/$defs/" + name}
}

// strProp returns a compact string property descriptor.
func strProp(desc string) map[string]interface{} {
	return map[string]interface{}{"type": "string", "description": desc}
}

// intProp returns a compact integer property descriptor.
func intProp(desc string) map[string]interface{} {
	return map[string]interface{}{"type": "integer", "description": desc}
}

// boolProp returns a compact boolean property descriptor.
func boolProp(desc string) map[string]interface{} {
	return map[string]interface{}{"type": "boolean", "description": desc}
}

// enumProp returns a string property with a closed enum.
func enumProp(desc string, values []string) map[string]interface{} {
	return map[string]interface{}{"type": "string", "description": desc, "enum": values}
}

// arrayProp returns a compact array property descriptor.
func arrayProp(desc string) map[string]interface{} {
	return map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": desc}
}

// dispatchInputSchema builds the shared dispatch inputSchema for gum.read/write/destructive.
// extraProps are appended to the base dispatch properties.
func dispatchInputSchema(extraProps map[string]interface{}) map[string]interface{} {
	props := map[string]interface{}{
		"op_id":      strProp("Catalog operation ID."),
		"args":       map[string]interface{}{"type": "object", "description": "Operation arguments."},
		"variant_id": strProp("Optional variant override."),
		"fields":     strProp("Comma-separated field mask."),
		"page_size":  intProp("Page size."),
		"page_token": strProp("Pagination token."),
		"format":     enumProp("Output format.", []string{"toon", "csv", "json", "markdown"}),
	}
	for k, v := range extraProps {
		props[k] = v
	}
	return map[string]interface{}{
		"type":       "object",
		"properties": props,
		"required":   []string{"op_id", "args"},
	}
}

// WriteDescription returns the registered description string for the gum.write meta-tool.
// Per spec.md §2: description MUST contain "Note: high-stakes writes may require confirmation"
// and MUST be ≤80 cl100k_base tokens.
func WriteDescription() (string, error) {
	return "Invoke a write-class catalog op. Note: high-stakes writes may require confirmation before dispatch.", nil
}

// tierATools returns the 27 Tier A tool definitions in wire-form order.
// 9 meta-tools followed by 18 convenience tools.
func tierATools() []toolDef {
	writeDesc, _ := WriteDescription()
	confirmProps := map[string]interface{}{
		"confirmed":          boolProp("User confirmed."),
		"confirmation_token": strProp("Confirmation token."),
	}

	metaTools := []toolDef{
		{
			Name:        "gum.search_apis",
			Description: "Hybrid BM25+embedding search over catalog ops. Returns op summaries.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"query": strProp("Search query."),
					"k":     intProp("Max results (default 5)."),
				},
				"required": []string{"query"},
			},
			OutputSchema: ref("ToonResult"),
		},
		{
			Name:        "gum.describe_op",
			Description: "Return compact metadata for one op: variants, risk class, scopes, output profile.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"op_id":      strProp("Catalog operation ID."),
					"variant_id": strProp("Optional variant ID."),
				},
				"required": []string{"op_id"},
			},
			OutputSchema: ref("SingleObjectResult"),
		},
		{
			Name:        "gum.read",
			Description: "Invoke a read-class catalog op. Enforces read risk-gate.",
			InputSchema: dispatchInputSchema(nil),
			OutputSchema: ref("ToonResult"),
		},
		{
			Name:         "gum.write",
			Description:  writeDesc,
			InputSchema:  dispatchInputSchema(confirmProps),
			OutputSchema: ref("SingleObjectResult"),
		},
		{
			Name:        "gum.destructive",
			Description: "Invoke a destructive-class catalog op. Requires confirmation.",
			InputSchema: dispatchInputSchema(confirmProps),
			OutputSchema: ref("SingleObjectResult"),
		},
		{
			Name:        "gum.code",
			Description: "Execute a Risor snippet that may call catalog ops via gum_call.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"language":            enumProp("Sandbox language.", []string{"risor"}),
					"source":              strProp("Snippet source code."),
					"allow_write":         boolProp("Allow write ops (default false)."),
					"allow_destructive":   boolProp("Allow destructive ops (default false)."),
					"destructive_budget":  intProp("Max destructive calls (default 0)."),
					"destructive_scope":   arrayProp("Allowed destructive op_ids."),
					"confirmed":           boolProp("User confirmed."),
					"confirmation_token":  strProp("Confirmation token."),
				},
				"required": []string{"language", "source"},
			},
			OutputSchema: ref("RawJsonResult"),
		},
		{
			Name:        "gum.poll",
			Description: "Poll a long-running operation (LRO) by name.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"operation_name": strProp("LRO name to poll."),
				},
				"required": []string{"operation_name"},
			},
			OutputSchema: ref("SingleObjectResult"),
		},
		{
			Name:        "gum.cache_stats",
			Description: "Return per-profile cache hit/miss stats.",
			InputSchema: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
			OutputSchema: ref("SingleObjectResult"),
		},
		{
			Name:        "gum.gain",
			Description: "Return token-savings analytics from the gain ledger.",
			InputSchema: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
			OutputSchema: ref("SingleObjectResult"),
		},
	}

	// 18 convenience tools.
	convTools := []toolDef{
		{
			Name:        "gmail.search",
			Description: "Search Gmail messages as [id, from, subject, date, snippet].",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"userId":     strProp("User ID or 'me'."),
					"q":          strProp("Gmail search query."),
					"labelIds":   arrayProp("Label IDs filter."),
					"maxResults": intProp("Max results."),
					"pageToken":  strProp("Page token."),
				},
				"required": []string{"userId"},
			},
			OutputSchema: ref("ToonResult"),
		},
		{
			Name:        "gmail.get_message",
			Description: "Fetch one Gmail message, compact by default.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"userId":          strProp("User ID or 'me'."),
					"id":              strProp("Message ID."),
					"format":          strProp("Message format."),
					"metadataHeaders": arrayProp("Headers to include."),
				},
				"required": []string{"userId", "id"},
			},
			OutputSchema: ref("SingleObjectResult"),
		},
		{
			Name:        "gmail.send",
			Description: "Send a message or draft through Gmail.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"userId":             strProp("User ID or 'me'."),
					"message":            map[string]interface{}{"type": "object", "description": "Message resource."},
					"threadId":           strProp("Thread ID."),
					"confirmed":          boolProp("User confirmed."),
					"confirmation_token": strProp("Confirmation token."),
				},
				"required": []string{"userId", "message"},
			},
			OutputSchema: ref("SingleObjectResult"),
		},
		{
			Name:        "gmail.create_draft",
			Description: "Create a Gmail draft without sending.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"userId":             strProp("User ID or 'me'."),
					"message":            map[string]interface{}{"type": "object", "description": "Message resource."},
					"confirmed":          boolProp("User confirmed."),
					"confirmation_token": strProp("Confirmation token."),
				},
				"required": []string{"userId", "message"},
			},
			OutputSchema: ref("SingleObjectResult"),
		},
		{
			Name:        "drive.find",
			Description: "Search Drive files as [id, name, mimeType, size, modifiedTime, parents].",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"q":         strProp("Drive search query."),
					"pageSize":  intProp("Page size."),
					"pageToken": strProp("Page token."),
					"corpora":   strProp("Corpora scope."),
					"driveId":   strProp("Shared drive ID."),
				},
			},
			OutputSchema: ref("ToonResult"),
		},
		{
			Name:        "drive.get_file",
			Description: "Fetch metadata and supported text export for one Drive file.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"fileId":   strProp("File ID."),
					"alt":      strProp("Response format."),
					"mimeType": strProp("Export MIME type."),
					"fields":   strProp("Field mask."),
				},
				"required": []string{"fileId"},
			},
			OutputSchema: ref("SingleObjectResult"),
		},
		{
			Name:        "drive.share",
			Description: "Add a Drive permission.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"fileId":                  strProp("File ID."),
					"permission":              map[string]interface{}{"type": "object", "description": "Permission resource."},
					"sendNotificationEmail":   boolProp("Send notification email."),
					"emailMessage":            strProp("Email message."),
					"confirmed":               boolProp("User confirmed."),
					"confirmation_token":      strProp("Confirmation token."),
				},
				"required": []string{"fileId", "permission"},
			},
			OutputSchema: ref("SingleObjectResult"),
		},
		{
			Name:        "calendar.upcoming",
			Description: "List upcoming events in a time window.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"calendarId": strProp("Calendar ID."),
					"timeMin":    strProp("Start time (RFC3339)."),
					"timeMax":    strProp("End time (RFC3339)."),
					"q":          strProp("Search query."),
					"maxResults": intProp("Max results."),
					"pageToken":  strProp("Page token."),
				},
				"required": []string{"calendarId"},
			},
			OutputSchema: ref("ToonResult"),
		},
		{
			Name:        "calendar.create_event",
			Description: "Create a Calendar event.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"calendarId":         strProp("Calendar ID."),
					"event":              map[string]interface{}{"type": "object", "description": "Event resource."},
					"sendUpdates":        strProp("Who to notify."),
					"confirmed":          boolProp("User confirmed."),
					"confirmation_token": strProp("Confirmation token."),
				},
				"required": []string{"calendarId", "event"},
			},
			OutputSchema: ref("SingleObjectResult"),
		},
		{
			Name:        "calendar.update_event",
			Description: "Update a Calendar event.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"calendarId":         strProp("Calendar ID."),
					"eventId":            strProp("Event ID."),
					"event":              map[string]interface{}{"type": "object", "description": "Event resource."},
					"sendUpdates":        strProp("Who to notify."),
					"confirmed":          boolProp("User confirmed."),
					"confirmation_token": strProp("Confirmation token."),
				},
				"required": []string{"calendarId", "eventId", "event"},
			},
			OutputSchema: ref("SingleObjectResult"),
		},
		{
			Name:        "docs.get",
			Description: "Fetch a Google Doc as markdown.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"documentId":          strProp("Document ID."),
					"suggestionsViewMode": strProp("Suggestions view mode."),
					"includeTabsContent":  boolProp("Include tabs content."),
				},
				"required": []string{"documentId"},
			},
			OutputSchema: ref("SingleObjectResult"),
		},
		{
			Name:        "docs.create",
			Description: "Create a Google Doc.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"document":           map[string]interface{}{"type": "object", "description": "Document resource."},
					"confirmed":          boolProp("User confirmed."),
					"confirmation_token": strProp("Confirmation token."),
				},
				"required": []string{"document"},
			},
			OutputSchema: ref("SingleObjectResult"),
		},
		{
			Name:        "sheets.read",
			Description: "Read a Sheets range.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"spreadsheetId":        strProp("Spreadsheet ID."),
					"range":                strProp("A1 notation range."),
					"majorDimension":       strProp("ROWS or COLUMNS."),
					"valueRenderOption":    strProp("Value render option."),
					"dateTimeRenderOption": strProp("DateTime render option."),
				},
				"required": []string{"spreadsheetId", "range"},
			},
			OutputSchema: ref("ToonResult"),
		},
		{
			Name:        "sheets.write",
			Description: "Write values to a Sheets range.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"spreadsheetId":          strProp("Spreadsheet ID."),
					"range":                  strProp("A1 notation range."),
					"values":                 map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "array"}, "description": "Row data."},
					"valueInputOption":       strProp("Value input option."),
					"includeValuesInResponse": boolProp("Include values in response."),
					"confirmed":              boolProp("User confirmed."),
					"confirmation_token":     strProp("Confirmation token."),
				},
				"required": []string{"spreadsheetId", "range", "values"},
			},
			OutputSchema: ref("SingleObjectResult"),
		},
		{
			Name:        "slides.get",
			Description: "Fetch compact Slides presentation metadata and page summaries.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"presentationId": strProp("Presentation ID."),
					"fields":         strProp("Field mask."),
				},
				"required": []string{"presentationId"},
			},
			OutputSchema: ref("SingleObjectResult"),
		},
		{
			Name:        "tasks.list",
			Description: "List Google Tasks.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"tasklist":      strProp("Task list ID."),
					"showCompleted": boolProp("Show completed tasks."),
					"showHidden":    boolProp("Show hidden tasks."),
					"maxResults":    intProp("Max results."),
					"pageToken":     strProp("Page token."),
				},
				"required": []string{"tasklist"},
			},
			OutputSchema: ref("ToonResult"),
		},
		{
			Name:        "tasks.create",
			Description: "Create a Google Task.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"tasklist":           strProp("Task list ID."),
					"task":               map[string]interface{}{"type": "object", "description": "Task resource."},
					"parent":             strProp("Parent task ID."),
					"previous":           strProp("Previous sibling task ID."),
					"confirmed":          boolProp("User confirmed."),
					"confirmation_token": strProp("Confirmation token."),
				},
				"required": []string{"tasklist", "task"},
			},
			OutputSchema: ref("SingleObjectResult"),
		},
		{
			Name:        "flights.search",
			Description: "Search Google Flights via the bundled fli plugin.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"origin":         strProp("Origin airport code."),
					"destination":    strProp("Destination airport code."),
					"departure_date": strProp("Departure date (YYYY-MM-DD)."),
					"return_date":    strProp("Return date (YYYY-MM-DD)."),
					"adults":         intProp("Number of adults."),
					"cabin":          strProp("Cabin class."),
				},
				"required": []string{"origin", "destination", "departure_date"},
			},
			OutputSchema: ref("ToonResult"),
		},
	}

	tools := make([]toolDef, 0, len(metaTools)+len(convTools))
	tools = append(tools, metaTools...)
	tools = append(tools, convTools...)
	return tools
}

// tokenCount serializes v as JSON and counts cl100k_base tokens.
func tokenCount(enc tokenizer.Codec, v interface{}) (int, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return 0, fmt.Errorf("marshal: %w", err)
	}
	ids, _, err := enc.Encode(string(b))
	if err != nil {
		return 0, fmt.Errorf("encode: %w", err)
	}
	return len(ids), nil
}

// Measure measures the Tier A tool definitions and returns a Report.
// It uses the cl100k_base tokenizer (github.com/tiktoken-go/tokenizer) as required by spec.md §2.1.
// No network calls are made; measurement is purely local against the registered tool definitions.
func Measure() (*Report, error) {
	enc, err := tokenizer.Get(tokenizer.Cl100kBase)
	if err != nil {
		return nil, fmt.Errorf("measure-tier-a: get tokenizer: %w", err)
	}

	tools := tierATools()

	perTool := make(map[string]int, len(tools))
	toolsTotal := 0
	for _, t := range tools {
		n, err := tokenCount(enc, t)
		if err != nil {
			return nil, fmt.Errorf("measure-tier-a: count tokens for %s: %w", t.Name, err)
		}
		perTool[t.Name] = n
		toolsTotal += n
	}

	defsOverhead, err := tokenCount(enc, sharedDefs)
	if err != nil {
		return nil, fmt.Errorf("measure-tier-a: count defs tokens: %w", err)
	}

	// Framing: the JSON wrapper {"tools":[...],"$defs":{...}} minus the inner content.
	framing := map[string]interface{}{
		"tools": []interface{}{},
		"$defs": map[string]interface{}{},
	}
	framingTokens, err := tokenCount(enc, framing)
	if err != nil {
		return nil, fmt.Errorf("measure-tier-a: count framing tokens: %w", err)
	}

	total := toolsTotal + defsOverhead + framingTokens

	return &Report{
		TotalTokens:    total,
		PerTool:        perTool,
		DefsOverhead:   defsOverhead,
		FramingReserve: framingTokens,
	}, nil
}

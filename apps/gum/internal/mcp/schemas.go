package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// confirmationProps is the JSON fragment added to every write convenience tool
// schema that requires the confirmation_passthrough=yes contract (spec §4.1).
// It declares `confirmed` and `confirmation_token` as optional fields.
const confirmationProps = `"confirmed":{"type":"boolean"},"confirmation_token":{"type":"string"}`

// writeInvokeBaseProps is the shared 7-param base for gum.write and gum.destructive
// (op_id, args, variant_id, fields, page_size, page_token, format) per spec §4.1.
const writeInvokeBaseProps = `"op_id":{"type":"string"},` +
	`"args":{"type":"object"},` +
	`"variant_id":{"type":"string"},` +
	`"fields":{"type":"string"},` +
	`"page_size":{"type":"integer","minimum":1},` +
	`"page_token":{"type":"string"},` +
	`"format":{"type":"string","enum":["toon","csv","json","markdown"]}`

// writeInvokeSchema builds the 9-param schema shared by gum.write and gum.destructive:
// the 7-param base plus confirmed+confirmation_token, required=["op_id","args"].
func writeInvokeSchema() json.RawMessage {
	return rawSchema(`{
		"type":"object",
		"properties":{` + writeInvokeBaseProps + `,` + confirmationProps + `},
		"required":["op_id","args"],
		"additionalProperties":false
	}`)
}

// withConfirmationFields builds a JSON Schema for a write convenience tool by
// embedding confirmationProps alongside the tool-specific properties.
// baseProps is the comma-separated JSON property definitions for that tool
// (no trailing comma), requiredList is the "required" array JSON (e.g.
// `["to","subject","body"]`).
func withConfirmationFields(baseProps, requiredList string) json.RawMessage {
	return rawSchema(fmt.Sprintf(`{
		"type":"object",
		"properties":{%s,%s},
		"required":%s,
		"additionalProperties":false
	}`, baseProps, confirmationProps, requiredList))
}

// metaToolDescription returns the canonical description for a meta-tool.
// Descriptions are intentionally terse: the Tier A token budget tracked by
// testdata/tier-a-token-baseline.json applies to these strings.
func metaToolDescription(name string) string {
	switch name {
	case "gum.search_apis":
		return "BM25 search over the catalog; returns op_id matches."
	case "gum.describe_op":
		return "Return the catalog entry for an op_id."
	case "gum.read":
		return "Invoke a read-class catalog op."
	case "gum.write":
		return "Invoke a write-class catalog op (allow_write enforced)."
	case "gum.destructive":
		return "Invoke a destructive op with a confirmation_token."
	case "gum.code":
		return "Run a Risor v2 script in the sandbox."
	case "gum.poll":
		return "Poll a long-running operation (v0.2.0)."
	case "gum.cache_stats":
		return "Return the dispatcher cache stats."
	case "gum.gain":
		return "Return cumulative gain-ledger stats."
	}
	return "gum meta-tool"
}

// convenienceToolDescription returns the canonical description for a
// convenience tool. The Tier A token budget applies to these strings too.
func convenienceToolDescription(name string) string {
	switch name {
	case "gmail_search":
		return "List Gmail messages matching a query."
	case "gmail_get_message":
		return "Fetch a Gmail message by id."
	case "gmail_send":
		return "Send a Gmail message."
	case "gmail_create_draft":
		return "Create a Gmail draft."
	case "drive_find":
		return "Find Google Drive files."
	case "drive_get_file":
		return "Fetch a Drive file's metadata."
	case "drive_share":
		return "Share a Drive file with a principal."
	case "calendar_upcoming":
		return "List upcoming Calendar events."
	case "calendar_create_event":
		return "Create a Calendar event."
	case "calendar_update_event":
		return "Update a Calendar event."
	case "docs_get":
		return "Fetch a Google Doc."
	case "docs_create":
		return "Create a Google Doc."
	case "sheets_read":
		return "Read a Sheets range."
	case "sheets_write":
		return "Write to a Sheets range."
	case "slides_get":
		return "Fetch a Slides presentation."
	case "tasks_list":
		return "List Google Tasks."
	case "tasks_create":
		return "Create a Google Task."
	case "flights_search":
		return "Search Google Flights itineraries."
	}
	return "gum convenience tool"
}

// metaToolSchema returns the JSON Schema for a meta-tool's input.
// All schemas set additionalProperties:false per spec.md §4.1 criterion 4.
func metaToolSchema(name string) json.RawMessage {
	switch name {
	case "gum.search_apis":
		// spec §4.1 line 291: gum.search_apis(query, k=5); §2139: k default=5, range 1–20.
		return rawSchema(`{
			"type":"object",
			"properties":{
				"query":{"type":"string"},
				"k":{"type":"integer","default":5,"minimum":1,"maximum":20}
			},
			"required":["query"],
			"additionalProperties":false
		}`)
	case "gum.describe_op":
		return rawSchema(`{
			"type":"object",
			"properties":{"op_id":{"type":"string"}},
			"required":["op_id"],
			"additionalProperties":false
		}`)
	case "gum.read":
		return rawSchema(`{
			"type":"object",
			"properties":{
				"op_id":{"type":"string"},
				"args":{"type":"object"},
				"variant_id":{"type":"string"},
				"fields":{"type":"string"},
				"page_size":{"type":"integer","minimum":1},
				"page_token":{"type":"string"},
				"format":{"type":"string","enum":["toon","csv","json","markdown"]}
			},
			"required":["op_id"],
			"additionalProperties":false
		}`)
	case "gum.write":
		// spec §4.1 / §6.1 — 9-param write schema; confirmed+confirmation_token optional.
		return writeInvokeSchema()
	case "gum.destructive":
		// spec §4.1 line ~284 / §6.1 — 9-param destructive schema; confirmed+confirmation_token optional.
		return writeInvokeSchema()
	case "gum.code":
		// spec.md §4.1 (8-param table) and §6.1 (gum.code semantics).
		// language enum is the v0.1.0 closed set: only "risor".
		// Reserved strings starlark/yaegi/js/python MUST NOT appear here per §6.1;
		// add them only when those runtimes ship.
		return rawSchema(`{
			"type":"object",
			"properties":{
				"language":{"type":"string","enum":["risor"]},
				"source":{"type":"string"},
				"allow_write":{"type":"boolean","default":false},
				"allow_destructive":{"type":"boolean","default":false},
				"destructive_budget":{"type":"integer","default":0,"minimum":0},
				"destructive_scope":{"type":"array","items":{"type":"string"},"default":[]},
				"confirmed":{"type":"boolean","default":false},
				"confirmation_token":{"type":"string"}
			},
			"required":["language","source"],
			"additionalProperties":false
		}`)
	case "gum.poll":
		return rawSchema(`{
			"type":"object",
			"properties":{"operation_name":{"type":"string"}},
			"required":["operation_name"],
			"additionalProperties":false
		}`)
	case "gum.cache_stats":
		return rawSchema(`{"type":"object","properties":{},"additionalProperties":false}`)
	case "gum.gain":
		// The §2793 GainResult envelope is summary-only; by_op aggregation is a
		// CLI-local convenience (`gum gain --by-op`), not part of the MCP
		// contract, so it is not advertised here (review gum-y5wb).
		return rawSchema(`{"type":"object","properties":{},"additionalProperties":false}`)
	}
	return rawSchema(`{"type":"object","additionalProperties":false}`)
}

// convenienceToolSchema returns a per-tool JSON Schema for a convenience tool.
// All schemas set additionalProperties:false per spec.md §4.1 criterion 4.
func convenienceToolSchema(name string) json.RawMessage {
	switch name {
	case "gmail_search":
		return rawSchema(`{
			"type":"object",
			"properties":{
				"query":{"type":"string"},
				"maxResults":{"type":"integer","minimum":1,"maximum":500},
				"labelIds":{"type":"array","items":{"type":"string"}}
			},
			"required":["query"],
			"additionalProperties":false
		}`)
	case "gmail_get_message":
		return rawSchema(`{
			"type":"object",
			"properties":{
				"id":{"type":"string"},
				"format":{"type":"string","enum":["full","minimal","raw","metadata"]}
			},
			"required":["id"],
			"additionalProperties":false
		}`)
	case "gmail_send":
		return withConfirmationFields(
			`"to":{"type":"string"},"subject":{"type":"string"},"body":{"type":"string"},"cc":{"type":"string"},"bcc":{"type":"string"}`,
			`["to","subject","body"]`,
		)
	case "gmail_create_draft":
		return withConfirmationFields(
			`"to":{"type":"string"},"subject":{"type":"string"},"body":{"type":"string"},"cc":{"type":"string"}`,
			`["to","subject","body"]`,
		)
	case "drive_find":
		return rawSchema(`{
			"type":"object",
			"properties":{
				"query":{"type":"string"},
				"pageSize":{"type":"integer","minimum":1,"maximum":1000},
				"orderBy":{"type":"string"}
			},
			"required":["query"],
			"additionalProperties":false
		}`)
	case "drive_get_file":
		return rawSchema(`{
			"type":"object",
			"properties":{
				"fileId":{"type":"string"},
				"fields":{"type":"string"}
			},
			"required":["fileId"],
			"additionalProperties":false
		}`)
	case "drive_share":
		return withConfirmationFields(
			`"fileId":{"type":"string"},"role":{"type":"string","enum":["reader","commenter","writer","owner"]},"type":{"type":"string","enum":["user","group","domain","anyone"]},"emailAddress":{"type":"string"}`,
			`["fileId","role","type"]`,
		)
	case "calendar_upcoming":
		return rawSchema(`{
			"type":"object",
			"properties":{
				"maxResults":{"type":"integer","minimum":1,"maximum":250},
				"timeMin":{"type":"string"},
				"timeMax":{"type":"string"},
				"calendarId":{"type":"string"}
			},
			"required":["maxResults"],
			"additionalProperties":false
		}`)
	case "calendar_create_event":
		return withConfirmationFields(
			`"calendarId":{"type":"string"},"summary":{"type":"string"},"start":{"type":"string"},"end":{"type":"string"},"description":{"type":"string"},"attendees":{"type":"array","items":{"type":"string"}},"sendUpdates":{"type":"string"}`,
			`["calendarId","summary","start","end"]`,
		)
	case "calendar_update_event":
		return withConfirmationFields(
			`"calendarId":{"type":"string"},"eventId":{"type":"string"},"summary":{"type":"string"},"start":{"type":"string"},"end":{"type":"string"},"description":{"type":"string"},"sendUpdates":{"type":"string"}`,
			`["calendarId","eventId"]`,
		)
	case "docs_get":
		return rawSchema(`{
			"type":"object",
			"properties":{
				"documentId":{"type":"string"},
				"suggestionsViewMode":{"type":"string"}
			},
			"required":["documentId"],
			"additionalProperties":false
		}`)
	case "docs_create":
		return withConfirmationFields(
			`"title":{"type":"string"},"body":{"type":"string"}`,
			`["title"]`,
		)
	case "sheets_read":
		return rawSchema(`{
			"type":"object",
			"properties":{
				"spreadsheetId":{"type":"string"},
				"range":{"type":"string"},
				"majorDimension":{"type":"string","enum":["ROWS","COLUMNS"]}
			},
			"required":["spreadsheetId","range"],
			"additionalProperties":false
		}`)
	case "sheets_write":
		return withConfirmationFields(
			`"spreadsheetId":{"type":"string"},"range":{"type":"string"},"values":{"type":"array","items":{"type":"array"}},"valueInputOption":{"type":"string","enum":["RAW","USER_ENTERED"]}`,
			`["spreadsheetId","range","values"]`,
		)
	case "slides_get":
		return rawSchema(`{
			"type":"object",
			"properties":{
				"presentationId":{"type":"string"},
				"fields":{"type":"string"}
			},
			"required":["presentationId"],
			"additionalProperties":false
		}`)
	case "tasks_list":
		return rawSchema(`{
			"type":"object",
			"properties":{
				"tasklist":{"type":"string"},
				"maxResults":{"type":"integer","minimum":1,"maximum":100},
				"showCompleted":{"type":"boolean"}
			},
			"required":["tasklist"],
			"additionalProperties":false
		}`)
	case "tasks_create":
		return withConfirmationFields(
			`"tasklist":{"type":"string"},"title":{"type":"string"},"notes":{"type":"string"},"due":{"type":"string"}`,
			`["tasklist","title"]`,
		)
	case "flights_search":
		return rawSchema(`{
			"type":"object",
			"properties":{
				"origin":{"type":"string"},
				"destination":{"type":"string"},
				"departureDate":{"type":"string"},
				"returnDate":{"type":"string"},
				"adults":{"type":"integer","minimum":1,"maximum":9}
			},
			"required":["origin","destination","departureDate"],
			"additionalProperties":false
		}`)
	}
	return rawSchema(`{"type":"object","additionalProperties":false}`)
}

// metaToolOutputSchema returns the JSON Schema for a meta-tool's output
// (structuredContent). Every meta-tool MUST register a non-nil outputSchema
// — required by spec §4 and verified by TestTierARegistrationScan.
func metaToolOutputSchema(name string) json.RawMessage {
	switch name {
	case "gum.search_apis":
		return toonResultSchema()
	case "gum.describe_op":
		return singleObjectResultSchema()
	case "gum.read":
		return toonResultSchema()
	case "gum.write":
		return singleObjectResultSchema()
	case "gum.destructive":
		return singleObjectResultSchema()
	case "gum.code":
		return rawJSONResultSchema()
	case "gum.poll":
		return singleObjectResultSchema()
	case "gum.cache_stats":
		return singleObjectResultSchema()
	case "gum.gain":
		return singleObjectResultSchema()
	}
	return singleObjectResultSchema()
}

func skillsListSchema() json.RawMessage {
	return rawSchema(`{
		"type":"object",
		"properties":{},
		"additionalProperties":false
	}`)
}

const (
	skillNamePatternForSchema    = "^[a-z0-9-]+$"
	skillVersionPatternForSchema = "^(latest|[0-9]+\\\\.[0-9]+\\\\.[0-9]+)$"
)

func skillsGetSchema() json.RawMessage {
	return rawSchema(`{
		"type":"object",
		"properties":{
			"name":{"type":"string","pattern":"` + skillNamePatternForSchema + `","maxLength":64},
			"version":{"type":"string","pattern":"` + skillVersionPatternForSchema + `","maxLength":32},
			"max_bytes":{"type":"integer","minimum":0}
		},
		"required":["name"],
		"additionalProperties":false
	}`)
}

// convenienceToolOutputSchema returns the JSON Schema for a convenience tool's
// output (structuredContent). Every convenience tool MUST register a non-nil
// outputSchema.
func convenienceToolOutputSchema(name string) json.RawMessage {
	switch name {
	case "gmail_search", "drive_find", "calendar_upcoming",
		"sheets_read", "tasks_list", "flights_search":
		return toonResultSchema()
	}
	return singleObjectResultSchema()
}

// toonResultSchema is the shared output schema for ops that return a TOON
// table (headers/rows). Root type is "object" so go-sdk registration accepts
// it (spec §4 conformance).
func toonResultSchema() json.RawMessage {
	return rawSchema(`{
		"type":"object",
		"$defs":{
			"ToonResult":{
				"type":"object",
				"properties":{
					"headers":{"type":"array","items":{"type":"string"}},
					"rows":{"type":"array","items":{"type":"array"}},
					"omitted_count":{"type":"integer"}
				},
				"required":["headers","rows"]
			}
		},
		"properties":{
			"headers":{"type":"array","items":{"type":"string"}},
			"rows":{"type":"array","items":{"type":"array"}},
			"omitted_count":{"type":"integer"}
		}
	}`)
}

// singleObjectResultSchema is the shared output schema for ops that return a
// single structured object payload.
func singleObjectResultSchema() json.RawMessage {
	return rawSchema(`{
		"type":"object",
		"$defs":{
			"SingleObjectResult":{
				"type":"object",
				"properties":{"data":{"type":"object"}}
			}
		},
		"properties":{"data":{"type":"object"}}
	}`)
}

// rawJSONResultSchema is the output schema for gum.code, which returns the raw
// Risor return value plus optional gum_parallel envelope.
func rawJSONResultSchema() json.RawMessage {
	return rawSchema(`{
		"type":"object",
		"$defs":{
			"RawJsonResult":{
				"type":"object",
				"properties":{"body":{"type":"object"}}
			}
		},
		"properties":{"body":{"type":"object"}}
	}`)
}

// rawSchema strips insignificant whitespace and returns the result as
// json.RawMessage. Inputs are static and known-valid at build time.
func rawSchema(s string) json.RawMessage {
	var buf bytes.Buffer
	if err := json.Compact(&buf, []byte(s)); err != nil {
		// Static inputs must be valid JSON; panic so mis-edits fail loudly.
		panic("rawSchema: invalid JSON: " + err.Error())
	}
	return json.RawMessage(buf.Bytes())
}

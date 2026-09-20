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

// maxItemsProp is the shared max_items property: a per-call replacement for the
// active output profile's collapse_arrays cap. Accepts a positive integer or
// the string "all" (gum-pmbp).
const maxItemsProp = `"max_items":{"oneOf":[{"type":"integer","minimum":1},{"const":"all"}]}`

// writeInvokeBaseProps is the shared 8-param base for gum.write and gum.destructive
// (op_id, args, variant_id, fields, page_size, page_token, format, max_items) per spec §4.1.
const writeInvokeBaseProps = `"op_id":{"type":"string"},` +
	`"args":{"type":"object"},` +
	`"variant_id":{"type":"string"},` +
	`"fields":{"type":"string"},` +
	`"page_size":{"type":"integer","minimum":1},` +
	`"page_token":{"type":"string"},` +
	`"format":{"type":"string","enum":["toon","csv","json","markdown"]},` +
	maxItemsProp

// writeInvokeSchema builds the 10-param schema shared by gum.write and gum.destructive:
// the 8-param base plus confirmed+confirmation_token, required=["op_id","args"].
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
				"format":{"type":"string","enum":["toon","csv","json","markdown"]},
				` + maxItemsProp + `
			},
			"required":["op_id"],
			"additionalProperties":false
		}`)
	case "gum.write":
		// spec §4.1 / §6.1 — 10-param write schema; confirmed+confirmation_token optional.
		return writeInvokeSchema()
	case "gum.destructive":
		// spec §4.1 line ~284 / §6.1 — 10-param destructive schema; confirmed+confirmation_token optional.
		return writeInvokeSchema()
	case "gum.code":
		// spec.md §4.1 (8-param table) and §6.1 (gum.code semantics).
		// language enum is the v0.1.0 closed set: only "risor".
		// Reserved strings starlark/yaegi/js/python MUST NOT appear here per §6.1;
		// add them only when those runtimes ship.
		//
		// destructive_budget and destructive_scope carry the §1083 gate the
		// executor enforces: budget 0..20, scope an array of at most 20
		// {op_id, resource_key} objects. The scope items used to be declared
		// as strings, which the executor's extractScope drops on the floor, so
		// a caller that trusted the schema got an unscoped destructive run.
		return rawSchema(`{
			"type":"object",
			"properties":{
				"language":{"type":"string","enum":["risor"]},
				"source":{"type":"string"},
				"allow_write":{"type":"boolean","default":false},
				"allow_destructive":{"type":"boolean","default":false},
				"destructive_budget":{"type":"integer","default":0,"minimum":0,"maximum":20},
				"destructive_scope":{"type":"array","maxItems":20,"default":[],
					"items":{"type":"object","required":["op_id"],"additionalProperties":false,
						"properties":{"op_id":{"type":"string"},"resource_key":{"type":"string"}}}},
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
//
// Every property name here is an argument the backing op accepts, or the tool's
// declared body mapping, or a transport control the handler strips. That is what
// lets makeConvenienceHandler run behind validatedHandler: before gum-n1gi the
// schemas named `query`, `to`, `subject` and flat body fields that the dispatch
// kernel rejected as unknown, so enforcing them would have rejected every call.
// TestConvenienceSchemaPropertiesReachTheKernel pins the property set against
// the embedded catalog, so a schema cannot drift away from its op again.
func convenienceToolSchema(name string) json.RawMessage {
	switch name {
	case "gmail_search":
		return rawSchema(`{
			"type":"object",
			"properties":{
				"userId":{"type":"string"},
				"q":{"type":"string"},
				"labelIds":{"type":"array","items":{"type":"string"}},
				"maxResults":{"type":"integer","minimum":1,"maximum":500},
				"pageToken":{"type":"string"},
				"format":{"type":"string","enum":["toon","json"]}
			},
			"required":["userId"],
			"additionalProperties":false
		}`)
	case "gmail_get_message":
		// `format` here is the op's own request field (the Gmail payload shape),
		// not the §9 wire-format control; see ConvenienceABI.FormatControl.
		return rawSchema(`{
			"type":"object",
			"properties":{
				"userId":{"type":"string"},
				"id":{"type":"string"},
				"format":{"type":"string","enum":["full","minimal","raw","metadata"]},
				"metadataHeaders":{"type":"array","items":{"type":"string"}}
			},
			"required":["userId","id"],
			"additionalProperties":false
		}`)
	case "gmail_send":
		return withConfirmationFields(
			`"userId":{"type":"string"},"message":{"type":"object"},"threadId":{"type":"string"}`,
			`["userId","message"]`,
		)
	case "gmail_create_draft":
		return withConfirmationFields(
			`"userId":{"type":"string"},"message":{"type":"object"}`,
			`["userId","message"]`,
		)
	case "drive_find":
		return rawSchema(`{
			"type":"object",
			"properties":{
				"q":{"type":"string"},
				"pageSize":{"type":"integer","minimum":1,"maximum":1000},
				"pageToken":{"type":"string"},
				"corpora":{"type":"string"},
				"driveId":{"type":"string"},
				"format":{"type":"string","enum":["toon","json"]}
			},
			"additionalProperties":false
		}`)
	case "drive_get_file":
		return rawSchema(`{
			"type":"object",
			"properties":{
				"fileId":{"type":"string"},
				"alt":{"type":"string"},
				"fields":{"type":"string"},
				"format":{"type":"string","enum":["markdown","json"]}
			},
			"required":["fileId"],
			"additionalProperties":false
		}`)
	case "drive_share":
		return withConfirmationFields(
			`"fileId":{"type":"string"},"permission":{"type":"object"},"sendNotificationEmail":{"type":"boolean"}`,
			`["fileId","permission"]`,
		)
	case "calendar_upcoming":
		return rawSchema(`{
			"type":"object",
			"properties":{
				"calendarId":{"type":"string"},
				"timeMin":{"type":"string"},
				"timeMax":{"type":"string"},
				"q":{"type":"string"},
				"maxResults":{"type":"integer","minimum":1,"maximum":250},
				"pageToken":{"type":"string"},
				"format":{"type":"string","enum":["toon","json"]}
			},
			"required":["calendarId"],
			"additionalProperties":false
		}`)
	case "calendar_create_event":
		return withConfirmationFields(
			`"calendarId":{"type":"string"},"event":{"type":"object"},"sendUpdates":{"type":"string"}`,
			`["calendarId","event"]`,
		)
	case "calendar_update_event":
		return withConfirmationFields(
			`"calendarId":{"type":"string"},"eventId":{"type":"string"},"event":{"type":"object"},"sendUpdates":{"type":"string"}`,
			`["calendarId","eventId","event"]`,
		)
	case "docs_get":
		return rawSchema(`{
			"type":"object",
			"properties":{
				"documentId":{"type":"string"},
				"suggestionsViewMode":{"type":"string"},
				"includeTabsContent":{"type":"boolean"},
				"format":{"type":"string","enum":["markdown","json"]}
			},
			"required":["documentId"],
			"additionalProperties":false
		}`)
	case "docs_create":
		return withConfirmationFields(
			`"document":{"type":"object"}`,
			`["document"]`,
		)
	case "sheets_read":
		return rawSchema(`{
			"type":"object",
			"properties":{
				"spreadsheetId":{"type":"string"},
				"range":{"type":"string"},
				"majorDimension":{"type":"string","enum":["ROWS","COLUMNS"]},
				"valueRenderOption":{"type":"string"},
				"dateTimeRenderOption":{"type":"string"},
				"format":{"type":"string","enum":["csv","json"]}
			},
			"required":["spreadsheetId","range"],
			"additionalProperties":false
		}`)
	case "sheets_write":
		return withConfirmationFields(
			`"spreadsheetId":{"type":"string"},"range":{"type":"string"},"values":{"type":"array","items":{"type":"array"}},"valueInputOption":{"type":"string","enum":["RAW","USER_ENTERED"]},"includeValuesInResponse":{"type":"boolean"}`,
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
				"showCompleted":{"type":"boolean"},
				"showHidden":{"type":"boolean"},
				"maxResults":{"type":"integer","minimum":1,"maximum":100},
				"pageToken":{"type":"string"},
				"format":{"type":"string","enum":["toon","json"]}
			},
			"required":["tasklist"],
			"additionalProperties":false
		}`)
	case "tasks_create":
		return withConfirmationFields(
			`"tasklist":{"type":"string"},"task":{"type":"object"},"parent":{"type":"string"},"previous":{"type":"string"}`,
			`["tasklist","task"]`,
		)
	case "flights_search":
		return rawSchema(`{
			"type":"object",
			"properties":{
				"origin":{"type":"string"},
				"destination":{"type":"string"},
				"departure_date":{"type":"string"},
				"return_date":{"type":"string"},
				"adults":{"type":"integer","minimum":1,"maximum":9},
				"cabin":{"type":"string"},
				"format":{"type":"string","enum":["toon","json"]}
			},
			"required":["origin","destination","departure_date"],
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
		return describeOpResultSchema()
	case "gum.read", "gum.write", "gum.destructive", "gum.code":
		return shapedResultSchema()
	case "gum.poll":
		// gum.poll returns the upstream LRO Operation untouched: no profile
		// runs, so it cannot emit a ToonResult or a csv/markdown
		// SingleObjectResult. Registering the full three-branch shaped schema
		// promised branches this tool never takes.
		return namedResultSchema("RawJsonResult", rawJSONResultDefJSON)
	case "gum.cache_stats":
		return cacheStatsResultSchema()
	case "gum.gain":
		return gainResultSchema()
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
// outputSchema. Every one of them dispatches through dispatchAndShape, so they
// all share one schema; the name is kept so the per-tool registration scan can
// keep calling this per tool.
func convenienceToolOutputSchema(_ string) json.RawMessage {
	return shapedResultSchema()
}

// expressionMetaDefJSON is the spec §13 ExpressionMeta definition. Every
// result shape $refs it, so it is emitted once per registered outputSchema.
// It stays open (additionalProperties: true) because §13 requires clients to
// accept future _-prefixed diagnostic fields without rejecting the response.
const expressionMetaDefJSON = `{
		"type":"object",
		"required":["profile","op_id","variant_id","lossy","result_count"],
		"properties":{
			"profile":{"type":"string"},
			"op_id":{"type":"string"},
			"variant_id":{"type":["string","null"]},
			"lossy":{"type":"boolean"},
			"result_count":{"type":"integer","minimum":0},
			"omitted_count":{"type":"integer","minimum":0},
			"on_empty_message":{"type":["string","null"]},
			"full_result_path":{"type":"string"},
			"full_result_resource":{"type":"string"},
			"project_root_uri":{"type":["string","null"]},
			"_profile_resolution_warning":{"type":["string","null"]},
			"artifact_expires_at":{"type":["string","null"]},
			"intentional_zero_max_items":{"type":["boolean","null"]},
			"_code_output_truncated":{"type":["boolean","null"]}
		},
		"additionalProperties":true
	}`

// resultShapeDefsJSON is the shared `$defs` block: the three spec §13 result
// shapes plus the ExpressionMeta they reference. `data` is left open on
// purpose (§2704): markdown data is a string, json data is any JSON value.
const resultShapeDefsJSON = `"$defs":{
		"ExpressionMeta":` + expressionMetaDefJSON + `,
		"ToonResult":{
			"type":"object",
			"required":["format","toon","_expression"],
			"properties":{
				"format":{"const":"toon"},
				"toon":{"type":"string"},
				"op":{"type":"string"},
				"variant":{"type":"string"},
				"next_page_token":{"type":"string"},
				"_expression":{"$ref":"#/$defs/ExpressionMeta"}
			},
			"additionalProperties":false
		},
		"SingleObjectResult":{
			"type":"object",
			"required":["format","data","_expression"],
			"properties":{
				"format":{"enum":["json","csv","markdown"]},
				"data":{},
				"_expression":{"$ref":"#/$defs/ExpressionMeta"}
			},
			"additionalProperties":false
		},
		"RawJsonResult":{
			"type":"object",
			"required":["format","data","_expression"],
			"properties":{
				"format":{"const":"json"},
				"data":{},
				"_expression":{"$ref":"#/$defs/ExpressionMeta"}
			},
			"additionalProperties":false
		}
	}`

// toonResultSchema is the output schema for tools that always emit a §13
// ToonResult. Only gum.search_apis qualifies: it renders its own TOON body
// and never routes through the expression pipeline, so its resolved format
// cannot vary. Root type is "object" because the go-sdk requires it.
func toonResultSchema() json.RawMessage {
	return rawSchema(`{
		"type":"object",
		` + resultShapeDefsJSON + `,
		"allOf":[{"$ref":"#/$defs/ToonResult"}]
	}`)
}

// shapedResultSchema is the output schema for every tool whose
// structuredContent comes from dispatchAndShape. Which of the three §13
// shapes it emits is decided at call time by the resolved profile format
// (§2701-2705), and a project-local profile can change that format for the
// same tool, so the registered schema must admit all three branches.
//
// `anyOf`, not `oneOf`: a {"format":"json","data":...} value validates
// against both SingleObjectResult and RawJsonResult, and `oneOf` rejects a
// value that matches more than one branch.
func shapedResultSchema() json.RawMessage {
	return rawSchema(`{
		"type":"object",
		` + resultShapeDefsJSON + `,
		"anyOf":[
			{"$ref":"#/$defs/ToonResult"},
			{"$ref":"#/$defs/SingleObjectResult"},
			{"$ref":"#/$defs/RawJsonResult"}
		]
	}`)
}

// singleObjectResultSchema is the output schema for tools that emit a single
// shaped record.
func singleObjectResultSchema() json.RawMessage {
	return rawSchema(`{
		"type":"object",
		` + resultShapeDefsJSON + `,
		"allOf":[{"$ref":"#/$defs/SingleObjectResult"}]
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

// describeOpResultDefJSON is the spec §13 DescribeOpResult definition, the
// schema §2258 requires gum.describe_op to register. Transcribed from §13 with
// the `description` and `$comment` text dropped: those address spec readers,
// not clients, and no registered schema in this file carries them.
//
// The `oneOf` is the execution_support discriminator: a "full" op MUST NOT
// carry `unsupported_capabilities`, and every other value MUST carry it. §918
// splits the reason but not the requirement: "partial" lists the non-executable
// atoms, "schema_only" and "typed_executor_required" list the blocking ones.
const describeOpResultDefJSON = `{
	  "type": "object",
	  "required": [
	    "op_id",
	    "title",
	    "summary",
	    "default_variant_id",
	    "variants",
	    "variants_total",
	    "variants_omitted_count",
	    "risk_class",
	    "execution_support"
	  ],
	  "properties": {
	    "op_id": {
	      "type": "string"
	    },
	    "title": {
	      "type": "string"
	    },
	    "summary": {
	      "type": "string"
	    },
	    "default_variant_id": {
	      "type": "string"
	    },
	    "variants": {
	      "type": "array",
	      "items": {
	        "type": "object",
	        "required": [
	          "variant_id",
	          "stability",
	          "execution_support"
	        ],
	        "properties": {
	          "variant_id": {
	            "type": "string"
	          },
	          "stability": {
	            "enum": [
	              "stable",
	              "beta",
	              "alpha",
	              "deprecated"
	            ]
	          },
	          "execution_support": {
	            "enum": [
	              "full",
	              "partial",
	              "schema_only",
	              "typed_executor_required"
	            ]
	          },
	          "interface_kind": {
	            "type": "string"
	          },
	          "deprecated_reason": {
	            "type": [
	              "string",
	              "null"
	            ]
	          }
	        },
	        "additionalProperties": true
	      }
	    },
	    "variants_total": {
	      "type": "integer",
	      "minimum": 0
	    },
	    "variants_omitted_count": {
	      "type": "integer",
	      "minimum": 0
	    },
	    "risk_class": {
	      "enum": [
	        "read",
	        "write",
	        "destructive",
	        "code"
	      ]
	    },
	    "scopes": {
	      "type": "array",
	      "items": {
	        "type": "string"
	      }
	    },
	    "output_profile": {
	      "type": [
	        "string",
	        "null"
	      ]
	    },
	    "schema_refs": {
	      "type": "object",
	      "properties": {
	        "input": {
	          "type": "string"
	        },
	        "output": {
	          "type": "string"
	        }
	      },
	      "additionalProperties": false
	    },
	    "capability_class_warnings": {
	      "type": "array",
	      "items": {
	        "type": "string"
	      }
	    },
	    "risk_override": {
	      "type": "boolean"
	    },
	    "risk_override_reason": {
	      "type": "string",
	      "maxLength": 200
	    },
	    "execution_support": {
	      "enum": [
	        "full",
	        "partial",
	        "schema_only",
	        "typed_executor_required"
	      ]
	    },
	    "unsupported_capabilities": {
	      "type": "array",
	      "items": {
	        "type": "string"
	      }
	    },
	    "_expression": {
	      "$ref": "#/$defs/ExpressionMeta"
	    }
	  },
	  "oneOf": [
	    {
	      "properties": {
	        "execution_support": {
	          "const": "full"
	        }
	      },
	      "not": {
	        "required": [
	          "unsupported_capabilities"
	        ]
	      }
	    },
	    {
	      "properties": {
	        "execution_support": {
	          "enum": [
	            "partial",
	            "schema_only",
	            "typed_executor_required"
	          ]
	        }
	      },
	      "required": [
	        "unsupported_capabilities"
	      ]
	    }
	  ],
	  "additionalProperties": false
	}`

// gainResultDefJSON is the spec §13 GainResult definition, the schema §2256
// requires gum.gain to register. Its `oneOf` binds `mode` to exactly one
// mode-specific array: summary→sessions, session→operations, history→history.
const gainResultDefJSON = `{
	  "type": "object",
	  "required": [
	    "mode",
	    "window",
	    "baseline_tokens",
	    "actual_tokens",
	    "savings_tokens",
	    "savings_pct",
	    "end_to_end_savings",
	    "batch_envelope_overhead",
	    "tokenizer"
	  ],
	  "properties": {
	    "mode": {
	      "enum": [
	        "summary",
	        "session",
	        "history"
	      ]
	    },
	    "window": {
	      "type": "string"
	    },
	    "baseline_tokens": {
	      "type": "integer",
	      "minimum": 0
	    },
	    "actual_tokens": {
	      "type": "integer",
	      "minimum": 0
	    },
	    "savings_tokens": {
	      "type": "integer"
	    },
	    "savings_pct": {
	      "type": [
	        "number",
	        "null"
	      ]
	    },
	    "end_to_end_savings": {
	      "type": [
	        "number",
	        "null"
	      ]
	    },
	    "batch_envelope_overhead": {
	      "type": "integer",
	      "minimum": 0
	    },
	    "per_op_shaping_savings": {
	      "type": [
	        "number",
	        "null"
	      ]
	    },
	    "by_tool": {
	      "type": "array",
	      "items": {
	        "type": "object",
	        "required": [
	          "tool",
	          "calls",
	          "baseline_tokens",
	          "actual_tokens"
	        ],
	        "properties": {
	          "tool": {
	            "type": "string"
	          },
	          "calls": {
	            "type": "integer",
	            "minimum": 0
	          },
	          "baseline_tokens": {
	            "type": "integer",
	            "minimum": 0
	          },
	          "actual_tokens": {
	            "type": "integer",
	            "minimum": 0
	          }
	        },
	        "additionalProperties": false
	      }
	    },
	    "sessions": {
	      "type": "array",
	      "items": {
	        "type": "object",
	        "required": [
	          "session",
	          "calls",
	          "baseline_tokens",
	          "actual_tokens",
	          "savings_pct",
	          "op_families"
	        ],
	        "properties": {
	          "session": {
	            "type": "string",
	            "pattern": "^[0-9a-f]{8}$"
	          },
	          "calls": {
	            "type": "integer",
	            "minimum": 0
	          },
	          "baseline_tokens": {
	            "type": "integer",
	            "minimum": 0
	          },
	          "actual_tokens": {
	            "type": "integer",
	            "minimum": 0
	          },
	          "savings_pct": {
	            "type": [
	              "number",
	              "null"
	            ]
	          },
	          "op_families": {
	            "type": "array",
	            "items": {
	              "type": "string"
	            }
	          }
	        },
	        "additionalProperties": false
	      }
	    },
	    "operations": {
	      "type": "array",
	      "items": {
	        "type": "object",
	        "required": [
	          "op_id",
	          "op_family",
	          "calls",
	          "baseline_tokens",
	          "actual_tokens",
	          "cache_status",
	          "field_mask_status"
	        ],
	        "properties": {
	          "op_id": {
	            "type": "string"
	          },
	          "op_family": {
	            "type": "string"
	          },
	          "calls": {
	            "type": "integer",
	            "minimum": 0
	          },
	          "baseline_tokens": {
	            "type": "integer",
	            "minimum": 0
	          },
	          "actual_tokens": {
	            "type": "integer",
	            "minimum": 0
	          },
	          "cache_status": {
	            "type": "string"
	          },
	          "field_mask_status": {
	            "type": "string"
	          }
	        },
	        "additionalProperties": false
	      }
	    },
	    "history": {
	      "type": "array",
	      "items": {
	        "type": "object",
	        "required": [
	          "session",
	          "op_family",
	          "baseline_tokens",
	          "actual_tokens",
	          "savings_pct"
	        ],
	        "properties": {
	          "session": {
	            "type": "string",
	            "pattern": "^[0-9a-f]{8}$"
	          },
	          "op_family": {
	            "type": "string"
	          },
	          "baseline_tokens": {
	            "type": "integer",
	            "minimum": 0
	          },
	          "actual_tokens": {
	            "type": "integer",
	            "minimum": 0
	          },
	          "savings_pct": {
	            "type": [
	              "number",
	              "null"
	            ]
	          }
	        },
	        "additionalProperties": false
	      }
	    },
	    "tokenizer": {
	      "type": "string"
	    },
	    "_expression": {
	      "$ref": "#/$defs/ExpressionMeta"
	    }
	  },
	  "oneOf": [
	    {
	      "required": [
	        "sessions"
	      ],
	      "properties": {
	        "mode": {
	          "const": "summary"
	        },
	        "window": {
	          "pattern": "^(last-30-sessions|since:.+)$"
	        }
	      },
	      "not": {
	        "anyOf": [
	          {
	            "required": [
	              "operations"
	            ]
	          },
	          {
	            "required": [
	              "history"
	            ]
	          }
	        ]
	      }
	    },
	    {
	      "required": [
	        "operations"
	      ],
	      "properties": {
	        "mode": {
	          "const": "session"
	        },
	        "window": {
	          "pattern": "^session:[0-9a-f]{8}$"
	        }
	      },
	      "not": {
	        "anyOf": [
	          {
	            "required": [
	              "sessions"
	            ]
	          },
	          {
	            "required": [
	              "history"
	            ]
	          }
	        ]
	      }
	    },
	    {
	      "required": [
	        "history"
	      ],
	      "properties": {
	        "mode": {
	          "const": "history"
	        },
	        "window": {
	          "pattern": "^history(:since:.+)?$"
	        }
	      },
	      "not": {
	        "anyOf": [
	          {
	            "required": [
	              "sessions"
	            ]
	          },
	          {
	            "required": [
	              "operations"
	            ]
	          }
	        ]
	      }
	    }
	  ],
	  "additionalProperties": false
	}`

// cacheStatsResultDefJSON is the spec §13 CacheStatsResult definition, the
// schema §2257 requires gum.cache_stats to register.
const cacheStatsResultDefJSON = `{
	  "type": "object",
	  "required": [
	    "semantic",
	    "http",
	    "prompt",
	    "audit_broken"
	  ],
	  "properties": {
	    "semantic": {
	      "type": "object",
	      "required": [
	        "hits",
	        "misses",
	        "evictions",
	        "entries",
	        "bytes"
	      ],
	      "properties": {
	        "hits": {
	          "type": "integer",
	          "minimum": 0
	        },
	        "misses": {
	          "type": "integer",
	          "minimum": 0
	        },
	        "evictions": {
	          "type": "integer",
	          "minimum": 0
	        },
	        "entries": {
	          "type": "integer",
	          "minimum": 0
	        },
	        "bytes": {
	          "type": "integer",
	          "minimum": 0
	        }
	      },
	      "additionalProperties": false
	    },
	    "http": {
	      "type": "object",
	      "required": [
	        "hits",
	        "misses",
	        "entries",
	        "bytes"
	      ],
	      "properties": {
	        "hits": {
	          "type": "integer",
	          "minimum": 0
	        },
	        "misses": {
	          "type": "integer",
	          "minimum": 0
	        },
	        "entries": {
	          "type": "integer",
	          "minimum": 0
	        },
	        "bytes": {
	          "type": "integer",
	          "minimum": 0
	        }
	      },
	      "additionalProperties": false
	    },
	    "prompt": {
	      "type": "object",
	      "required": [
	        "supported",
	        "hits_estimate"
	      ],
	      "properties": {
	        "supported": {
	          "type": "boolean"
	        },
	        "hits_estimate": {
	          "type": [
	            "integer",
	            "null"
	          ],
	          "minimum": 0
	        }
	      },
	      "additionalProperties": false
	    },
	    "audit_broken": {
	      "type": "boolean"
	    },
	    "_expression": {
	      "$ref": "#/$defs/ExpressionMeta"
	    }
	  },
	  "additionalProperties": false
	}`

// namedResultSchema wraps one spec §13 named result shape in the registered
// outputSchema envelope: an object root (§3180 — the pinned go-sdk validates a
// registered outputSchema as an object schema), the shared ExpressionMeta
// $def that every named shape $refs, and an allOf pointing at the shape.
func namedResultSchema(name, defJSON string) json.RawMessage {
	return rawSchema(`{
		"type":"object",
		"$defs":{
			"ExpressionMeta":` + expressionMetaDefJSON + `,
			"` + name + `":` + defJSON + `
		},
		"allOf":[{"$ref":"#/$defs/` + name + `"}]
	}`)
}

// describeOpResultSchema is the output schema gum.describe_op registers
// (spec §2258).
func describeOpResultSchema() json.RawMessage {
	return namedResultSchema("DescribeOpResult", describeOpResultDefJSON)
}

// gainResultSchema is the output schema gum.gain registers (spec §2256).
func gainResultSchema() json.RawMessage {
	return namedResultSchema("GainResult", gainResultDefJSON)
}

// cacheStatsResultSchema is the output schema gum.cache_stats registers
// (spec §2257).
func cacheStatsResultSchema() json.RawMessage {
	return namedResultSchema("CacheStatsResult", cacheStatsResultDefJSON)
}

// skillsListResultSchema describes what skills_list actually returns: the
// embedded-skill summaries. The skill helpers sit outside the Tier A roster
// (§403, §2709), so §13 names no shape for them and they describe their own
// payload instead of borrowing a Tier A envelope.
func skillsListResultSchema() json.RawMessage {
	return rawSchema(`{
		"type":"object",
		"required":["skills"],
		"properties":{
			"skills":{"type":"array","items":{"$ref":"#/$defs/SkillSummary"}}
		},
		"additionalProperties":false,
		"$defs":{"SkillSummary":` + skillSummaryDefJSON + `}
	}`)
}

// skillSummaryDefJSON is the skills_list row: skills.Summary, which is
// skills.Skill without the body.
const skillSummaryDefJSON = `{
	"type":"object",
	"required":["name","version","summary","min_gum","sha256","bytes"],
	"properties":{
		"name":{"type":"string"},
		"version":{"type":"string"},
		"summary":{"type":"string"},
		"min_gum":{"type":"string"},
		"sha256":{"type":"string"},
		"bytes":{"type":"integer","minimum":0}
	},
	"additionalProperties":false
}`

// rawJSONResultDefJSON is the spec §13 RawJsonResult definition, registered on
// its own by the one tool that always emits it.
const rawJSONResultDefJSON = `{
	"type": "object",
	"required": [
		"format",
		"data",
		"_expression"
	],
	"properties": {
		"format": {
			"const": "json"
		},
		"data": {},
		"_expression": {
			"$ref": "#/$defs/ExpressionMeta"
		}
	},
	"additionalProperties": false
}`

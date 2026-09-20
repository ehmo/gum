package mcp

// ConvenienceABI is the ABI binding contract for one Tier A convenience tool,
// per spec.md §4.1 table.
type ConvenienceABI struct {
	OpID                    string
	VariantRule             string // "default" or a fixed variant_id
	OutputProfile           string
	Formats                 []string
	ConfirmationPassthrough bool

	// FormatControl is true when the tool advertises the §9 output-format
	// control under the key `format`. Spec §4.1 adds it to a row that lists
	// more than one format. gmail_get_message is the one multi-format row that
	// does not get it: gmail.users.messages.get declares its own `format`
	// request field (full/minimal/raw/metadata), and one key cannot mean both
	// the wire encoding and the Gmail payload shape. There the key belongs to
	// the op and passes through untouched.
	FormatControl bool

	// BodyArg names the single advertised argument whose object value IS the
	// upstream request body. drive_share advertises `permission`; the kernel
	// wants {"body": {"role": ..., "type": ...}}, because validateParams
	// collapses every location=body RequestField into the reserved "body" key.
	BodyArg string

	// BodyFields name advertised arguments that each become one body field
	// under their own name. sheets_write advertises `values`; the kernel wants
	// {"body": {"values": ...}}. A row may carry both: gmail_send spreads
	// `message` across the body and then overlays the top-level `threadId`.
	BodyFields []string
}

// convenienceABITable is the canonical ABI map for all 18 Tier A convenience
// tools (spec.md §4.1 lines 347-366).
var convenienceABITable = map[string]ConvenienceABI{
	"gmail_search": {
		OpID:          "gmail.users.messages.list",
		VariantRule:   "default",
		OutputProfile: "gmail.search.compact",
		Formats:       []string{"toon", "json"},
		FormatControl: true,
	},
	"gmail_get_message": {
		OpID:          "gmail.users.messages.get",
		VariantRule:   "default",
		OutputProfile: "gmail.message.compact",
		Formats:       []string{"markdown", "json"},
	},
	"gmail_send": {
		OpID:                    "gmail.users.messages.send",
		VariantRule:             "default",
		OutputProfile:           "gmail.send.result",
		Formats:                 []string{"json"},
		ConfirmationPassthrough: true,
		BodyArg:                 "message",
		BodyFields:              []string{"threadId"},
	},
	"gmail_create_draft": {
		OpID:                    "gmail.users.drafts.create",
		VariantRule:             "default",
		OutputProfile:           "gmail.draft.result",
		Formats:                 []string{"json"},
		ConfirmationPassthrough: true,
		BodyFields:              []string{"message"},
	},
	"drive_find": {
		OpID:          "drive.files.list",
		VariantRule:   "default",
		OutputProfile: "drive.files.compact",
		Formats:       []string{"toon", "json"},
		FormatControl: true,
	},
	"drive_get_file": {
		OpID:          "drive.files.get",
		VariantRule:   "default",
		OutputProfile: "drive.file.compact",
		Formats:       []string{"markdown", "json"},
		FormatControl: true,
	},
	"drive_share": {
		OpID:                    "drive.permissions.create",
		VariantRule:             "default",
		OutputProfile:           "drive.permission.result",
		Formats:                 []string{"json"},
		ConfirmationPassthrough: true,
		BodyArg:                 "permission",
	},
	"calendar_upcoming": {
		OpID:          "calendar.events.list",
		VariantRule:   "default",
		OutputProfile: "calendar.events.compact",
		Formats:       []string{"toon", "json"},
		FormatControl: true,
	},
	"calendar_create_event": {
		OpID:                    "calendar.events.insert",
		VariantRule:             "default",
		OutputProfile:           "calendar.event.result",
		Formats:                 []string{"json"},
		ConfirmationPassthrough: true,
		BodyArg:                 "event",
	},
	"calendar_update_event": {
		OpID:                    "calendar.events.update",
		VariantRule:             "default",
		OutputProfile:           "calendar.event.result",
		Formats:                 []string{"json"},
		ConfirmationPassthrough: true,
		BodyArg:                 "event",
	},
	"docs_get": {
		OpID:          "docs.documents.get",
		VariantRule:   "default",
		OutputProfile: "docs.document.markdown",
		Formats:       []string{"markdown", "json"},
		FormatControl: true,
	},
	"docs_create": {
		OpID:                    "docs.documents.create",
		VariantRule:             "default",
		OutputProfile:           "docs.create.result",
		Formats:                 []string{"json"},
		ConfirmationPassthrough: true,
		BodyArg:                 "document",
	},
	"sheets_read": {
		OpID:          "sheets.spreadsheets.values.get",
		VariantRule:   "default",
		OutputProfile: "sheets.values.compact",
		Formats:       []string{"csv", "json"},
		FormatControl: true,
	},
	"sheets_write": {
		OpID:                    "sheets.spreadsheets.values.update",
		VariantRule:             "default",
		OutputProfile:           "sheets.write.result",
		Formats:                 []string{"json"},
		ConfirmationPassthrough: true,
		BodyFields:              []string{"values"},
	},
	"slides_get": {
		OpID:          "slides.presentations.get",
		VariantRule:   "default",
		OutputProfile: "slides.presentation.compact",
		Formats:       []string{"json"},
	},
	"tasks_list": {
		OpID:          "tasks.tasks.list",
		VariantRule:   "default",
		OutputProfile: "tasks.list.compact",
		Formats:       []string{"toon", "json"},
		FormatControl: true,
	},
	"tasks_create": {
		OpID:                    "tasks.tasks.insert",
		VariantRule:             "default",
		OutputProfile:           "tasks.create.result",
		Formats:                 []string{"json"},
		ConfirmationPassthrough: true,
		BodyArg:                 "task",
	},
	"flights_search": {
		OpID:          "flights.search",
		VariantRule:   "flights.v1.plugin.search",
		OutputProfile: "flights.search.v1",
		Formats:       []string{"toon", "json"},
		FormatControl: true,
	},
}

// ConvenienceToolABI returns the ABI binding for the named Tier A convenience
// tool, or nil if name is not in the table. Returns a pointer to a copy so the
// caller cannot mutate the table.
func ConvenienceToolABI(name string) *ConvenienceABI {
	if row, ok := convenienceABITable[name]; ok {
		return &row
	}
	return nil
}

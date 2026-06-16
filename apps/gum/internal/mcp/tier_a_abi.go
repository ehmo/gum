package mcp

// ConvenienceABI is the ABI binding contract for one Tier A convenience tool,
// per spec.md §4.1 table.
type ConvenienceABI struct {
	OpID                    string
	VariantRule             string // "default" or a fixed variant_id
	OutputProfile           string
	Formats                 []string
	ConfirmationPassthrough bool
}

// convenienceABITable is the canonical ABI map for all 18 Tier A convenience
// tools (spec.md §4.1 lines 347-366).
var convenienceABITable = map[string]ConvenienceABI{
	"gmail_search": {
		OpID:          "gmail.users.messages.list",
		VariantRule:   "default",
		OutputProfile: "gmail.search.compact",
		Formats:       []string{"toon", "json"},
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
	},
	"gmail_create_draft": {
		OpID:                    "gmail.users.drafts.create",
		VariantRule:             "default",
		OutputProfile:           "gmail.draft.result",
		Formats:                 []string{"json"},
		ConfirmationPassthrough: true,
	},
	"drive_find": {
		OpID:          "drive.files.list",
		VariantRule:   "default",
		OutputProfile: "drive.files.compact",
		Formats:       []string{"toon", "json"},
	},
	"drive_get_file": {
		OpID:          "drive.files.get",
		VariantRule:   "default",
		OutputProfile: "drive.file.compact",
		Formats:       []string{"markdown", "json"},
	},
	"drive_share": {
		OpID:                    "drive.permissions.create",
		VariantRule:             "default",
		OutputProfile:           "drive.permission.result",
		Formats:                 []string{"json"},
		ConfirmationPassthrough: true,
	},
	"calendar_upcoming": {
		OpID:          "calendar.events.list",
		VariantRule:   "default",
		OutputProfile: "calendar.events.compact",
		Formats:       []string{"toon", "json"},
	},
	"calendar_create_event": {
		OpID:                    "calendar.events.insert",
		VariantRule:             "default",
		OutputProfile:           "calendar.event.result",
		Formats:                 []string{"json"},
		ConfirmationPassthrough: true,
	},
	"calendar_update_event": {
		OpID:                    "calendar.events.update",
		VariantRule:             "default",
		OutputProfile:           "calendar.event.result",
		Formats:                 []string{"json"},
		ConfirmationPassthrough: true,
	},
	"docs_get": {
		OpID:          "docs.documents.get",
		VariantRule:   "default",
		OutputProfile: "docs.document.markdown",
		Formats:       []string{"markdown", "json"},
	},
	"docs_create": {
		OpID:                    "docs.documents.create",
		VariantRule:             "default",
		OutputProfile:           "docs.create.result",
		Formats:                 []string{"json"},
		ConfirmationPassthrough: true,
	},
	"sheets_read": {
		OpID:          "sheets.spreadsheets.values.get",
		VariantRule:   "default",
		OutputProfile: "sheets.values.compact",
		Formats:       []string{"csv", "json"},
	},
	"sheets_write": {
		OpID:                    "sheets.spreadsheets.values.update",
		VariantRule:             "default",
		OutputProfile:           "sheets.write.result",
		Formats:                 []string{"json"},
		ConfirmationPassthrough: true,
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
	},
	"tasks_create": {
		OpID:                    "tasks.tasks.insert",
		VariantRule:             "default",
		OutputProfile:           "tasks.create.result",
		Formats:                 []string{"json"},
		ConfirmationPassthrough: true,
	},
	"flights_search": {
		OpID:          "flights.search",
		VariantRule:   "flights.v1.plugin.search",
		OutputProfile: "flights.search.v1",
		Formats:       []string{"toon", "json"},
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

package main

import "github.com/ehmo/gum/internal/catalog"

// request_fields_data.go is the central, reviewable source of RequestField
// descriptors for the Tier A convenience ops (Search Console is populated in
// its own builder). Applied to the embedded catalog offline via
// `gen-catalog --apply-request-fields`, which sets Op.RequestFields by op_id.
//
// Locations are authoritative: path params come from each op's URL template;
// query vs body follows the Google REST mapping (request body fields are the
// top-level properties of the method's request schema). Fields not listed here
// still work via the §12.0 grammar (unlisted args default to the query string);
// listing a field adds typed flags, enum/type validation, skeleton, and wizard.

func rfPath(name string) catalog.RequestField {
	return catalog.RequestField{Name: name, Location: catalog.RequestFieldPath, Type: "string", Required: true}
}
func rfQuery(name, typ string) catalog.RequestField {
	return catalog.RequestField{Name: name, Location: catalog.RequestFieldQuery, Type: typ}
}
func rfBody(name, typ string) catalog.RequestField {
	return catalog.RequestField{Name: name, Location: catalog.RequestFieldBody, Type: typ}
}

// tierARequestFields maps op_id -> RequestField set for the Tier A roster.
func tierARequestFields() map[string][]catalog.RequestField {
	q := catalog.RequestFieldQuery
	b := catalog.RequestFieldBody
	arg := catalog.RequestFieldArg
	return map[string][]catalog.RequestField{
		// ── Flights (bundled fli plugin; args pass straight to the tool) ─────
		"flights.search": {
			{Name: "origin", Location: arg, Type: "string", Required: true, Description: "Origin airport/city, e.g. SFO."},
			{Name: "destination", Location: arg, Type: "string", Required: true, Description: "Destination airport/city, e.g. JFK."},
			{Name: "departure_date", Location: arg, Type: "string", Format: "date", Required: true, Description: "Departure date (YYYY-MM-DD)."},
			{Name: "return_date", Location: arg, Type: "string", Format: "date", Description: "Return date (YYYY-MM-DD); omit for one-way."},
			{Name: "adults", Location: arg, Type: "integer", Description: "Number of adult passengers."},
			{Name: "cabin", Location: arg, Type: "string", Description: "Cabin class, e.g. economy, premium_economy, business, first."},
		},

		// ── Bundled Shape-1 plugins (args pass straight to the tool) ─────────
		"scholar.search": {
			{Name: "query", Location: arg, Type: "string", Required: true, Description: "Search terms for Google Scholar."},
		},
		"patents.search": {
			{Name: "query", Location: arg, Type: "string", Required: true, Description: "Search terms for Google Patents."},
		},
		"youtube.transcripts.get": {
			{Name: "video_id", Location: arg, Type: "string", Required: true, Description: "YouTube video ID (or full watch URL)."},
			{Name: "language", Location: arg, Type: "string", Description: "Preferred transcript language code, e.g. en."},
		},
		"trends.daily": {
			{Name: "geo", Location: arg, Type: "string", Required: true, Description: "Region code for trending searches, e.g. US."},
		},

		// ── Calendar body-only ops (no path/query params to derive) ─────────
		"calendar.calendars.insert": {
			{Name: "summary", Location: b, Type: "string", Required: true, Description: "Calendar title."},
			{Name: "description", Location: b, Type: "string"},
			{Name: "location", Location: b, Type: "string"},
			{Name: "timeZone", Location: b, Type: "string", Description: "IANA tz, e.g. America/Los_Angeles."},
		},
		"calendar.freebusy.query": {
			{Name: "timeMin", Location: b, Type: "string", Format: "date-time", Required: true, Description: "Start of the interval (RFC 3339)."},
			{Name: "timeMax", Location: b, Type: "string", Format: "date-time", Required: true, Description: "End of the interval (RFC 3339)."},
			{Name: "timeZone", Location: b, Type: "string"},
			{Name: "items", Location: b, Type: "array", ItemType: "object", Required: true, Description: "Calendars to query, e.g. items:=[{\"id\":\"primary\"}]."},
		},
		// ── Gmail ────────────────────────────────────────────────────────────
		"gmail.users.messages.list": {
			rfPath("userId"),
			{Name: "q", Location: q, Type: "string", Description: "Gmail search query, e.g. from:alice is:unread."},
			rfQuery("maxResults", "integer"),
			rfQuery("pageToken", "string"),
			{Name: "labelIds", Location: q, Type: "array", ItemType: "string", Description: "Only return messages with these label IDs."},
			rfQuery("includeSpamTrash", "boolean"),
		},
		"gmail.users.messages.get": {
			rfPath("userId"),
			rfPath("id"),
			{Name: "format", Location: q, Type: "string", Enum: []string{"minimal", "full", "raw", "metadata"}, Description: "Amount of message detail to return."},
			{Name: "metadataHeaders", Location: q, Type: "array", ItemType: "string", Description: "When format=metadata, the headers to include."},
		},
		"gmail.users.messages.send": {
			rfPath("userId"),
			{Name: "raw", Location: b, Type: "string", Description: "Base64url-encoded RFC 2822 message."},
			{Name: "threadId", Location: b, Type: "string", Description: "Thread to attach the reply to."},
		},
		"gmail.users.drafts.create": {
			rfPath("userId"),
			{Name: "message", Location: b, Type: "object", Description: "Draft message object, e.g. message:={\"raw\":\"...\"}."},
		},

		// ── Drive ────────────────────────────────────────────────────────────
		"drive.files.list": {
			{Name: "q", Location: q, Type: "string", Description: "Search query, e.g. name contains 'report' and trashed=false."},
			rfQuery("pageSize", "integer"),
			rfQuery("pageToken", "string"),
			rfQuery("orderBy", "string"),
			rfQuery("fields", "string"),
			{Name: "spaces", Location: q, Type: "string", Description: "Comma-separated: drive, appDataFolder."},
			{Name: "corpora", Location: q, Type: "string", Description: "One of: user, domain, drive, allDrives (free-form per the API)."},
			rfQuery("driveId", "string"),
			rfQuery("includeItemsFromAllDrives", "boolean"),
			rfQuery("supportsAllDrives", "boolean"),
		},
		"drive.files.get": {
			rfPath("fileId"),
			rfQuery("fields", "string"),
			{Name: "alt", Location: q, Type: "string", Enum: []string{"json", "media", "proto"}, Description: "media downloads file content."},
			rfQuery("acknowledgeAbuse", "boolean"),
			rfQuery("supportsAllDrives", "boolean"),
		},
		"drive.files.create": {
			{Name: "name", Location: b, Type: "string", Description: "File or folder name."},
			{Name: "mimeType", Location: b, Type: "string", Description: "Set to application/vnd.google-apps.folder to create a folder."},
			{Name: "parents", Location: b, Type: "array", ItemType: "string", Description: "Parent folder IDs."},
			rfQuery("fields", "string"),
			rfQuery("supportsAllDrives", "boolean"),
		},
		"drive.files.update": {
			rfPath("fileId"),
			{Name: "name", Location: b, Type: "string", Description: "New file name."},
			{Name: "description", Location: b, Type: "string"},
			{Name: "starred", Location: b, Type: "boolean"},
			{Name: "trashed", Location: b, Type: "boolean"},
			rfQuery("addParents", "string"),
			rfQuery("removeParents", "string"),
			rfQuery("fields", "string"),
			rfQuery("supportsAllDrives", "boolean"),
		},
		"drive.files.copy": {
			rfPath("fileId"),
			{Name: "name", Location: b, Type: "string", Description: "Name for the copied file."},
			{Name: "parents", Location: b, Type: "array", ItemType: "string", Description: "Parent folder IDs for the copy."},
			rfQuery("fields", "string"),
			rfQuery("supportsAllDrives", "boolean"),
		},
		"drive.files.delete": {
			rfPath("fileId"),
			rfQuery("supportsAllDrives", "boolean"),
		},
		"drive.files.export": {
			rfPath("fileId"),
			{Name: "mimeType", Location: q, Type: "string", Required: true, Description: "Export MIME type, e.g. application/pdf."},
		},
		"drive.permissions.list": {
			rfPath("fileId"),
			rfQuery("pageSize", "integer"),
			rfQuery("pageToken", "string"),
			rfQuery("fields", "string"),
			rfQuery("supportsAllDrives", "boolean"),
		},
		"drive.permissions.get": {
			rfPath("fileId"),
			rfPath("permissionId"),
			rfQuery("supportsAllDrives", "boolean"),
		},
		"drive.permissions.create": {
			rfPath("fileId"),
			{Name: "role", Location: b, Type: "string", Required: true, Enum: []string{"owner", "organizer", "fileOrganizer", "writer", "commenter", "reader"}},
			{Name: "type", Location: b, Type: "string", Required: true, Enum: []string{"user", "group", "domain", "anyone"}},
			{Name: "emailAddress", Location: b, Type: "string", Description: "For type=user or group."},
			{Name: "domain", Location: b, Type: "string", Description: "For type=domain."},
			rfQuery("sendNotificationEmail", "boolean"),
			rfQuery("transferOwnership", "boolean"),
			rfQuery("supportsAllDrives", "boolean"),
		},

		// ── Calendar ─────────────────────────────────────────────────────────
		"drive.permissions.update": {
			rfPath("fileId"),
			rfPath("permissionId"),
			{Name: "role", Location: b, Type: "string", Required: true, Enum: []string{"owner", "organizer", "fileOrganizer", "writer", "commenter", "reader"}},
			rfQuery("transferOwnership", "boolean"),
			rfQuery("supportsAllDrives", "boolean"),
		},
		"drive.permissions.delete": {
			rfPath("fileId"),
			rfPath("permissionId"),
			rfQuery("supportsAllDrives", "boolean"),
		},

		"calendar.events.list": {
			rfPath("calendarId"),
			{Name: "timeMin", Location: q, Type: "string", Format: "date-time", Description: "Lower bound (RFC 3339) for event end time."},
			{Name: "timeMax", Location: q, Type: "string", Format: "date-time", Description: "Upper bound (RFC 3339) for event start time."},
			rfQuery("maxResults", "integer"),
			rfQuery("singleEvents", "boolean"),
			{Name: "orderBy", Location: q, Type: "string", Enum: []string{"startTime", "updated"}},
			rfQuery("q", "string"),
			rfQuery("pageToken", "string"),
			rfQuery("showDeleted", "boolean"),
		},
		"calendar.events.insert": {
			rfPath("calendarId"),
			{Name: "summary", Location: b, Type: "string", Description: "Event title."},
			{Name: "description", Location: b, Type: "string"},
			{Name: "location", Location: b, Type: "string"},
			{Name: "start", Location: b, Type: "object", Required: true, Description: "e.g. start:={\"dateTime\":\"2026-06-01T10:00:00-07:00\"}."},
			{Name: "end", Location: b, Type: "object", Required: true, Description: "e.g. end:={\"dateTime\":\"2026-06-01T11:00:00-07:00\"}."},
			{Name: "attendees", Location: b, Type: "array", ItemType: "object", Description: "e.g. attendees:=[{\"email\":\"a@b.com\"}]."},
			{Name: "sendUpdates", Location: q, Type: "string", Enum: []string{"all", "externalOnly", "none"}},
		},
		"calendar.events.update": {
			rfPath("calendarId"),
			rfPath("eventId"),
			{Name: "summary", Location: b, Type: "string"},
			{Name: "description", Location: b, Type: "string"},
			{Name: "location", Location: b, Type: "string"},
			{Name: "start", Location: b, Type: "object", Required: true},
			{Name: "end", Location: b, Type: "object", Required: true},
			{Name: "sendUpdates", Location: q, Type: "string", Enum: []string{"all", "externalOnly", "none"}},
		},

		// ── Docs ─────────────────────────────────────────────────────────────
		"docs.documents.get": {
			rfPath("documentId"),
			{Name: "suggestionsViewMode", Location: q, Type: "string", Enum: []string{"DEFAULT_FOR_CURRENT_ACCESS", "SUGGESTIONS_INLINE", "PREVIEW_SUGGESTIONS_ACCEPTED", "PREVIEW_WITHOUT_SUGGESTIONS"}},
			rfQuery("includeTabsContent", "boolean"),
		},
		"docs.documents.create": {
			{Name: "title", Location: b, Type: "string", Description: "Title of the new document."},
		},

		"docs.documents.batchUpdate": {
			rfPath("documentId"),
			{Name: "requests", Location: b, Type: "array", ItemType: "object", Required: true, Description: "Docs batchUpdate request objects."},
			{Name: "writeControl", Location: b, Type: "object", Description: "Optional revision precondition."},
		},

		// ── Sheets ───────────────────────────────────────────────────────────
		"sheets.spreadsheets.create": {
			{Name: "properties", Location: b, Type: "object", Description: "Spreadsheet properties, including title."},
			{Name: "sheets", Location: b, Type: "array", ItemType: "object", Description: "Initial sheet definitions."},
		},
		"sheets.spreadsheets.get": {
			rfPath("spreadsheetId"),
			rfQuery("ranges", "array"),
			rfQuery("includeGridData", "boolean"),
		},
		"sheets.spreadsheets.batchUpdate": {
			rfPath("spreadsheetId"),
			{Name: "requests", Location: b, Type: "array", ItemType: "object", Required: true, Description: "Sheets batchUpdate request objects."},
			{Name: "includeSpreadsheetInResponse", Location: b, Type: "boolean"},
			{Name: "responseRanges", Location: b, Type: "array", ItemType: "string"},
		},
		"sheets.spreadsheets.values.get": {
			rfPath("spreadsheetId"),
			{Name: "range", Location: catalog.RequestFieldPath, Type: "string", Required: true, Description: "A1 notation, e.g. Sheet1!A1:C10."},
			{Name: "majorDimension", Location: q, Type: "string", Enum: []string{"ROWS", "COLUMNS"}},
			{Name: "valueRenderOption", Location: q, Type: "string", Enum: []string{"FORMATTED_VALUE", "UNFORMATTED_VALUE", "FORMULA"}},
			{Name: "dateTimeRenderOption", Location: q, Type: "string", Enum: []string{"SERIAL_NUMBER", "FORMATTED_STRING"}},
		},
		"sheets.spreadsheets.values.update": {
			rfPath("spreadsheetId"),
			{Name: "range", Location: catalog.RequestFieldPath, Type: "string", Required: true, Description: "A1 notation, e.g. Sheet1!A1:C10."},
			{Name: "valueInputOption", Location: q, Type: "string", Required: true, Enum: []string{"RAW", "USER_ENTERED"}, Description: "How input data is interpreted."},
			rfQuery("includeValuesInResponse", "boolean"),
			{Name: "responseValueRenderOption", Location: q, Type: "string", Enum: []string{"FORMATTED_VALUE", "UNFORMATTED_VALUE", "FORMULA"}},
			{Name: "responseDateTimeRenderOption", Location: q, Type: "string", Enum: []string{"SERIAL_NUMBER", "FORMATTED_STRING"}},
			{Name: "values", Location: b, Type: "array", ItemType: "array", Description: "Row-major 2D array, e.g. values:=[[\"a\",\"b\"],[1,2]]."},
		},

		// ── Slides ───────────────────────────────────────────────────────────
		"sheets.spreadsheets.values.batchGet": {
			rfPath("spreadsheetId"),
			{Name: "ranges", Location: q, Type: "array", ItemType: "string", Required: true, Description: "A1 ranges to read."},
			{Name: "majorDimension", Location: q, Type: "string", Enum: []string{"ROWS", "COLUMNS"}},
			{Name: "valueRenderOption", Location: q, Type: "string", Enum: []string{"FORMATTED_VALUE", "UNFORMATTED_VALUE", "FORMULA"}},
			{Name: "dateTimeRenderOption", Location: q, Type: "string", Enum: []string{"SERIAL_NUMBER", "FORMATTED_STRING"}},
		},
		"sheets.spreadsheets.values.batchUpdate": {
			rfPath("spreadsheetId"),
			{Name: "valueInputOption", Location: b, Type: "string", Required: true, Enum: []string{"RAW", "USER_ENTERED"}, Description: "How input data is interpreted."},
			{Name: "data", Location: b, Type: "array", ItemType: "object", Required: true, Description: "ValueRange objects to write."},
			{Name: "includeValuesInResponse", Location: b, Type: "boolean"},
			{Name: "responseValueRenderOption", Location: b, Type: "string", Enum: []string{"FORMATTED_VALUE", "UNFORMATTED_VALUE", "FORMULA"}},
			{Name: "responseDateTimeRenderOption", Location: b, Type: "string", Enum: []string{"SERIAL_NUMBER", "FORMATTED_STRING"}},
		},
		"sheets.spreadsheets.values.append": {
			rfPath("spreadsheetId"),
			{Name: "range", Location: catalog.RequestFieldPath, Type: "string", Required: true, Description: "A1 table range to append after."},
			{Name: "valueInputOption", Location: q, Type: "string", Required: true, Enum: []string{"RAW", "USER_ENTERED"}, Description: "How input data is interpreted."},
			{Name: "insertDataOption", Location: q, Type: "string", Enum: []string{"OVERWRITE", "INSERT_ROWS"}},
			rfQuery("includeValuesInResponse", "boolean"),
			{Name: "values", Location: b, Type: "array", ItemType: "array", Required: true, Description: "Row-major 2D array to append."},
		},
		"sheets.spreadsheets.values.clear": {
			rfPath("spreadsheetId"),
			{Name: "range", Location: catalog.RequestFieldPath, Type: "string", Required: true, Description: "A1 range to clear."},
		},
		"slides.presentations.get": {
			rfPath("presentationId"),
		},
		"slides.presentations.create": {
			{Name: "title", Location: b, Type: "string", Description: "Presentation title."},
		},
		"tasks.tasklists.insert": {
			{Name: "title", Location: b, Type: "string", Required: true, Description: "Task list title."},
		},
		"tasks.tasklists.update": {
			rfPath("tasklist"),
			{Name: "title", Location: b, Type: "string", Required: true, Description: "Task list title."},
		},
		"tasks.tasks.update": {
			rfPath("tasklist"),
			rfPath("task"),
			{Name: "title", Location: b, Type: "string", Description: "Task title."},
			{Name: "notes", Location: b, Type: "string"},
			{Name: "due", Location: b, Type: "string", Format: "date-time", Description: "Due date (RFC 3339)."},
			{Name: "status", Location: b, Type: "string", Enum: []string{"needsAction", "completed"}},
		},
		"admin.directory.groups.insert": {
			{Name: "email", Location: b, Type: "string", Required: true, Description: "Group email address."},
			{Name: "name", Location: b, Type: "string", Description: "Display name."},
			{Name: "description", Location: b, Type: "string"},
		},
		"admin.directory.groups.update": {
			rfPath("groupKey"),
			{Name: "email", Location: b, Type: "string", Description: "Group email address."},
			{Name: "name", Location: b, Type: "string", Description: "Display name."},
			{Name: "description", Location: b, Type: "string"},
		},
		"admin.directory.members.insert": {
			rfPath("groupKey"),
			{Name: "email", Location: b, Type: "string", Required: true, Description: "Member email address."},
			{Name: "role", Location: b, Type: "string", Required: true, Enum: []string{"OWNER", "MANAGER", "MEMBER"}},
			{Name: "type", Location: b, Type: "string", Enum: []string{"USER", "GROUP", "CUSTOMER", "EXTERNAL"}},
		},
		"admin.directory.users.insert": {
			rfQuery("resolveConflictAccount", "boolean"),
			{Name: "primaryEmail", Location: b, Type: "string", Required: true, Description: "Primary user email address."},
			{Name: "name", Location: b, Type: "object", Required: true, Description: "User name object with givenName and familyName."},
			{Name: "password", Location: b, Type: "string", Description: "Initial password when not using generated password flow."},
			{Name: "orgUnitPath", Location: b, Type: "string"},
			{Name: "suspended", Location: b, Type: "boolean"},
		},
		"admin.directory.users.update": {
			rfPath("userKey"),
			{Name: "primaryEmail", Location: b, Type: "string"},
			{Name: "name", Location: b, Type: "object", Description: "User name object with givenName and familyName."},
			{Name: "orgUnitPath", Location: b, Type: "string"},
			{Name: "suspended", Location: b, Type: "boolean"},
			{Name: "recoveryEmail", Location: b, Type: "string"},
			{Name: "recoveryPhone", Location: b, Type: "string"},
		},

		// ── Tasks ────────────────────────────────────────────────────────────
		"tasks.tasks.list": {
			rfPath("tasklist"),
			rfQuery("maxResults", "integer"),
			rfQuery("showCompleted", "boolean"),
			rfQuery("showHidden", "boolean"),
			rfQuery("showDeleted", "boolean"),
			{Name: "dueMin", Location: q, Type: "string", Format: "date-time"},
			{Name: "dueMax", Location: q, Type: "string", Format: "date-time"},
			{Name: "updatedMin", Location: q, Type: "string", Format: "date-time", Description: "Only tasks modified since this RFC 3339 time."},
			rfQuery("pageToken", "string"),
		},
		"tasks.tasks.insert": {
			rfPath("tasklist"),
			{Name: "parent", Location: q, Type: "string", Description: "Parent task ID (for subtasks)."},
			{Name: "previous", Location: q, Type: "string", Description: "Previous sibling task ID (ordering)."},
			{Name: "title", Location: b, Type: "string", Description: "Task title."},
			{Name: "notes", Location: b, Type: "string"},
			{Name: "due", Location: b, Type: "string", Format: "date-time", Description: "Due date (RFC 3339)."},
			{Name: "status", Location: b, Type: "string", Enum: []string{"needsAction", "completed"}},
		},
	}
}

// silence unused-helper warnings if a future edit drops the last caller.
var _ = rfBody

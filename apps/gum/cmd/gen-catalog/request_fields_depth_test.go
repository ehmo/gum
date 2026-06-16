package main

import (
	"testing"

	"github.com/ehmo/gum/internal/catalog"
)

func TestP1DepthOpsHaveRequestFields(t *testing.T) {
	fields := tierARequestFields()
	want := map[string][]string{
		"drive.files.create":                  {"name"},
		"drive.files.update":                  {"fileId"},
		"drive.files.copy":                    {"fileId"},
		"drive.files.delete":                  {"fileId"},
		"drive.files.export":                  {"fileId", "mimeType"},
		"drive.permissions.list":              {"fileId"},
		"drive.permissions.get":               {"fileId", "permissionId"},
		"drive.permissions.update":            {"fileId", "permissionId", "role"},
		"drive.permissions.delete":            {"fileId", "permissionId"},
		"docs.documents.batchUpdate":          {"documentId", "requests"},
		"sheets.spreadsheets.create":          {"properties"},
		"sheets.spreadsheets.get":             {"spreadsheetId"},
		"sheets.spreadsheets.batchUpdate":     {"spreadsheetId", "requests"},
		"sheets.spreadsheets.values.batchGet": {"spreadsheetId", "ranges"},
		"sheets.spreadsheets.values.batchUpdate": {"spreadsheetId",
			"valueInputOption", "data"},
		"sheets.spreadsheets.values.append": {"spreadsheetId", "range", "valueInputOption",
			"values"},
		"sheets.spreadsheets.values.clear": {"spreadsheetId", "range"},
	}

	for opID, names := range want {
		t.Run(opID, func(t *testing.T) {
			got := fields[opID]
			if len(got) == 0 {
				t.Fatalf("%s request fields missing", opID)
			}
			for _, name := range names {
				if !hasRequestField(got, name) {
					t.Fatalf("%s request fields missing %q in %#v", opID, name, got)
				}
			}
		})
	}
}

func TestP1DepthOpsRequiredFields(t *testing.T) {
	fields := tierARequestFields()
	wantRequired := map[string][]string{
		"drive.files.export":                     {"fileId", "mimeType"},
		"drive.permissions.update":               {"fileId", "permissionId", "role"},
		"docs.documents.batchUpdate":             {"documentId", "requests"},
		"sheets.spreadsheets.batchUpdate":        {"spreadsheetId", "requests"},
		"sheets.spreadsheets.values.batchGet":    {"spreadsheetId", "ranges"},
		"sheets.spreadsheets.values.batchUpdate": {"spreadsheetId", "valueInputOption", "data"},
		"sheets.spreadsheets.values.append":      {"spreadsheetId", "range", "valueInputOption", "values"},
		"sheets.spreadsheets.values.clear":       {"spreadsheetId", "range"},
	}

	for opID, names := range wantRequired {
		t.Run(opID, func(t *testing.T) {
			for _, name := range names {
				field, ok := findRequestField(fields[opID], name)
				if !ok {
					t.Fatalf("%s request fields missing %q", opID, name)
				}
				if !field.Required {
					t.Fatalf("%s request field %q is not marked required", opID, name)
				}
			}
		})
	}
}

func hasRequestField(fields []catalog.RequestField, name string) bool {
	_, ok := findRequestField(fields, name)
	return ok
}

func findRequestField(fields []catalog.RequestField, name string) (catalog.RequestField, bool) {
	for _, field := range fields {
		if field.Name == name {
			return field, true
		}
	}
	return catalog.RequestField{}, false
}

package main

import "github.com/ehmo/gum/internal/catalog"

const (
	scopeDocsReadonly   = "https://www.googleapis.com/auth/documents.readonly"
	scopeDocs           = "https://www.googleapis.com/auth/documents"
	scopeSheetsReadonly = "https://www.googleapis.com/auth/spreadsheets.readonly"
	scopeSheets         = "https://www.googleapis.com/auth/spreadsheets"
	scopeSlidesReadonly = "https://www.googleapis.com/auth/presentations.readonly"
	scopeSlides         = "https://www.googleapis.com/auth/presentations"
)

// BuildDocsOps returns the Tier A Docs v1 operations:
//
//	read:  docs.documents.get    (GET)
//	write: docs.documents.create (POST)
//
// Spec §4.1 lines 359-360. Backs the docs_get / docs_create convenience tools
// declared in internal/mcp/tier_a_abi.go. Both variants use BYO OAuth and the
// typed-rest-sdk adapter; live discovery-doc walk is deferred to the offline
// gen-catalog network path — these hand-curated entries unblock the dispatch
// resolver so smoke tests can exercise gum call --risk=read end-to-end.
func BuildDocsOps() []catalog.Op {
	op := func(opID, variantID, title, summary string, risk catalog.RiskClass, scopes []string, method, path, goCall string) catalog.Op {
		return makeWorkspaceOp(workspaceOpSpec{
			opID: opID, variantID: variantID, title: title, summary: summary,
			service: "docs", riskClass: risk, scopes: scopes,
			httpMethod: method, httpPath: path,
			goPkg: "google.golang.org/api/docs/v1", goCall: goCall,
		})
	}
	const base = "https://docs.googleapis.com/v1"
	return []catalog.Op{
		op("docs.documents.get", "docs.v1.rest.documents.get", "Get a Doc",
			"Fetch a Google Doc by document ID. Backs the docs_get convenience tool.",
			catalog.RiskClassRead, []string{scopeDocsReadonly}, "GET", base+"/documents/{documentId}", "Documents.Get"),
		op("docs.documents.create", "docs.v1.rest.documents.create", "Create a Doc",
			"Create a Google Doc. Request body lives in args.document. Backs the docs_create convenience tool.",
			catalog.RiskClassWrite, []string{scopeDocs}, "POST", base+"/documents", "Documents.Create"),
		op("docs.documents.batchUpdate", "docs.v1.rest.documents.batchUpdate", "Edit a Doc (batchUpdate)",
			"Apply a batch of edit requests to a Google Doc (insert/replace text, formatting, tables, images). The core Docs editing op.",
			catalog.RiskClassWrite, []string{scopeDocs}, "POST", base+"/documents/{documentId}:batchUpdate", "Documents.BatchUpdate"),
	}
}

// BuildSheetsOps returns the Tier A Sheets v4 operations:
//
//	read:  sheets.spreadsheets.values.get    (GET)
//	write: sheets.spreadsheets.values.update (PUT)
//
// Spec §4.1 lines 361-362. Backs the sheets_read / sheets_write convenience
// tools declared in internal/mcp/tier_a_abi.go.
func BuildSheetsOps() []catalog.Op {
	op := func(opID, variantID, title, summary string, risk catalog.RiskClass, scopes []string, method, path, goCall string) catalog.Op {
		return makeWorkspaceOp(workspaceOpSpec{
			opID: opID, variantID: variantID, title: title, summary: summary,
			service: "sheets", riskClass: risk, scopes: scopes,
			httpMethod: method, httpPath: path,
			goPkg: "google.golang.org/api/sheets/v4", goCall: goCall,
		})
	}
	ro := []string{scopeSheetsReadonly}
	rw := []string{scopeSheets}
	const base = "https://sheets.googleapis.com/v4/spreadsheets"
	return []catalog.Op{
		op("sheets.spreadsheets.create", "sheets.v4.rest.spreadsheets.create", "Create a Spreadsheet",
			"Create a new Google Spreadsheet.",
			catalog.RiskClassWrite, rw, "POST", base, "Spreadsheets.Create"),
		op("sheets.spreadsheets.get", "sheets.v4.rest.spreadsheets.get", "Get a Spreadsheet",
			"Fetch a spreadsheet's metadata, sheets, and (optionally) cell data.",
			catalog.RiskClassRead, ro, "GET", base+"/{spreadsheetId}", "Spreadsheets.Get"),
		op("sheets.spreadsheets.batchUpdate", "sheets.v4.rest.spreadsheets.batchUpdate", "Edit a Spreadsheet (batchUpdate)",
			"Apply structural/formatting edits to a spreadsheet (add sheets, format cells, charts, conditional formatting).",
			catalog.RiskClassWrite, rw, "POST", base+"/{spreadsheetId}:batchUpdate", "Spreadsheets.BatchUpdate"),
		op("sheets.spreadsheets.values.get", "sheets.v4.rest.spreadsheets.values.get", "Read a Sheets Range",
			"Read values from a Google Sheets range. Backs the sheets_read convenience tool.",
			catalog.RiskClassRead, ro, "GET", base+"/{spreadsheetId}/values/{range}", "Spreadsheets.Values.Get"),
		op("sheets.spreadsheets.values.batchGet", "sheets.v4.rest.spreadsheets.values.batchGet", "Read Multiple Sheets Ranges",
			"Read values from several ranges of a spreadsheet in one call.",
			catalog.RiskClassRead, ro, "GET", base+"/{spreadsheetId}/values:batchGet", "Spreadsheets.Values.BatchGet"),
		op("sheets.spreadsheets.values.update", "sheets.v4.rest.spreadsheets.values.update", "Update a Sheets Range",
			"Write values to a Google Sheets range. Backs the sheets_write convenience tool.",
			catalog.RiskClassWrite, rw, "PUT", base+"/{spreadsheetId}/values/{range}", "Spreadsheets.Values.Update"),
		op("sheets.spreadsheets.values.batchUpdate", "sheets.v4.rest.spreadsheets.values.batchUpdate", "Update Multiple Sheets Ranges",
			"Write values to several ranges of a spreadsheet in one call.",
			catalog.RiskClassWrite, rw, "POST", base+"/{spreadsheetId}/values:batchUpdate", "Spreadsheets.Values.BatchUpdate"),
		op("sheets.spreadsheets.values.append", "sheets.v4.rest.spreadsheets.values.append", "Append Rows to a Sheet",
			"Append rows of values after a table in a spreadsheet range.",
			catalog.RiskClassWrite, rw, "POST", base+"/{spreadsheetId}/values/{range}:append", "Spreadsheets.Values.Append"),
		op("sheets.spreadsheets.values.clear", "sheets.v4.rest.spreadsheets.values.clear", "Clear a Sheets Range",
			"Clear the values from a spreadsheet range (keeps formatting).",
			catalog.RiskClassWrite, rw, "POST", base+"/{spreadsheetId}/values/{range}:clear", "Spreadsheets.Values.Clear"),
	}
}

// BuildSlidesOps returns the Tier A Slides v1 operation:
//
//	read: slides.presentations.get (GET)
//
// Spec §4.1 line 363. Backs the slides_get convenience tool declared in
// internal/mcp/tier_a_abi.go. Write ops on Slides go through batchUpdate and
// are not exposed as a Tier A convenience tool in v0.1.0 (callers fall back
// to gum call slides.presentations.batchUpdate when needed).
func BuildSlidesOps() []catalog.Op {
	op := func(opID, variantID, title, summary string, risk catalog.RiskClass, scopes []string, method, path, goCall string) catalog.Op {
		return makeWorkspaceOp(workspaceOpSpec{
			opID: opID, variantID: variantID, title: title, summary: summary,
			service: "slides", riskClass: risk, scopes: scopes,
			httpMethod: method, httpPath: path,
			goPkg: "google.golang.org/api/slides/v1", goCall: goCall,
		})
	}
	const base = "https://slides.googleapis.com/v1/presentations"
	return []catalog.Op{
		op("slides.presentations.get", "slides.v1.rest.presentations.get", "Get a Slides Presentation",
			"Fetch compact Slides presentation metadata and page summaries. Backs the slides_get convenience tool.",
			catalog.RiskClassRead, []string{scopeSlidesReadonly}, "GET", base+"/{presentationId}", "Presentations.Get"),
		op("slides.presentations.create", "slides.v1.rest.presentations.create", "Create a Slides Presentation",
			"Create a new Google Slides presentation.",
			catalog.RiskClassWrite, []string{scopeSlides}, "POST", base, "Presentations.Create"),
		op("slides.presentations.batchUpdate", "slides.v1.rest.presentations.batchUpdate", "Edit a Slides Presentation (batchUpdate)",
			"Apply a batch of edit requests to a presentation (add slides, insert text/shapes/images, formatting). The core Slides editing op.",
			catalog.RiskClassWrite, []string{scopeSlides}, "POST", base+"/{presentationId}:batchUpdate", "Presentations.BatchUpdate"),
	}
}

// workspaceOpSpec is the shared shape used to declare a discovery-rest
// Workspace op. Mirrors tasksOpSpec but parameterizes service + go_pkg so the
// builder can serve docs, sheets, and slides from one helper.
type workspaceOpSpec struct {
	opID        string
	variantID   string
	title       string
	summary     string
	service     string
	riskClass   catalog.RiskClass
	scopes      []string
	httpMethod  string
	httpPath    string
	goPkg       string
	goCall      string
	adminPolicy *catalog.AdminPolicy
}

// makeWorkspaceOp builds a catalog.Op for one Workspace discovery-rest op.
func makeWorkspaceOp(s workspaceOpSpec) catalog.Op {
	return catalog.Op{
		OpID:             s.opID,
		OpSchemaVersion:  1,
		Title:            s.title,
		Summary:          s.summary,
		Service:          s.service,
		ServiceFamily:    "workspace",
		DefaultVariantID: s.variantID,
		Variants: []catalog.Variant{
			{
				VariantID:            s.variantID,
				VariantSchemaVersion: 1,
				Version:              versionFromVariantID(s.variantID),
				Stability:            catalog.StabilityStable,
				InterfaceKind:        catalog.InterfaceKindDiscoveryREST,
				BackendKind:          catalog.BackendKindTypedRestSDK,
				Preferred:            true,
				RiskClass:            s.riskClass,
				AuthStrategy:         catalog.AuthStrategyBYOOAuth,
				Scopes:               s.scopes,
				AdminPolicy:          s.adminPolicy,
				Binding: &catalog.Binding{
					BindingSchemaVersion: 1,
					AdapterKey:           "rest.typed-rest-sdk",
					OperationKey:         s.opID,
					HTTP: &catalog.HTTPBinding{
						Method: s.httpMethod,
						Path:   s.httpPath,
					},
					GoPkg:  s.goPkg,
					GoCall: s.goCall,
				},
			},
		},
	}
}

// versionFromVariantID extracts the API version label embedded in the variant
// id (e.g. "docs.v1.rest.documents.get" -> "v1", "sheets.v4.rest.…" -> "v4").
// The Workspace ops in v0.1.0 all carry a "<service>.<vN>." prefix, so a
// single token lookup is sufficient and explicit (avoids drifting from the
// httpPath component).
func versionFromVariantID(variantID string) string {
	for i := 0; i+3 < len(variantID); i++ {
		if variantID[i] == '.' && variantID[i+1] == 'v' &&
			variantID[i+2] >= '0' && variantID[i+2] <= '9' {
			j := i + 2
			for j < len(variantID) && variantID[j] >= '0' && variantID[j] <= '9' {
				j++
			}
			return variantID[i+1 : j]
		}
	}
	return ""
}

package main

import "github.com/ehmo/gum/internal/catalog"

// Google Forms API v1 OAuth scopes.
const (
	scopeFormsBody              = "https://www.googleapis.com/auth/forms.body"
	scopeFormsBodyReadonly      = "https://www.googleapis.com/auth/forms.body.readonly"
	scopeFormsResponsesReadonly = "https://www.googleapis.com/auth/forms.responses.readonly"
)

// BuildFormsOps returns the Google Forms API v1 surface: form create/get/
// batchUpdate (structure editing) and response list/get. typed-rest-sdk,
// byo_oauth. Discovery ids are forms.forms.* (apiName == resource).
func BuildFormsOps() []catalog.Op {
	op := func(opID, variantID, title, summary string, risk catalog.RiskClass, scopes []string, method, path, goCall string) catalog.Op {
		return makeWorkspaceOp(workspaceOpSpec{
			opID: opID, variantID: variantID, title: title, summary: summary,
			service: "forms", riskClass: risk, scopes: scopes,
			httpMethod: method, httpPath: path,
			goPkg: "google.golang.org/api/forms/v1", goCall: goCall,
		})
	}
	const base = "https://forms.googleapis.com/v1"
	return []catalog.Op{
		op("forms.forms.create", "forms.v1.rest.forms.create", "Create a Form",
			"Create a new Google Form (args.body.info.title). Returns the formId.",
			catalog.RiskClassWrite, []string{scopeFormsBody}, "POST", base+"/forms", "Forms.Create"),
		op("forms.forms.get", "forms.v1.rest.forms.get", "Get a Form",
			"Fetch a form's structure (items, questions, settings) by formId.",
			catalog.RiskClassRead, []string{scopeFormsBodyReadonly}, "GET", base+"/forms/{formId}", "Forms.Get"),
		op("forms.forms.batchUpdate", "forms.v1.rest.forms.batchUpdate", "Edit a Form (batchUpdate)",
			"Apply a batch of edits to a form (add/update/delete items, update form info/settings). The core Forms editing op.",
			catalog.RiskClassWrite, []string{scopeFormsBody}, "POST", base+"/forms/{formId}:batchUpdate", "Forms.BatchUpdate"),
		op("forms.forms.responses.list", "forms.v1.rest.forms.responses.list", "List Form Responses",
			"List the submitted responses for a form (optional filter, pageSize, pageToken).",
			catalog.RiskClassRead, []string{scopeFormsResponsesReadonly}, "GET", base+"/forms/{formId}/responses", "Forms.Responses.List"),
		op("forms.forms.responses.get", "forms.v1.rest.forms.responses.get", "Get a Form Response",
			"Fetch a single form response by responseId.",
			catalog.RiskClassRead, []string{scopeFormsResponsesReadonly}, "GET", base+"/forms/{formId}/responses/{responseId}", "Forms.Responses.Get"),
	}
}

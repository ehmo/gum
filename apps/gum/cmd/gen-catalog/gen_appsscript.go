package main

import "github.com/ehmo/gum/internal/catalog"

// Apps Script API v1 OAuth scopes.
const (
	scopeScriptProjects         = "https://www.googleapis.com/auth/script.projects"
	scopeScriptProjectsReadonly = "https://www.googleapis.com/auth/script.projects.readonly"
	scopeScriptDeployments      = "https://www.googleapis.com/auth/script.deployments"
)

// BuildAppsScriptOps returns the Apps Script API v1 project-management surface:
// projects create/get, content get/update, deployments list. typed-rest-sdk,
// byo_oauth. (scripts.run — executing a deployed function — is intentionally
// excluded: it requires a matching OAuth client + deployment and is unsafe to
// model as a generic op.)
func BuildAppsScriptOps() []catalog.Op {
	op := func(opID, variantID, title, summary string, risk catalog.RiskClass, scope, method, path, goCall string) catalog.Op {
		return makeWorkspaceOp(workspaceOpSpec{
			opID: opID, variantID: variantID, title: title, summary: summary,
			service: "script", riskClass: risk, scopes: []string{scope},
			httpMethod: method, httpPath: path,
			goPkg: "google.golang.org/api/script/v1", goCall: goCall,
		})
	}
	const base = "https://script.googleapis.com/v1"
	return []catalog.Op{
		op("script.projects.create", "script.v1.rest.projects.create", "Create an Apps Script Project",
			"Create a new (standalone) Apps Script project (args.body.title).",
			catalog.RiskClassWrite, scopeScriptProjects, "POST", base+"/projects", "Projects.Create"),
		op("script.projects.get", "script.v1.rest.projects.get", "Get an Apps Script Project",
			"Fetch an Apps Script project's metadata by scriptId.",
			catalog.RiskClassRead, scopeScriptProjectsReadonly, "GET", base+"/projects/{scriptId}", "Projects.Get"),
		op("script.projects.getContent", "script.v1.rest.projects.getContent", "Get Apps Script Content",
			"Fetch the source files of an Apps Script project.",
			catalog.RiskClassRead, scopeScriptProjectsReadonly, "GET", base+"/projects/{scriptId}/content", "Projects.GetContent"),
		op("script.projects.updateContent", "script.v1.rest.projects.updateContent", "Update Apps Script Content",
			"Replace the source files of an Apps Script project (args.body.files).",
			catalog.RiskClassWrite, scopeScriptProjects, "PUT", base+"/projects/{scriptId}/content", "Projects.UpdateContent"),
		op("script.projects.deployments.list", "script.v1.rest.projects.deployments.list", "List Apps Script Deployments",
			"List the deployments of an Apps Script project.",
			catalog.RiskClassRead, scopeScriptDeployments, "GET", base+"/projects/{scriptId}/deployments", "Projects.Deployments.List"),
	}
}

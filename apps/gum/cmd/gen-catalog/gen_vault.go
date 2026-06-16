package main

import "github.com/ehmo/gum/internal/catalog"

// Google Vault API v1 OAuth scopes.
const (
	scopeEdiscovery         = "https://www.googleapis.com/auth/ediscovery"
	scopeEdiscoveryReadonly = "https://www.googleapis.com/auth/ediscovery.readonly"
)

// BuildVaultOps returns the Google Vault API v1 matter surface: list/get/create/
// update/close/delete. Vault requires a Workspace account with eDiscovery
// privileges. typed-rest-sdk, byo_oauth.
func BuildVaultOps() []catalog.Op {
	op := func(opID, variantID, title, summary string, risk catalog.RiskClass, scope, method, path, goCall string) catalog.Op {
		return makeWorkspaceOp(workspaceOpSpec{
			opID: opID, variantID: variantID, title: title, summary: summary,
			service: "vault", riskClass: risk, scopes: []string{scope},
			httpMethod: method, httpPath: path,
			goPkg: "google.golang.org/api/vault/v1", goCall: goCall,
		})
	}
	const base = "https://vault.googleapis.com/v1"
	return []catalog.Op{
		op("vault.matters.list", "vault.v1.rest.matters.list", "List Vault Matters",
			"List the eDiscovery matters the caller can access (state, view, pageSize).",
			catalog.RiskClassRead, scopeEdiscoveryReadonly, "GET", base+"/matters", "Matters.List"),
		op("vault.matters.get", "vault.v1.rest.matters.get", "Get a Vault Matter",
			"Fetch a matter by matterId.",
			catalog.RiskClassRead, scopeEdiscoveryReadonly, "GET", base+"/matters/{matterId}", "Matters.Get"),
		op("vault.matters.create", "vault.v1.rest.matters.create", "Create a Vault Matter",
			"Create a new eDiscovery matter (args.body: name, description).",
			catalog.RiskClassWrite, scopeEdiscovery, "POST", base+"/matters", "Matters.Create"),
		op("vault.matters.update", "vault.v1.rest.matters.update", "Update a Vault Matter",
			"Update a matter's name/description by matterId.",
			catalog.RiskClassWrite, scopeEdiscovery, "PUT", base+"/matters/{matterId}", "Matters.Update"),
		op("vault.matters.close", "vault.v1.rest.matters.close", "Close a Vault Matter",
			"Close an eDiscovery matter by matterId.",
			catalog.RiskClassWrite, scopeEdiscovery, "POST", base+"/matters/{matterId}:close", "Matters.Close"),
		op("vault.matters.delete", "vault.v1.rest.matters.delete", "Delete a Vault Matter",
			"Delete a matter by matterId (must be closed first). Destructive — requires confirmation per §6.1.",
			catalog.RiskClassDestructive, scopeEdiscovery, "DELETE", base+"/matters/{matterId}", "Matters.Delete"),
	}
}

package main

import "github.com/ehmo/gum/internal/catalog"

// People API (Contacts) OAuth scopes.
const (
	scopeContactsReadonly = "https://www.googleapis.com/auth/contacts.readonly"
	scopeContacts         = "https://www.googleapis.com/auth/contacts"
)

// BuildPeopleOps returns the Tier A People API (v1) surface for the user's
// contacts: the `people` resource (connections.list / get / searchContacts /
// create / update / delete) and the `contactGroups` resource (list / get /
// create / delete). Uses makeWorkspaceOp (discovery-rest / typed-rest-sdk,
// byo_oauth). resourceName path params use {+resourceName} reserved expansion
// so the "people/cNNN" / "contactGroups/NNN" value keeps its '/'.
//
// NOTE: op_ids carry the consistent service.resource.method shape; the People
// Discovery doc names the same methods people.* and contactGroups.* —
// discoveryMethodID() (enrich_discovery.go) maps between them for enrichment.
func BuildPeopleOps() []catalog.Op {
	op := func(opID, variantID, title, summary string, risk catalog.RiskClass, scopes []string, method, path, goCall string) catalog.Op {
		return makeWorkspaceOp(workspaceOpSpec{
			opID: opID, variantID: variantID, title: title, summary: summary,
			service: "people", riskClass: risk, scopes: scopes,
			httpMethod: method, httpPath: path,
			goPkg: "google.golang.org/api/people/v1", goCall: goCall,
		})
	}
	ro := []string{scopeContactsReadonly}
	rw := []string{scopeContacts}
	const base = "https://people.googleapis.com/v1"
	return []catalog.Op{
		// --- people / connections ---
		op("people.connections.list", "people.v1.rest.connections.list", "List Contacts",
			"List the authenticated user's contacts (requires personFields, e.g. names,emailAddresses,phoneNumbers).",
			catalog.RiskClassRead, ro, "GET", base+"/people/me/connections", "People.Connections.List"),
		op("people.people.get", "people.v1.rest.people.get", "Get a Contact",
			"Fetch a contact or profile by resourceName (people/cNNN, or people/me). Requires personFields.",
			catalog.RiskClassRead, ro, "GET", base+"/{+resourceName}", "People.Get"),
		op("people.people.searchContacts", "people.v1.rest.people.searchContacts", "Search Contacts",
			"Search the user's contacts by query (requires query + readMask).",
			catalog.RiskClassRead, ro, "GET", base+"/people:searchContacts", "People.SearchContacts"),
		op("people.people.createContact", "people.v1.rest.people.createContact", "Create a Contact",
			"Create a new contact (args.body: names, emailAddresses, phoneNumbers, …).",
			catalog.RiskClassWrite, rw, "POST", base+"/people:createContact", "People.CreateContact"),
		op("people.people.updateContact", "people.v1.rest.people.updateContact", "Update a Contact",
			"Update a contact by resourceName (requires updatePersonFields; pass the current etag in the body).",
			catalog.RiskClassWrite, rw, "PATCH", base+"/{+resourceName}:updateContact", "People.UpdateContact"),
		op("people.people.deleteContact", "people.v1.rest.people.deleteContact", "Delete a Contact",
			"Delete a contact by resourceName. Destructive — requires confirmation per §6.1.",
			catalog.RiskClassDestructive, rw, "DELETE", base+"/{+resourceName}:deleteContact", "People.DeleteContact"),
		// --- contact groups (labels) ---
		op("people.contactGroups.list", "people.v1.rest.contactGroups.list", "List Contact Groups",
			"List the user's contact groups (labels).",
			catalog.RiskClassRead, ro, "GET", base+"/contactGroups", "ContactGroups.List"),
		op("people.contactGroups.get", "people.v1.rest.contactGroups.get", "Get a Contact Group",
			"Fetch a contact group by resourceName (contactGroups/NNN).",
			catalog.RiskClassRead, ro, "GET", base+"/{+resourceName}", "ContactGroups.Get"),
		op("people.contactGroups.create", "people.v1.rest.contactGroups.create", "Create a Contact Group",
			"Create a new contact group/label (args.body.contactGroup.name).",
			catalog.RiskClassWrite, rw, "POST", base+"/contactGroups", "ContactGroups.Create"),
		op("people.contactGroups.delete", "people.v1.rest.contactGroups.delete", "Delete a Contact Group",
			"Delete a contact group by resourceName. Destructive — requires confirmation per §6.1.",
			catalog.RiskClassDestructive, rw, "DELETE", base+"/{+resourceName}", "ContactGroups.Delete"),
	}
}

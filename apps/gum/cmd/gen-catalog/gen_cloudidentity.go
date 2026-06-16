package main

import "github.com/ehmo/gum/internal/catalog"

// Cloud Identity API v1 OAuth scope (groups read).
const scopeCloudIdentityGroupsReadonly = "https://www.googleapis.com/auth/cloud-identity.groups.readonly"

// BuildCloudIdentityOps returns a read surface for Cloud Identity groups +
// memberships (the modern successor to Admin Directory groups). Groups and
// memberships are addressed by resource name via {+name}/{+parent}. groups.list
// requires a `parent` query (customers/<id>). typed-rest-sdk, byo_oauth.
func BuildCloudIdentityOps() []catalog.Op {
	op := func(opID, variantID, title, summary, method, path, goCall string) catalog.Op {
		return makeWorkspaceOp(workspaceOpSpec{
			opID: opID, variantID: variantID, title: title, summary: summary,
			service: "cloudidentity", riskClass: catalog.RiskClassRead,
			scopes:     []string{scopeCloudIdentityGroupsReadonly},
			httpMethod: method, httpPath: path,
			goPkg: "google.golang.org/api/cloudidentity/v1", goCall: goCall,
		})
	}
	const base = "https://cloudidentity.googleapis.com/v1"
	return []catalog.Op{
		op("cloudidentity.groups.list", "cloudidentity.v1.rest.groups.list", "List Cloud Identity Groups",
			"List groups under a parent (parent=customers/<id>; view=BASIC|FULL).",
			"GET", base+"/groups", "Groups.List"),
		op("cloudidentity.groups.get", "cloudidentity.v1.rest.groups.get", "Get a Cloud Identity Group",
			"Fetch a group by resource name (groups/<id>).",
			"GET", base+"/{+name}", "Groups.Get"),
		op("cloudidentity.groups.memberships.list", "cloudidentity.v1.rest.groups.memberships.list", "List Group Memberships",
			"List the memberships of a Cloud Identity group (parent=groups/<id>).",
			"GET", base+"/{+parent}/memberships", "Groups.Memberships.List"),
		op("cloudidentity.groups.memberships.get", "cloudidentity.v1.rest.groups.memberships.get", "Get a Group Membership",
			"Fetch a single membership by resource name (groups/<id>/memberships/<id>).",
			"GET", base+"/{+name}", "Groups.Memberships.Get"),
	}
}

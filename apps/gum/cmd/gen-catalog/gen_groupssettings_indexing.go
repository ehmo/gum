package main

import "github.com/ehmo/gum/internal/catalog"

// Groups Settings API v1 + Web Search Indexing API v3 OAuth scopes.
const (
	scopeGroupsSettings = "https://www.googleapis.com/auth/apps.groups.settings"
	scopeIndexing       = "https://www.googleapis.com/auth/indexing"
)

// BuildGroupsSettingsOps returns the Groups Settings API v1 surface: read/update
// a Workspace group's configuration (who can post, join, etc.). The group is
// addressed by its email (groupUniqueId). The Discovery method ids are
// camelCased "groupsSettings.*" — discoveryMethodID maps gum's lowercase service
// op_ids to them. typed-rest-sdk, byo_oauth.
func BuildGroupsSettingsOps() []catalog.Op {
	op := func(opID, variantID, title, summary string, risk catalog.RiskClass, method, path, goCall string) catalog.Op {
		return makeWorkspaceOp(workspaceOpSpec{
			opID: opID, variantID: variantID, title: title, summary: summary,
			service: "groupssettings", riskClass: risk, scopes: []string{scopeGroupsSettings},
			httpMethod: method, httpPath: path,
			goPkg: "google.golang.org/api/groupssettings/v1", goCall: goCall,
		})
	}
	const base = "https://www.googleapis.com/groups/v1/groups"
	return []catalog.Op{
		op("groupssettings.groups.get", "groupssettings.v1.rest.groups.get", "Get Group Settings",
			"Fetch a Workspace group's settings by email (groupUniqueId): posting permissions, join policy, archiving, etc.",
			catalog.RiskClassRead, "GET", base+"/{groupUniqueId}", "Groups.Get"),
		op("groupssettings.groups.update", "groupssettings.v1.rest.groups.update", "Update Group Settings",
			"Replace a Workspace group's settings (args.body).",
			catalog.RiskClassWrite, "PUT", base+"/{groupUniqueId}", "Groups.Update"),
	}
}

// BuildIndexingOps returns the Web Search Indexing API v3 surface: notify Google
// of URL updates/removals (JobPosting/BroadcastEvent structured data) and read
// the last notification metadata for a URL. typed-rest-sdk, byo_oauth.
func BuildIndexingOps() []catalog.Op {
	op := func(opID, variantID, title, summary string, risk catalog.RiskClass, method, path, goCall string) catalog.Op {
		return makeWorkspaceOp(workspaceOpSpec{
			opID: opID, variantID: variantID, title: title, summary: summary,
			service: "indexing", riskClass: risk, scopes: []string{scopeIndexing},
			httpMethod: method, httpPath: path,
			goPkg: "google.golang.org/api/indexing/v3", goCall: goCall,
		})
	}
	const base = "https://indexing.googleapis.com/v3"
	return []catalog.Op{
		op("indexing.urlNotifications.publish", "indexing.v3.rest.urlNotifications.publish", "Publish a URL Notification",
			"Notify Google that a URL was updated or deleted (args.body: url, type=URL_UPDATED|URL_DELETED).",
			catalog.RiskClassWrite, "POST", base+"/urlNotifications:publish", "UrlNotifications.Publish"),
		op("indexing.urlNotifications.getMetadata", "indexing.v3.rest.urlNotifications.getMetadata", "Get URL Notification Metadata",
			"Fetch the most recent notification metadata gum sent Google for a URL (url query param).",
			catalog.RiskClassRead, "GET", base+"/urlNotifications/metadata", "UrlNotifications.GetMetadata"),
	}
}

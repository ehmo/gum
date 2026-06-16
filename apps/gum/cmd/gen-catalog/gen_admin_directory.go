package main

import "github.com/ehmo/gum/internal/catalog"

// Admin SDK Directory API OAuth scopes.
const (
	scopeAdminDirectoryUserReadonly        = "https://www.googleapis.com/auth/admin.directory.user.readonly"
	scopeAdminDirectoryGroupReadonly       = "https://www.googleapis.com/auth/admin.directory.group.readonly"
	scopeAdminDirectoryGroupMemberReadonly = "https://www.googleapis.com/auth/admin.directory.group.member.readonly"
	scopeAdminDirectoryUser                = "https://www.googleapis.com/auth/admin.directory.user"
	scopeAdminDirectoryGroup               = "https://www.googleapis.com/auth/admin.directory.group"
	scopeAdminDirectoryGroupMember         = "https://www.googleapis.com/auth/admin.directory.group.member"
)

// BuildAdminDirectoryOps returns the long-tail raw-http surface for the
// Admin SDK Directory API. Spec §5.7: raw-http variants opt into the
// read-only allowlist escape hatch so callers may pass query parameters that
// aren't in the discovery doc with a `_validation_warnings` envelope entry
// instead of an INVALID_ARGS error.
//
// READS use the raw-http long-tail surface (the §5.7 allowlist escape hatch).
// WRITES use the typed-rest-sdk path (adminDirectoryWriteOps) so the policy
// kernel keeps full visibility into directory mutations — the deliberate split
// noted in v0.1.0. Destructive ops (user/group/member delete) are
// confirmation-gated.
func BuildAdminDirectoryOps() []catalog.Op {
	ops := []catalog.Op{
		makeAdminDirectoryOp(adminDirectoryOpSpec{
			opID:           "admin.directory.users.list",
			variantID:      "admin.directory_v1.rawhttp.users.list",
			title:          "List Workspace directory users",
			summary:        "List the directory user accounts in the customer's Google Workspace.",
			scopes:         []string{scopeAdminDirectoryUserReadonly},
			httpMethod:     "GET",
			httpPath:       "/admin/directory/v1/users",
			paramsRequired: [][]string{{"customer", "string"}},
			paramsOptional: [][]string{
				{"domain", "string"},
				{"maxResults", "integer"},
				{"pageToken", "string"},
				{"query", "string"},
				{"showDeleted", "string"},
			},
		}),
		makeAdminDirectoryOp(adminDirectoryOpSpec{
			opID:           "admin.directory.users.get",
			variantID:      "admin.directory_v1.rawhttp.users.get",
			title:          "Get a Workspace directory user",
			summary:        "Fetch a single directory user account by key (userKey = id, primaryEmail, or alias).",
			scopes:         []string{scopeAdminDirectoryUserReadonly},
			httpMethod:     "GET",
			httpPath:       "/admin/directory/v1/users/{userKey}",
			paramsRequired: [][]string{{"userKey", "string"}},
			paramsOptional: [][]string{
				{"projection", "string"},
				{"viewType", "string"},
			},
		}),
		makeAdminDirectoryOp(adminDirectoryOpSpec{
			opID:           "admin.directory.groups.list",
			variantID:      "admin.directory_v1.rawhttp.groups.list",
			title:          "List Workspace directory groups",
			summary:        "List the directory groups in the customer's Google Workspace.",
			scopes:         []string{scopeAdminDirectoryGroupReadonly},
			httpMethod:     "GET",
			httpPath:       "/admin/directory/v1/groups",
			paramsRequired: [][]string{{"customer", "string"}},
			paramsOptional: [][]string{
				{"domain", "string"},
				{"maxResults", "integer"},
				{"pageToken", "string"},
				{"query", "string"},
				{"userKey", "string"},
			},
		}),
		// --- additional reads (raw-http, consistent with the above) ---
		makeAdminDirectoryOp(adminDirectoryOpSpec{
			opID:           "admin.directory.groups.get",
			variantID:      "admin.directory_v1.rawhttp.groups.get",
			title:          "Get a Workspace directory group",
			summary:        "Fetch a single directory group by key (groupKey = id or email).",
			scopes:         []string{scopeAdminDirectoryGroupReadonly},
			httpMethod:     "GET",
			httpPath:       "/admin/directory/v1/groups/{groupKey}",
			paramsRequired: [][]string{{"groupKey", "string"}},
		}),
		makeAdminDirectoryOp(adminDirectoryOpSpec{
			opID:           "admin.directory.members.list",
			variantID:      "admin.directory_v1.rawhttp.members.list",
			title:          "List group members",
			summary:        "List the members of a Workspace directory group.",
			scopes:         []string{scopeAdminDirectoryGroupMemberReadonly},
			httpMethod:     "GET",
			httpPath:       "/admin/directory/v1/groups/{groupKey}/members",
			paramsRequired: [][]string{{"groupKey", "string"}},
			paramsOptional: [][]string{
				{"maxResults", "integer"},
				{"pageToken", "string"},
				{"roles", "string"},
				{"includeDerivedMembership", "string"},
			},
		}),
		makeAdminDirectoryOp(adminDirectoryOpSpec{
			opID:           "admin.directory.members.get",
			variantID:      "admin.directory_v1.rawhttp.members.get",
			title:          "Get a group member",
			summary:        "Fetch a single member of a Workspace directory group.",
			scopes:         []string{scopeAdminDirectoryGroupMemberReadonly},
			httpMethod:     "GET",
			httpPath:       "/admin/directory/v1/groups/{groupKey}/members/{memberKey}",
			paramsRequired: [][]string{{"groupKey", "string"}, {"memberKey", "string"}},
		}),
	}
	ops = append(ops, adminDirectoryWriteOps()...)
	return ops
}

// adminDirectoryWriteOps returns the directory MUTATION surface via the typed-
// rest-sdk path (full https URLs), so the policy kernel sees every write/
// destructive op. user/group/member create+update+delete.
func adminDirectoryWriteOps() []catalog.Op {
	op := func(opID, variantID, title, summary string, risk catalog.RiskClass, scope, method, path, goCall string, resourceKeys []string) catalog.Op {
		return makeWorkspaceOp(workspaceOpSpec{
			opID: opID, variantID: variantID, title: title, summary: summary,
			service: "admin", riskClass: risk, scopes: []string{scope},
			httpMethod: method, httpPath: path,
			goPkg: "google.golang.org/api/admin/directory/v1", goCall: goCall,
			adminPolicy: adminFixtureWritePolicy(resourceKeys...),
		})
	}
	const base = "https://admin.googleapis.com/admin/directory/v1"
	return []catalog.Op{
		// users
		op("admin.directory.users.insert", "admin.directory_v1.rest.users.insert", "Create a directory user",
			"Create a new Workspace user account (args.body: primaryEmail, name, password, …).",
			catalog.RiskClassWrite, scopeAdminDirectoryUser, "POST", base+"/users", "Users.Insert", []string{"primaryEmail"}),
		op("admin.directory.users.update", "admin.directory_v1.rest.users.update", "Update a directory user",
			"Update a Workspace user account by userKey.",
			catalog.RiskClassWrite, scopeAdminDirectoryUser, "PUT", base+"/users/{userKey}", "Users.Update", []string{"userKey"}),
		op("admin.directory.users.delete", "admin.directory_v1.rest.users.delete", "Delete a directory user",
			"Delete a Workspace user account. Destructive — requires confirmation per §6.1.",
			catalog.RiskClassDestructive, scopeAdminDirectoryUser, "DELETE", base+"/users/{userKey}", "Users.Delete", []string{"userKey"}),
		// groups
		op("admin.directory.groups.insert", "admin.directory_v1.rest.groups.insert", "Create a directory group",
			"Create a new Workspace group (args.body: email, name, description).",
			catalog.RiskClassWrite, scopeAdminDirectoryGroup, "POST", base+"/groups", "Groups.Insert", []string{"email"}),
		op("admin.directory.groups.update", "admin.directory_v1.rest.groups.update", "Update a directory group",
			"Update a Workspace group by groupKey.",
			catalog.RiskClassWrite, scopeAdminDirectoryGroup, "PUT", base+"/groups/{groupKey}", "Groups.Update", []string{"groupKey"}),
		op("admin.directory.groups.delete", "admin.directory_v1.rest.groups.delete", "Delete a directory group",
			"Delete a Workspace group. Destructive — requires confirmation per §6.1.",
			catalog.RiskClassDestructive, scopeAdminDirectoryGroup, "DELETE", base+"/groups/{groupKey}", "Groups.Delete", []string{"groupKey"}),
		// members
		op("admin.directory.members.insert", "admin.directory_v1.rest.members.insert", "Add a group member",
			"Add a member to a Workspace group (args.body: email, role).",
			catalog.RiskClassWrite, scopeAdminDirectoryGroupMember, "POST", base+"/groups/{groupKey}/members", "Members.Insert", []string{"groupKey", "email"}),
		op("admin.directory.members.delete", "admin.directory_v1.rest.members.delete", "Remove a group member",
			"Remove a member from a Workspace group. Destructive — requires confirmation per §6.1.",
			catalog.RiskClassDestructive, scopeAdminDirectoryGroupMember, "DELETE", base+"/groups/{groupKey}/members/{memberKey}", "Members.Delete", []string{"groupKey", "memberKey"}),
	}
}

func adminFixtureWritePolicy(resourceKeys ...string) *catalog.AdminPolicy {
	return &catalog.AdminPolicy{
		BlastRadius:              catalog.AdminBlastRadiusFixtureWrite,
		FixtureOwnershipRequired: true,
		FixtureMarkerPrefix:      catalog.AdminFixtureMarkerPrefix,
		FixtureResourceKeys:      resourceKeys,
	}
}

var adminHighBlastRadiusExcludedOps = map[string]catalog.AdminBlastRadius{
	"admin.directory.orgunits.insert":              catalog.AdminBlastRadiusHighBlast,
	"admin.directory.orgunits.update":              catalog.AdminBlastRadiusHighBlast,
	"admin.directory.roles.insert":                 catalog.AdminBlastRadiusHighBlast,
	"admin.directory.roleAssignments.insert":       catalog.AdminBlastRadiusHighBlast,
	"admin.directory.domains.insert":               catalog.AdminBlastRadiusHighBlast,
	"admin.directory.domainAliases.insert":         catalog.AdminBlastRadiusHighBlast,
	"admin.directory.verificationCodes.generate":   catalog.AdminBlastRadiusHighBlast,
	"admin.directory.chromeosdevices.action.batch": catalog.AdminBlastRadiusHighBlast,
}

// adminDirectoryOpSpec parameterises a raw-http Admin Directory op. Distinct
// from workspaceOpSpec because the backend_kind / interface_kind / adapter_key
// triplet is fixed to the raw-http executor — the spec §5.7 long-tail path.
type adminDirectoryOpSpec struct {
	opID           string
	variantID      string
	title          string
	summary        string
	scopes         []string
	httpMethod     string
	httpPath       string
	paramsRequired [][]string
	paramsOptional [][]string
}

// makeAdminDirectoryOp builds a catalog.Op whose default variant is raw-http
// + read-class. Used by the dispatcher's §5.7 allowlist gate to recognise the
// op as eligible for the unknown-key pass-through.
func makeAdminDirectoryOp(s adminDirectoryOpSpec) catalog.Op {
	return catalog.Op{
		OpID:             s.opID,
		OpSchemaVersion:  1,
		Title:            s.title,
		Summary:          s.summary,
		Service:          "admin",
		ServiceFamily:    "workspace",
		ParamsRequired:   s.paramsRequired,
		ParamsOptional:   s.paramsOptional,
		DefaultVariantID: s.variantID,
		Variants: []catalog.Variant{
			{
				VariantID:            s.variantID,
				VariantSchemaVersion: 1,
				Version:              "v1",
				Stability:            catalog.StabilityStable,
				InterfaceKind:        catalog.InterfaceKindDiscoveryREST,
				BackendKind:          catalog.BackendKindRawHTTP,
				Preferred:            true,
				RiskClass:            catalog.RiskClassRead,
				AuthStrategy:         catalog.AuthStrategyBYOOAuth,
				Scopes:               s.scopes,
				Binding: &catalog.Binding{
					BindingSchemaVersion: 1,
					AdapterKey:           "rest.raw-http",
					OperationKey:         s.opID,
					HTTP: &catalog.HTTPBinding{
						Method: s.httpMethod,
						Path:   s.httpPath,
					},
				},
			},
		},
	}
}

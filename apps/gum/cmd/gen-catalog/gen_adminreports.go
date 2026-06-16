package main

import "github.com/ehmo/gum/internal/catalog"

// Admin SDK Reports API (reports_v1) OAuth scopes.
const (
	scopeAdminReportsAudit = "https://www.googleapis.com/auth/admin.reports.audit.readonly"
	scopeAdminReportsUsage = "https://www.googleapis.com/auth/admin.reports.usage.readonly"
)

// BuildAdminReportsOps returns the Admin SDK Reports API read surface: audit
// activities + customer/user/entity usage reports. Served under service
// "adminreports" (distinct from the Directory "admin" service, which owns a
// different Discovery doc); discoveryMethodID maps adminreports.* to the
// Discovery's reports.* ids. typed-rest-sdk, byo_oauth, all read-class.
func BuildAdminReportsOps() []catalog.Op {
	op := func(opID, variantID, title, summary, scope, path, goCall string) catalog.Op {
		return makeWorkspaceOp(workspaceOpSpec{
			opID: opID, variantID: variantID, title: title, summary: summary,
			service: "adminreports", riskClass: catalog.RiskClassRead, scopes: []string{scope},
			httpMethod: "GET", httpPath: path,
			goPkg: "google.golang.org/api/admin/reports/v1", goCall: goCall,
		})
	}
	const base = "https://admin.googleapis.com/admin/reports/v1"
	return []catalog.Op{
		op("adminreports.activities.list", "adminreports.reports_v1.rest.activities.list", "List Audit Activities",
			"List audit-log activity events for a user + application (userKey=all or an email; applicationName=login|admin|drive|token|…).",
			scopeAdminReportsAudit, base+"/activity/users/{userKey}/applications/{applicationName}", "Activities.List"),
		op("adminreports.customerUsageReports.get", "adminreports.reports_v1.rest.customerUsageReports.get", "Get Customer Usage Report",
			"Fetch customer-level usage parameters for a date (YYYY-MM-DD).",
			scopeAdminReportsUsage, base+"/usage/dates/{date}", "CustomerUsageReports.Get"),
		op("adminreports.userUsageReport.get", "adminreports.reports_v1.rest.userUsageReport.get", "Get User Usage Report",
			"Fetch per-user usage parameters for a user (userKey=all or an email) on a date.",
			scopeAdminReportsUsage, base+"/usage/users/{userKey}/dates/{date}", "UserUsageReport.Get"),
		op("adminreports.entityUsageReports.get", "adminreports.reports_v1.rest.entityUsageReports.get", "Get Entity Usage Report",
			"Fetch usage parameters for an entity (e.g. gplus_communities) on a date.",
			scopeAdminReportsUsage, base+"/usage/{entityType}/{entityKey}/dates/{date}", "EntityUsageReports.Get"),
	}
}

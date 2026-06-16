package main

import (
	"github.com/ehmo/gum/internal/catalog"
)

// Google Search Console (formerly Webmasters API) operations.
//
// The modern host is searchconsole.googleapis.com but www.googleapis.com still
// routes /webmasters/v3/* correctly. URL Inspection lives under /v1/urlInspection
// which is ONLY served on searchconsole.googleapis.com, so we use absolute URLs
// throughout for consistency.
//
// OAuth scopes: webmasters.readonly for read ops, webmasters for write/destructive.
const (
	scopeWebmasters         = "https://www.googleapis.com/auth/webmasters"
	scopeWebmastersReadonly = "https://www.googleapis.com/auth/webmasters.readonly"
	searchConsoleHost       = "https://searchconsole.googleapis.com"
)

// BuildSearchConsoleOps returns the fixed set of Search Console Tier A operations:
//
//	read:        sites.list, sites.get, sitemaps.list, sitemaps.get,
//	             searchanalytics.query, urlInspection.index.inspect
//	write:       sites.add (PUT), sitemaps.submit (PUT)
//	destructive: sites.delete (DELETE), sitemaps.delete (DELETE)
//
// All ops use auth_strategy = byo_oauth (with ADC fallback at runtime per
// CompositeResolver). All ops use the typed-rest-sdk adapter and discovery-rest
// interface kind. Paths are absolute URLs to keep host routing explicit.
func BuildSearchConsoleOps() []catalog.Op {
	return []catalog.Op{
		makeSearchConsoleOp(searchConsoleSpec{
			opID:       "searchconsole.sites.list",
			variantID:  "searchconsole.v1.rest.sites.list",
			title:      "List Search Console properties",
			summary:    "List the verified sites the authenticated user has access to in Search Console.",
			httpMethod: "GET",
			httpPath:   searchConsoleHost + "/webmasters/v3/sites",
			risk:       catalog.RiskClassRead,
			scope:      scopeWebmastersReadonly,
			goCall:     "Sites.List",
		}),
		makeSearchConsoleOp(searchConsoleSpec{
			opID:          "searchconsole.sites.get",
			variantID:     "searchconsole.v1.rest.sites.get",
			title:         "Get a Search Console site",
			summary:       "Retrieve information about a specific verified Search Console site.",
			httpMethod:    "GET",
			httpPath:      searchConsoleHost + "/webmasters/v3/sites/{siteUrl}",
			risk:          catalog.RiskClassRead,
			scope:         scopeWebmastersReadonly,
			goCall:        "Sites.Get",
			requestFields: []catalog.RequestField{scPath("siteUrl")},
		}),
		makeSearchConsoleOp(searchConsoleSpec{
			opID:          "searchconsole.sites.add",
			variantID:     "searchconsole.v1.rest.sites.add",
			title:         "Add a Search Console site",
			summary:       "Add a site to the user's set of Search Console properties (initiates ownership verification).",
			httpMethod:    "PUT",
			httpPath:      searchConsoleHost + "/webmasters/v3/sites/{siteUrl}",
			risk:          catalog.RiskClassWrite,
			scope:         scopeWebmasters,
			goCall:        "Sites.Add",
			requestFields: []catalog.RequestField{scPath("siteUrl")},
		}),
		makeSearchConsoleOp(searchConsoleSpec{
			opID:          "searchconsole.sites.delete",
			variantID:     "searchconsole.v1.rest.sites.delete",
			title:         "Remove a Search Console site",
			summary:       "Remove a site from the user's Search Console properties.",
			httpMethod:    "DELETE",
			httpPath:      searchConsoleHost + "/webmasters/v3/sites/{siteUrl}",
			risk:          catalog.RiskClassDestructive,
			scope:         scopeWebmasters,
			goCall:        "Sites.Delete",
			requestFields: []catalog.RequestField{scPath("siteUrl")},
		}),
		makeSearchConsoleOp(searchConsoleSpec{
			opID:          "searchconsole.sitemaps.list",
			variantID:     "searchconsole.v1.rest.sitemaps.list",
			title:         "List submitted sitemaps",
			summary:       "List sitemaps submitted for a Search Console site.",
			httpMethod:    "GET",
			httpPath:      searchConsoleHost + "/webmasters/v3/sites/{siteUrl}/sitemaps",
			risk:          catalog.RiskClassRead,
			scope:         scopeWebmastersReadonly,
			goCall:        "Sitemaps.List",
			requestFields: []catalog.RequestField{scPath("siteUrl")},
		}),
		makeSearchConsoleOp(searchConsoleSpec{
			opID:          "searchconsole.sitemaps.get",
			variantID:     "searchconsole.v1.rest.sitemaps.get",
			title:         "Get a sitemap",
			summary:       "Retrieve information about a specific submitted sitemap.",
			httpMethod:    "GET",
			httpPath:      searchConsoleHost + "/webmasters/v3/sites/{siteUrl}/sitemaps/{feedpath}",
			risk:          catalog.RiskClassRead,
			scope:         scopeWebmastersReadonly,
			goCall:        "Sitemaps.Get",
			requestFields: []catalog.RequestField{scPath("siteUrl"), scPath("feedpath")},
		}),
		makeSearchConsoleOp(searchConsoleSpec{
			opID:          "searchconsole.sitemaps.submit",
			variantID:     "searchconsole.v1.rest.sitemaps.submit",
			title:         "Submit a sitemap",
			summary:       "Submit a sitemap for a Search Console site (resubmitting an existing path refreshes processing).",
			httpMethod:    "PUT",
			httpPath:      searchConsoleHost + "/webmasters/v3/sites/{siteUrl}/sitemaps/{feedpath}",
			risk:          catalog.RiskClassWrite,
			scope:         scopeWebmasters,
			goCall:        "Sitemaps.Submit",
			requestFields: []catalog.RequestField{scPath("siteUrl"), scPath("feedpath")},
		}),
		makeSearchConsoleOp(searchConsoleSpec{
			opID:          "searchconsole.sitemaps.delete",
			variantID:     "searchconsole.v1.rest.sitemaps.delete",
			title:         "Remove a sitemap",
			summary:       "Delete a submitted sitemap from a Search Console site.",
			httpMethod:    "DELETE",
			httpPath:      searchConsoleHost + "/webmasters/v3/sites/{siteUrl}/sitemaps/{feedpath}",
			risk:          catalog.RiskClassDestructive,
			scope:         scopeWebmasters,
			goCall:        "Sitemaps.Delete",
			requestFields: []catalog.RequestField{scPath("siteUrl"), scPath("feedpath")},
		}),
		makeSearchConsoleOp(searchConsoleSpec{
			opID:       "searchconsole.searchanalytics.query",
			variantID:  "searchconsole.v1.rest.searchanalytics.query",
			title:      "Query search analytics",
			summary:    "Query Search Analytics impressions, clicks, CTR and position. JSON request body required (see args.body): startDate, endDate, dimensions, filters, rowLimit, etc.",
			httpMethod: "POST",
			httpPath:   searchConsoleHost + "/webmasters/v3/sites/{siteUrl}/searchAnalytics/query",
			risk:       catalog.RiskClassRead,
			scope:      scopeWebmastersReadonly,
			goCall:     "Searchanalytics.Query",
			requestFields: []catalog.RequestField{
				scPath("siteUrl"),
				{Name: "startDate", Location: catalog.RequestFieldBody, Type: "string", Format: "date", Required: true, Description: "Start date, inclusive (YYYY-MM-DD)."},
				{Name: "endDate", Location: catalog.RequestFieldBody, Type: "string", Format: "date", Required: true, Description: "End date, inclusive (YYYY-MM-DD)."},
				{Name: "dimensions", Location: catalog.RequestFieldBody, Type: "array", ItemType: "string", Enum: []string{"date", "query", "page", "country", "device", "searchAppearance"}, Description: "Group results by these dimensions (repeatable)."},
				{Name: "type", Location: catalog.RequestFieldBody, Type: "string", Enum: []string{"web", "image", "video", "news", "discover", "googleNews"}, Description: "Search type to filter on."},
				{Name: "aggregationType", Location: catalog.RequestFieldBody, Type: "string", Enum: []string{"auto", "byPage", "byProperty", "byNewsShowcasePanel"}, Description: "How data is aggregated."},
				{Name: "dataState", Location: catalog.RequestFieldBody, Type: "string", Enum: []string{"all", "final", "hourly_all"}, Description: "Whether to include fresh/partial data."},
				{Name: "rowLimit", Location: catalog.RequestFieldBody, Type: "integer", Format: "int64", Default: "1000", Description: "Maximum rows to return (1-25000)."},
				{Name: "startRow", Location: catalog.RequestFieldBody, Type: "integer", Format: "int64", Default: "0", Description: "Zero-based first-row index for pagination."},
				{Name: "dimensionFilterGroups", Location: catalog.RequestFieldBody, Type: "array", ItemType: "object", Description: "Filter groups (nested; pass as JSON via dimensionFilterGroups:=...)."},
			},
		}),
		makeSearchConsoleOp(searchConsoleSpec{
			opID:       "searchconsole.urlInspection.index.inspect",
			variantID:  "searchconsole.v1.rest.urlInspection.index.inspect",
			title:      "Inspect URL in Search Console",
			summary:    "Inspect an indexed URL using the Search Console URL Inspection API. Requires JSON request body (args.body): inspectionUrl, siteUrl, languageCode (optional).",
			httpMethod: "POST",
			httpPath:   searchConsoleHost + "/v1/urlInspection/index:inspect",
			risk:       catalog.RiskClassRead,
			scope:      scopeWebmastersReadonly,
			goCall:     "UrlInspection.Index.Inspect",
			requestFields: []catalog.RequestField{
				// siteUrl is a BODY field here (the path has no {siteUrl}) — the
				// location, not the name, decides routing.
				{Name: "inspectionUrl", Location: catalog.RequestFieldBody, Type: "string", Required: true, Description: "Fully-qualified URL to inspect."},
				{Name: "siteUrl", Location: catalog.RequestFieldBody, Type: "string", Required: true, Description: "Verified property the URL belongs to."},
				{Name: "languageCode", Location: catalog.RequestFieldBody, Type: "string", Description: "BCP-47 language code for the result (optional)."},
			},
		}),
	}
}

// searchConsoleSpec is the internal shape used to declare a Search Console op.
type searchConsoleSpec struct {
	opID          string
	variantID     string
	title         string
	summary       string
	httpMethod    string
	httpPath      string
	risk          catalog.RiskClass
	scope         string
	goCall        string
	requestFields []catalog.RequestField
}

// scPath builds a required path-parameter RequestField (substituted into the
// URL template, e.g. {siteUrl}).
func scPath(name string) catalog.RequestField {
	return catalog.RequestField{Name: name, Location: catalog.RequestFieldPath, Type: "string", Required: true}
}

// makeSearchConsoleOp builds a catalog.Op for one Search Console operation with
// the conventions shared across all 10 ops.
func makeSearchConsoleOp(s searchConsoleSpec) catalog.Op {
	return catalog.Op{
		OpID:             s.opID,
		OpSchemaVersion:  1,
		Title:            s.title,
		Summary:          s.summary,
		Service:          "searchconsole",
		ServiceFamily:    "search",
		DefaultVariantID: s.variantID,
		RequestFields:    s.requestFields,
		Variants: []catalog.Variant{
			{
				VariantID:            s.variantID,
				VariantSchemaVersion: 1,
				Version:              "v1",
				Stability:            catalog.StabilityStable,
				InterfaceKind:        catalog.InterfaceKindDiscoveryREST,
				BackendKind:          catalog.BackendKindTypedRestSDK,
				Preferred:            true,
				RiskClass:            s.risk,
				AuthStrategy:         catalog.AuthStrategyBYOOAuth,
				Scopes:               []string{s.scope},
				Binding: &catalog.Binding{
					BindingSchemaVersion: 1,
					AdapterKey:           "rest.typed-rest-sdk",
					OperationKey:         s.opID,
					HTTP: &catalog.HTTPBinding{
						Method: s.httpMethod,
						Path:   s.httpPath,
					},
					GoPkg:  "google.golang.org/api/searchconsole/v1",
					GoCall: s.goCall,
				},
			},
		},
	}
}

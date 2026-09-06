package main

import "github.com/ehmo/gum/internal/catalog"

// Google Ads API OAuth scope. `adwords` is a Google *restricted* scope, so it
// can never be served by the managed gum_oauth client — these ops are byo_oauth
// (the operator registers their own Desktop OAuth client whose Cloud project has
// the Google Ads API enabled and the adwords scope on its consent screen).
const scopeAdwords = "https://www.googleapis.com/auth/adwords"

// BuildGoogleAdsOps returns the Google Ads API ops: three read-only Keyword
// Planner methods (ideas, historical metrics, forecast metrics) plus the two
// GoogleAdsService methods that reach the account itself, googleAds:search
// (GAQL reporting, read) and googleAds:mutate (create/update/remove across
// supported resource type, destructive).
//
// Conversion uploads report offline events for eligible developer tokens.
// All six are byo_oauth (scope=adwords) and backed by the dedicated
// google-ads-sdk adapter (internal/adapters/googleads), which injects the
// secret developer-token header server-side so it never travels as an
// invocation arg.
//
// login-customer-id (the manager/MCC id) and customer_id (the queried account)
// are non-secret args; the developer token is sourced from the keychain/env.
func BuildGoogleAdsOps() []catalog.Op {
	// Shared request fields.
	customerID := catalog.RequestField{
		Name: "customerId", Location: catalog.RequestFieldPath, Type: "string", Required: true,
		Description: "The 10-digit Google Ads account id to query, e.g. 1234567890 (dashes allowed). Under a manager account this is the client account, not the manager.",
	}
	loginCustomerID := catalog.RequestField{
		Name: "loginCustomerId", Location: catalog.RequestFieldArg, Type: "string",
		Description: "Manager (MCC) account id, sent as the login-customer-id header. Required when the account is accessed through a manager account.",
	}
	// geo and language declare no Default: omitting either returns all-locations
	// / all-languages data, and a declared default is sent for real (gum-3gcv),
	// so declaring one here would silently narrow every unqualified query to the
	// US and to English.
	geo := catalog.RequestField{
		Name: "geoTargetConstants", Location: catalog.RequestFieldArg, Type: "array", ItemType: "string",
		Description: "Geo targets as resource names or bare ids (e.g. geoTargetConstants/2840 or 2840 for the US). Repeatable. Omit for all locations.",
	}
	language := catalog.RequestField{
		Name: "language", Location: catalog.RequestFieldArg, Type: "string",
		Description: "Language as a resource name or bare id (languageConstants/1000 or 1000 for English). Omit for all languages.",
	}
	network := catalog.RequestField{
		Name: "keywordPlanNetwork", Location: catalog.RequestFieldArg, Type: "string",
		Enum: []string{"GOOGLE_SEARCH", "GOOGLE_SEARCH_AND_PARTNERS"}, Default: "GOOGLE_SEARCH",
		Description: "Search network to estimate against.",
	}
	keywords := func(required bool, desc string) catalog.RequestField {
		return catalog.RequestField{
			Name: "keywords", Location: catalog.RequestFieldArg, Type: "array", ItemType: "string", Required: required,
			Description: desc,
		}
	}

	ideas := makeGoogleAdsOp(
		"googleads.keywordPlanIdeas.generateKeywordIdeas",
		"googleads.v24.rest.keywordPlanIdeas.generateKeywordIdeas",
		"Generate keyword ideas",
		"Discover new keyword ideas with monthly search volume, competition, and top-of-page bid ranges from a seed of keywords and/or a landing-page URL. Needs `keywords` and/or `url`.",
		"generateKeywordIdeas",
		"googleads.keyword_ideas.v1",
		[]catalog.RequestField{
			customerID, loginCustomerID,
			keywords(false, "Seed keywords (repeatable). Provide `keywords` and/or `url`."),
			{Name: "url", Location: catalog.RequestFieldArg, Type: "string", Description: "Seed landing-page URL. Provide `keywords` and/or `url`."},
			geo, language, network,
			{Name: "pageSize", Location: catalog.RequestFieldArg, Type: "integer", Format: "int32", Description: "Max ideas to return per page."},
			{Name: "includeAdultKeywords", Location: catalog.RequestFieldArg, Type: "boolean", Default: "false", Description: "Include adult keywords in the results."},
		},
	)

	historical := makeGoogleAdsOp(
		"googleads.keywordPlanIdeas.generateKeywordHistoricalMetrics",
		"googleads.v24.rest.keywordPlanIdeas.generateKeywordHistoricalMetrics",
		"Generate keyword historical metrics",
		"Fetch historical metrics (average monthly searches, per-month search volumes, competition index, top-of-page bid ranges) for a fixed list of keywords. Needs `keywords`.",
		"generateKeywordHistoricalMetrics",
		"googleads.keyword_historical.v1",
		[]catalog.RequestField{
			customerID, loginCustomerID,
			keywords(true, "Keywords to fetch historical metrics for (repeatable)."),
			geo, language, network,
		},
	)

	forecast := makeGoogleAdsOp(
		"googleads.keywordPlanIdeas.generateKeywordForecastMetrics",
		"googleads.v24.rest.keywordPlanIdeas.generateKeywordForecastMetrics",
		"Generate keyword forecast metrics",
		"Forecast clicks, impressions, cost, and CTR for a list of keywords over a future date range at a given max CPC. Needs `keywords`; for full campaign control pass a raw `body`.",
		"generateKeywordForecastMetrics",
		"", // forecast response is already compact (campaignForecastMetrics)
		[]catalog.RequestField{
			customerID, loginCustomerID,
			keywords(true, "Keywords to forecast (repeatable)."),
			geo, language, network,
			{Name: "maxCpcMicros", Location: catalog.RequestFieldArg, Type: "integer", Format: "int64", Default: "1000000", Description: "Max CPC bid in micros (1000000 = $1.00)."},
			{Name: "matchType", Location: catalog.RequestFieldArg, Type: "string", Enum: []string{"BROAD", "PHRASE", "EXACT"}, Default: "BROAD", Description: "Keyword match type for the forecast."},
			{Name: "forecastStartDate", Location: catalog.RequestFieldArg, Type: "string", Format: "date", Description: "Forecast window start (YYYY-MM-DD). Defaults to tomorrow."},
			{Name: "forecastEndDate", Location: catalog.RequestFieldArg, Type: "string", Format: "date", Description: "Forecast window end (YYYY-MM-DD). Defaults to 30 days out."},
		},
	)

	// GoogleAdsService.search runs GAQL against the account. One op replaces a
	// per-report catalog surface: the query text selects the resource, the
	// fields, and the date range, so campaign spend, ad-group performance, and
	// the search-terms report all arrive through this single binding.
	search := buildGoogleAdsOp(
		"googleads.googleAds.search",
		"googleads.v24.rest.googleAds.search",
		"Search with GAQL",
		"Run a Google Ads Query Language (GAQL) query against an account and return the matching rows. Use it for campaign, ad group, keyword, search-term, and spend reporting. Needs `query`.",
		"search",
		"",
		"https://googleads.googleapis.com/v24/customers/{customerId}/googleAds:search",
		catalog.RiskClassRead,
		[]catalog.RequestField{
			customerID, loginCustomerID,
			{
				Name: "query", Location: catalog.RequestFieldArg, Type: "string", Required: true,
				Description: "GAQL query, e.g. SELECT campaign.name, metrics.cost_micros FROM campaign WHERE segments.date DURING LAST_7_DAYS.",
			},
			// page_size stopped being configurable in v19: the API always
			// returns 10000 rows per page, so only the token is exposed.
			{Name: "pageToken", Location: catalog.RequestFieldArg, Type: "string", Description: "nextPageToken from a previous response, to fetch the next 10000 rows."},
		},
	)

	// GoogleAdsService.mutate is one atomic, ordered batch across resource
	// types, which is what campaign creation needs: a budget, the campaign that
	// references it, its ad groups, keywords, and ads all land in one request
	// via temporary resource ids (customers/X/campaignBudgets/-1).
	//
	// A batch can permanently remove resources. The risk class covers the
	// whole endpoint, so every batch requires destructive confirmation.
	mutate := buildGoogleAdsOp(
		"googleads.googleAds.mutate",
		"googleads.v24.rest.googleAds.mutate",
		"Mutate resources",
		"Create, update, or remove Google Ads resources in an ordered batch, atomic unless partialFailure=true. Needs `mutateOperations` and destructive confirmation because removals are permanent. Pass validateOnly=true to check a batch without applying it.",
		"mutate",
		"",
		"https://googleads.googleapis.com/v24/customers/{customerId}/googleAds:mutate",
		catalog.RiskClassDestructive,
		[]catalog.RequestField{
			customerID, loginCustomerID,
			{
				Name: "mutateOperations", Location: catalog.RequestFieldArg, Type: "array", ItemType: "object", Required: true,
				Description: "Ordered MutateOperation objects, e.g. {\"campaignBudgetOperation\":{\"create\":{...}}}. Later operations may reference earlier ones by temporary resource id (a negative id such as customers/123/campaignBudgets/-1).",
			},
			{
				Name: "validateOnly", Location: catalog.RequestFieldArg, Type: "boolean", Default: "false",
				Description: "Validate the batch and return errors without writing anything. Run every new batch this way first.",
			},
			{
				Name: "partialFailure", Location: catalog.RequestFieldArg, Type: "boolean", Default: "false",
				Description: "Apply the operations that succeed instead of failing the whole batch. Leave false to keep the batch atomic.",
			},
			{
				Name: "responseContentType", Location: catalog.RequestFieldArg, Type: "string",
				Enum: []string{"RESOURCE_NAME_ONLY", "MUTABLE_RESOURCE"}, Default: "RESOURCE_NAME_ONLY",
				Description: "RESOURCE_NAME_ONLY returns resource names; MUTABLE_RESOURCE returns the written resources.",
			},
		},
	)

	// ConversionUploadService.UploadClickConversions reports conversions that
	// happened away from the browser. Google restricts this endpoint to existing
	// upload users; new integrations must use the Data Manager API.
	//
	// partialFailure is not an arg: Google documents it as required for this
	// method, so the adapter sets it. Per-conversion errors come back in
	// partialFailureError, not as a failed request.
	uploads := buildGoogleAdsOp(
		"googleads.conversionUploads.uploadClickConversions",
		"googleads.v24.rest.conversionUploads.uploadClickConversions",
		"Upload click conversions",
		"Report offline conversions for eligible existing developer tokens. Needs `conversions`; set conversionAction, conversionDateTime, and a supported click id or user identifiers. Always inspect partialFailureError, even after HTTP success. New integrations must use the Data Manager API.",
		"uploadClickConversions",
		"",
		"https://googleads.googleapis.com/v24/customers/{customerId}:uploadClickConversions",
		catalog.RiskClassWrite,
		[]catalog.RequestField{
			customerID, loginCustomerID,
			{
				Name: "conversions", Location: catalog.RequestFieldArg, Type: "array", ItemType: "object", Required: true,
				Description: "ClickConversion objects with conversionAction, conversionDateTime (including timezone), and click ids or userIdentifiers. Optional fields include conversionValue, currencyCode, orderId, and consent. Google permits each orderId once per conversion action; inspect partialFailureError for rejected entries.",
			},
			{
				Name: "validateOnly", Location: catalog.RequestFieldArg, Type: "boolean", Default: "false",
				Description: "Validate the batch and return errors without recording anything.",
			},
		},
	)

	return []catalog.Op{ideas, historical, forecast, search, mutate, uploads}
}

// makeGoogleAdsOp builds a read-only Google Ads Keyword Planner op bound to the
// google-ads-sdk adapter. method is the REST custom-method suffix (e.g.
// "generateKeywordIdeas"); it drives both the adapter_key and the binding path,
// which for Keyword Planner hangs straight off the customer resource.
// outputProfile names the catalog-embedded expression profile applied at step 8
// (empty for ops whose raw response is already compact).
func makeGoogleAdsOp(opID, variantID, title, summary, method, outputProfile string, fields []catalog.RequestField) catalog.Op {
	return buildGoogleAdsOp(
		opID, variantID, title, summary, method, outputProfile,
		"https://googleads.googleapis.com/v24/customers/{customerId}:"+method,
		catalog.RiskClassRead, fields,
	)
}

// buildGoogleAdsOp is the shared shape for every google-ads-sdk op. path is the
// full REST binding path including the {customerId} template; the adapter
// resolves it from the customers segment onward, so a service sub-resource
// (".../customers/{customerId}/googleAds:search") works alongside a custom
// method hung off the customer itself.
func buildGoogleAdsOp(
	opID, variantID, title, summary, method, outputProfile, path string,
	risk catalog.RiskClass, fields []catalog.RequestField,
) catalog.Op {
	return catalog.Op{
		OpID:             opID,
		OpSchemaVersion:  1,
		Title:            title,
		Summary:          summary,
		Service:          "googleads",
		ServiceFamily:    "googleads",
		DefaultVariantID: variantID,
		Variants: []catalog.Variant{
			{
				VariantID:            variantID,
				VariantSchemaVersion: 1,
				Version:              "v24",
				Stability:            catalog.StabilityStable,
				InterfaceKind:        catalog.InterfaceKindDiscoveryREST,
				BackendKind:          catalog.BackendKindGoogleAdsSDK,
				Preferred:            true,
				RiskClass:            risk,
				AuthStrategy:         catalog.AuthStrategyBYOOAuth,
				Scopes:               []string{scopeAdwords},
				OutputProfile:        outputProfile,
				Binding: &catalog.Binding{
					BindingSchemaVersion: 1,
					AdapterKey:           "googleads." + method,
					OperationKey:         opID,
					HTTP: &catalog.HTTPBinding{
						Method: "POST",
						Path:   path,
					},
				},
			},
		},
		RequestFields: fields,
	}
}

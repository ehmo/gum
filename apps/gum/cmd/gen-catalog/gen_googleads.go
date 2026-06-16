package main

import "github.com/ehmo/gum/internal/catalog"

// Google Ads API OAuth scope. `adwords` is a Google *restricted* scope, so it
// can never be served by the managed gum_oauth client — these ops are byo_oauth
// (the operator registers their own Desktop OAuth client whose Cloud project has
// the Google Ads API enabled and the adwords scope on its consent screen).
const scopeAdwords = "https://www.googleapis.com/auth/adwords"

// BuildGoogleAdsOps returns the Google Ads API Keyword Planner ops: keyword
// ideas, historical metrics, and forecast metrics. All three are read-only,
// byo_oauth (scope=adwords), and backed by the dedicated google-ads-sdk adapter
// (internal/adapters/googleads), which injects the secret developer-token header
// server-side so it never travels as an invocation arg.
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
	geo := catalog.RequestField{
		Name: "geoTargetConstants", Location: catalog.RequestFieldArg, Type: "array", ItemType: "string", Default: "geoTargetConstants/2840",
		Description: "Geo targets as resource names or bare ids (e.g. geoTargetConstants/2840 or 2840 for the US). Repeatable. Omit for all locations.",
	}
	language := catalog.RequestField{
		Name: "language", Location: catalog.RequestFieldArg, Type: "string", Default: "languageConstants/1000",
		Description: "Language as a resource name or bare id (languageConstants/1000 or 1000 for English).",
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

	return []catalog.Op{ideas, historical, forecast}
}

// makeGoogleAdsOp builds a read-only Google Ads Keyword Planner op bound to the
// google-ads-sdk adapter. method is the REST custom-method suffix (e.g.
// "generateKeywordIdeas"); it drives both the adapter_key and the binding path.
// outputProfile names the catalog-embedded expression profile applied at step 8
// (empty for ops whose raw response is already compact).
func makeGoogleAdsOp(opID, variantID, title, summary, method, outputProfile string, fields []catalog.RequestField) catalog.Op {
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
				RiskClass:            catalog.RiskClassRead,
				AuthStrategy:         catalog.AuthStrategyBYOOAuth,
				Scopes:               []string{scopeAdwords},
				OutputProfile:        outputProfile,
				Binding: &catalog.Binding{
					BindingSchemaVersion: 1,
					AdapterKey:           "googleads." + method,
					OperationKey:         opID,
					HTTP: &catalog.HTTPBinding{
						Method: "POST",
						Path:   "https://googleads.googleapis.com/v24/customers/{customerId}:" + method,
					},
				},
			},
		},
		RequestFields: fields,
	}
}

package main

import "github.com/ehmo/gum/internal/catalog"

// Data Manager uses a sensitive OAuth scope. gum requires the operator's own
// Desktop OAuth client, with the Data Manager API enabled in its Cloud project.
const scopeDataManager = "https://www.googleapis.com/auth/datamanager"

const dataManagerBase = "https://datamanager.googleapis.com/v1"

// BuildDataManagerOps returns the Data Manager API v1 ops gum needs to report
// offline conversions and then check what Google did with them.
//
// Data Manager replaces ConversionUploadService.UploadClickConversions for
// every integration that did not already use it: that endpoint answers
// CUSTOMER_NOT_ALLOWLISTED_FOR_THIS_FEATURE for a new caller. The same
// conversion action id travels as productDestinationId and the same gclid
// travels under adIdentifiers.
//
// Ingestion is asynchronous. A 200 means Google accepted the request, not that
// it accepted every event. Check live-upload request IDs with
// datamanager.requestStatus.retrieve after 30 minutes; validation has no status.
//
// typed-rest-sdk, byo_oauth. No developer token: unlike the googleads ops this
// is a plain OAuth REST API.
func BuildDataManagerOps() []catalog.Op {
	ingest := makeDataManagerOp(
		"datamanager.events.ingest",
		"datamanager.v1.rest.events.ingest",
		"Ingest events",
		"Send conversion events to a Google Ads or Display & Video 360 account (args.body: destinations[] with operatingAccount {accountType:GOOGLE_ADS, accountId} and productDestinationId = the conversion action id, plus events[] with eventTimestamp, eventSource, adIdentifiers {gclid|gbraid|wbraid}, conversionValue, currency, and transactionId). At most 2000 events per request. Set validateOnly=true in the body to check a batch without recording it. For live uploads, check the returned requestId with datamanager.requestStatus.retrieve after 30 minutes; processing can take 24 hours. Status lookup is unavailable for validateOnly requests.",
		catalog.RiskClassWrite,
		"POST", dataManagerBase+"/events:ingest",
		"Events.Ingest",
		[]catalog.RequestField{
			{
				Name: "destinations", Location: catalog.RequestFieldBody, Type: "array", ItemType: "object", Required: true,
				Description: "Where the events go. Each entry carries operatingAccount {accountType, accountId} and productDestinationId, which for Google Ads is the conversion action id.",
			},
			{
				Name: "events", Location: catalog.RequestFieldBody, Type: "array", ItemType: "object", Required: true,
				Description: "The events to send. At most 2000 per request. Each carries eventTimestamp, eventSource (WEB, APP, IN_STORE, PHONE, MESSAGE, or OTHER), adIdentifiers {gclid|gbraid|wbraid}, conversionValue, currency, and transactionId.",
			},
			{
				Name: "consent", Location: catalog.RequestFieldBody, Type: "object",
				Description: "Request-level consent applied to every user in the request. A user-level consent inside an event overrides it.",
			},
			{
				Name: "validateOnly", Location: catalog.RequestFieldBody, Type: "boolean",
				Description: "Validate the batch without recording it. Status lookup is unavailable for validation requests.",
			},
			{
				Name: "encoding", Location: catalog.RequestFieldBody, Type: "string",
				Enum:        []string{"ENCODING_UNSPECIFIED", "HEX", "BASE64"},
				Description: "Encoding of hashed user identifiers. Required only for userData uploads, not for click-id conversions.",
			},
			{
				Name: "encryptionInfo", Location: catalog.RequestFieldBody, Type: "object",
				Description: "Encryption details for userData uploads. Omit when identifiers are hashed but not encrypted.",
			},
		},
	)

	status := makeDataManagerOp(
		"datamanager.requestStatus.retrieve",
		"datamanager.v1.rest.requestStatus.retrieve",
		"Retrieve request status",
		"Read what Google did with an earlier Data Manager request. Needs the requestId returned by a live ingest call. Wait 30 minutes before checking; processing can take 24 hours. Not available for validateOnly requests. Returns per-destination ingestion status, error info, and warnings.",
		catalog.RiskClassRead,
		"GET", dataManagerBase+"/requestStatus:retrieve",
		"RequestStatus.Retrieve",
		[]catalog.RequestField{
			{
				Name: "requestId", Location: catalog.RequestFieldQuery, Type: "string", Required: true,
				Description: "The requestId returned by an ingest call.",
			},
		},
	)

	return []catalog.Op{ingest, status}
}

// makeDataManagerOp mirrors makeWorkspaceOp but keeps the ops in the googleads
// service family, because the account they write to is a Google Ads account and
// the surface is only useful next to the googleads ops.
func makeDataManagerOp(
	opID, variantID, title, summary string,
	risk catalog.RiskClass,
	httpMethod, httpPath, goCall string,
	fields []catalog.RequestField,
) catalog.Op {
	return catalog.Op{
		OpID:             opID,
		OpSchemaVersion:  1,
		Title:            title,
		Summary:          summary,
		Service:          "datamanager",
		ServiceFamily:    "googleads",
		DefaultVariantID: variantID,
		Variants: []catalog.Variant{
			{
				VariantID:            variantID,
				VariantSchemaVersion: 1,
				Version:              "v1",
				Stability:            catalog.StabilityStable,
				InterfaceKind:        catalog.InterfaceKindDiscoveryREST,
				BackendKind:          catalog.BackendKindTypedRestSDK,
				Preferred:            true,
				RiskClass:            risk,
				AuthStrategy:         catalog.AuthStrategyBYOOAuth,
				Scopes:               []string{scopeDataManager},
				Binding: &catalog.Binding{
					BindingSchemaVersion: 1,
					AdapterKey:           "rest.typed-rest-sdk",
					OperationKey:         opID,
					HTTP: &catalog.HTTPBinding{
						Method: httpMethod,
						Path:   httpPath,
					},
					GoPkg:  "google.golang.org/api/datamanager/v1",
					GoCall: goCall,
				},
			},
		},
		RequestFields: fields,
	}
}

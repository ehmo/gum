---
title: Google Ads
description: "Google Ads operations in gum's generated catalog."
service_group: "Ads and maps"
---

# Google Ads

Google Ads has 6 operations in gum's generated catalog. Start with search when you know the task, use describe to inspect request fields and scopes, then dispatch through the command that matches the operation risk class.

| Count | Value |
| --- | --- |
| Family | Ads and maps |
| Operations | 6 |
| Risk classes | 1 destructive, 4 read, 1 write |
| Auth strategies | 6 byo_oauth |

## Start here

```bash
gum search "google ads"
gum describe googleads.googleAds.search
gum read googleads.googleAds.search --args '{"customerId":"<customerId>","query":"<query>"}' --output json
```

For write-class operations, gum requires the write command and an explicit write gate:

```bash
gum describe googleads.conversionUploads.uploadClickConversions
gum write googleads.conversionUploads.uploadClickConversions --allow-write --args '{"customerId":"<customerId>","conversions":[]}'
```

For destructive operations, run the call once for a confirmation envelope, review the target, then retry with the returned token:

```bash
gum destructive googleads.googleAds.mutate --args '{"customerId":"<customerId>","mutateOperations":[]}'
gum destructive googleads.googleAds.mutate --args '{"customerId":"<customerId>","mutateOperations":[]}' --confirmed --token '<confirmation_token>'
```

## Auth

Auth strategies in this service: 6 byo_oauth. Authenticate the strategy used by the operation you plan to call.

### Bring-your-own OAuth

1. In Google Cloud, enable Google Ads API.
2. Configure the OAuth consent screen. Add your Google account as a test user when the app is still in testing mode.
3. Create an OAuth client ID with application type `Desktop app`.
4. Add the scopes this service needs to the consent screen.
5. Store the client in gum:

```bash
printf '%s' "$GOOGLE_OAUTH_CLIENT_SECRET" \
  | gum auth use-oauth-client --client-id "$GOOGLE_OAUTH_CLIENT_ID" --secret-stdin
```

6. Store the Google Ads developer token:

```bash
printf '%s' "$GOOGLE_ADS_DEVELOPER_TOKEN" | gum auth use-ads-developer-token --stdin
```

7. Authorize this service:

```bash
gum login --service googleads
```

8. Verify the grant before dispatch:

```bash
gum auth status --scopes adwords
gum describe googleads.googleAds.search
```

Scopes used by these operations:

- `https://www.googleapis.com/auth/adwords`

Service setup notes: [Google Ads auth guide](../auth-guides/google-ads.md).

## Operations

| Operation | Risk | Auth | Summary |
| --- | --- | --- | --- |
| `googleads.conversionUploads.uploadClickConversions` | `write` | `byo_oauth` | Report offline conversions for eligible existing developer tokens. Needs `conversions`; set conversionAction, conversionDateTime, and a supported click id or user identifiers. Always inspect partialFailureError, even after HTTP success. New integrations must use the Data Manager API. |
| `googleads.googleAds.mutate` | `destructive` | `byo_oauth` | Create, update, or remove Google Ads resources in an ordered batch, atomic unless partialFailure=true. Needs `mutateOperations` and destructive confirmation because removals are permanent. Pass validateOnly=true to check a batch without applying it. |
| `googleads.googleAds.search` | `read` | `byo_oauth` | Run a Google Ads Query Language (GAQL) query against an account and return the matching rows. Use it for campaign, ad group, keyword, search-term, and spend reporting. Needs `query`. |
| `googleads.keywordPlanIdeas.generateKeywordForecastMetrics` | `read` | `byo_oauth` | Forecast clicks, impressions, cost, and CTR for a list of keywords over a future date range at a given max CPC. Needs `keywords`; for full campaign control pass a raw `body`. |
| `googleads.keywordPlanIdeas.generateKeywordHistoricalMetrics` | `read` | `byo_oauth` | Fetch historical metrics (average monthly searches, per-month search volumes, competition index, top-of-page bid ranges) for a fixed list of keywords. Needs `keywords`. |
| `googleads.keywordPlanIdeas.generateKeywordIdeas` | `read` | `byo_oauth` | Discover new keyword ideas with monthly search volume, competition, and top-of-page bid ranges from a seed of keywords and/or a landing-page URL. Needs `keywords` and/or `url`. |

## Next

- Use [API workflows](../api-workflows.md) for search, describe, invoke, and error handling.
- Use [Auth guides](../auth-guides/README.md) for service-specific Google setup.
- Use [Command index](../commands/README.md) for CLI flags and generated help.

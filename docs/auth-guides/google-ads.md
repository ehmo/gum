# Google Ads

Enable the Google Ads API in Google Cloud. You also need a Google Ads developer
token and a customer ID that the signed-in account can access.

OAuth scope:

- `https://www.googleapis.com/auth/adwords`

Setup:

```shell
printf '%s' "$GOOGLE_ADS_DEVELOPER_TOKEN" | gum auth use-ads-developer-token --stdin
gum login --service googleads
gum read googleads.keywordPlanIdeas.generateKeywordHistoricalMetrics --args '{"customerId":"<customer-id>","keywords":["gum"]}'
```

Google Ads can reject requests after OAuth if the developer token is pending,
the customer ID is wrong, or the account lacks access to the customer.

## Reporting and mutations

Use `googleads.googleAds.search` for GAQL queries. Pass `pageToken` from the
previous response to fetch another page. Google Ads fixes the page size;
narrow the selected fields or query if a response exceeds gum's size limit.

`googleads.googleAds.mutate` accepts ordered `mutateOperations`. It requires
`gum destructive` and the normal confirmation token flow for every batch,
including validation runs. Google cannot restore removed campaigns, so the
endpoint uses the destructive risk class. Set `validateOnly:true` to validate
without applying changes. Set it to false only for the batch you intend to
apply, and obtain a new confirmation token for that changed request.
[Google's removal guidance](https://support.google.com/google-ads/answer/2404259).

Mutation requests accept the declared top-level fields; `body` is rejected.
Boolean options must be valid booleans. gum sends each write once and does not
retry it after a server error. Check the account state before resubmitting a
request whose outcome is uncertain.

## Offline conversions

`googleads.conversionUploads.uploadClickConversions` accepts `conversions`
through `gum write --allow-write`. Set `validateOnly:true` to validate an
upload without recording it. gum always sends `partialFailure:true`.
Inspect `partialFailureError` even when the HTTP request succeeds; successful
rows can coexist with rejected rows. The same rule applies to mutation batches
using `partialFailure:true`.

Since June 15, 2026, Google restricts offline uploads for developer tokens with
no upload requests between December 17, 2025 and June 15, 2026. Those tokens
receive `CUSTOMER_NOT_ALLOWLISTED_FOR_THIS_FEATURE`. Google directs new
integrations to the Data Manager API.
[Google's access restriction](https://developers.google.com/google-ads/api/docs/deprecations).

The v24 upload request has no `debugEnabled` field. gum does not expose it.
[Google's v24 request schema](https://github.com/googleapis/googleapis/blob/master/google/ads/googleads/v24/services/conversion_upload_service.proto).

## Data Manager

Use `datamanager.events.ingest` for new offline conversion integrations. Enable
Data Manager API in the Google Cloud project that owns your OAuth client.
Authorize its separate sensitive scope; no Google Ads developer token is
required for these operations.
[Google's access setup](https://developers.google.com/data-manager/api/devguides/quickstart/set-up-access).

```shell
gum login --service datamanager
gum auth probe --scopes https://www.googleapis.com/auth/datamanager
gum describe datamanager.events.ingest
```

Pass a JSON object under `body` with `destinations`, `events`, and
`validateOnly:true`. For Google Ads, each destination uses
`operatingAccount:{accountType:"GOOGLE_ADS",accountId:"<customer-id>"}` and
`productDestinationId:"<conversion-action-id>"`. If access goes through a
manager, include the applicable `loginAccount` too.

Each event needs an RFC 3339 `eventTimestamp`. Include `eventSource` such as
`WEB` or `APP`, the applicable click identifier under `adIdentifiers`, and
`conversionValue`, `currency`, and `transactionId` when relevant. Google
rejected a validation request without `eventSource` in the release check.
A request can contain at most 2,000 events.
[Google's request schema](https://developers.google.com/data-manager/api/reference/rest/v1/events/ingest).

```shell
gum write datamanager.events.ingest --allow-write --format json --args '{"body":{"destinations":[{"operatingAccount":{"accountType":"GOOGLE_ADS","accountId":"<customer-id>"},"productDestinationId":"<conversion-action-id>"}],"events":[{"eventTimestamp":"<RFC-3339-time>","eventSource":"WEB","adIdentifiers":{"gclid":"<click-id>"},"conversionValue":0,"currency":"USD","transactionId":"<unique-event-id>"}],"validateOnly":true}}'
```

The `gum call` convenience form also accepts flat body fields. Conflicting
boolean values between flat fields and an explicit body fail locally. For
`gum write` and MCP, keep body fields inside the `body` object.

A live upload returns a request ID before processing finishes. Inspect
`fieldWarnings`, then wait 30 minutes before reading
`datamanager.requestStatus.retrieve` with that `requestId`. Inspect every
entry in `requestStatusPerDestination`, including errors and warnings.
Processing can take up to 24 hours. Google does not provide status diagnostics
for `validateOnly:true` requests.
[Google's diagnostics workflow](https://developers.google.com/data-manager/api/devguides/diagnostics).

The REST executor does not replay POST or PATCH after a transport failure,
response-read failure, or server error. An explicit 429 rejection permits one
retry. Check request state before resubmitting an upload with an uncertain
outcome. Error messages preserve the request ID and up to five field
violations, followed by an omitted count when needed.

Sources checked September 7, 2026.

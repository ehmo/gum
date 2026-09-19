# Google Ads and Data Manager

For offline conversion ingestion through Data Manager, use the
[Data Manager setup](#data-manager) below. The Google Ads API setup in the next
section requires a developer token; Data Manager uses its own OAuth scope.

## Google Ads API

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

### Default account

Every Google Ads operation needs `customerId`. Access through a manager account
also needs `loginCustomerId`. Store defaults in the profile config so calls can
omit both:

```shell
gum config set googleads.customer_id=<customer-id>
gum config set googleads.login_customer_id=<manager-customer-id>
gum read googleads.keywordPlanIdeas.generateKeywordHistoricalMetrics --args '{"keywords":["gum"]}'
```

`GUM_GOOGLE_ADS_CUSTOMER_ID` and `GUM_GOOGLE_ADS_LOGIN_CUSTOMER_ID` set the
same defaults from the environment. An argument in the call wins over the
environment, and the environment wins over the profile config. To send one call
without the manager default, pass `"loginCustomerId":""`.

A default must hold exactly 10 digits; dashes and spaces are allowed. A
malformed default fails the call with `INVALID_ARGS` and names the variable or
config key that holds it. With no argument and no default, the error names both
ways to set one. Defaults belong to one profile, so set them with `--profile`
for other profiles. The MCP server reads the profile config on every call, so a
new `gum config set` value applies without a restart. gum resolves the default
before it hashes the request, so the audit record and the cache key carry the
account the call used.

## Keyword Planner

The three `googleads.keywordPlanIdeas` operations return worldwide data unless
the call passes `geoTargetConstants`, and data for all languages unless it
passes `language`. gum leaves an omitted field out of the request, and Google
then covers all locations and languages. Nothing marks a worldwide result, and
its figures look plausible for a single country.

For United States searches in English, pass geo target 2840 and language 1000:

```shell
gum read googleads.keywordPlanIdeas.generateKeywordHistoricalMetrics --args '{"keywords":["horse breeds"],"geoTargetConstants":["2840"],"language":"1000"}'
```

`geoTargetConstants` is an array of bare ids such as `2840` or resource names
such as `geoTargetConstants/2840`. `language` takes one id, as `1000` or
`languageConstants/1000`. Google lists the
[geo target ids](https://developers.google.com/google-ads/api/data/geotargets)
and the
[language ids](https://developers.google.com/google-ads/api/data/codes-formats#languages).

### Default location and language

Store the pair in the profile config so every `keywordPlanIdeas` call sends it:

```shell
gum config set googleads.geo_target_constants=2840
gum config set googleads.language=1000
gum read googleads.keywordPlanIdeas.generateKeywordHistoricalMetrics --args '{"keywords":["horse breeds"]}'
```

`GUM_GOOGLE_ADS_GEO_TARGET_CONSTANTS` and `GUM_GOOGLE_ADS_LANGUAGE` set the same
defaults from the environment. Both the variable and the config key take a
comma-separated list for the geo targets and one id for the language. A call
argument wins over the environment, and the environment wins over the profile
config. To ask one call for worldwide figures, pass `"geoTargetConstants":[]`
and `"language":""`.

The defaults reach only the three `keywordPlanIdeas` operations, because they
are the operations that declare the two fields. With neither default set the
request carries no location and no language, which is the earlier behavior.

Each id must be digits, alone or behind its own resource prefix, so
`languageConstants/1000` is rejected for the geo key. A malformed default fails
the call with `INVALID_ARGS` and names the variable or config key that holds it.

### Merged keyword results

Google merges close variants of the submitted keywords into one result and keys
it by its own normalized text. A batch of 245 keywords returned 243 results on
September 18, 2026. Nothing in the response says which submitted string a result
answers, so a caller that joins on `text` reads the merged keywords as zero
volume.

gum maps the inputs back on the shaped formats. A result that answers for more
than one submitted keyword, or for one keyword that differs from its `text`,
carries `matchedInputs` with those submitted strings. Submitted keywords that
reached no result are listed in top-level `unmatchedInputs`. A batch whose
results already map one to one carries neither field.

```shell
gum read googleads.keywordPlanIdeas.generateKeywordHistoricalMetrics --args '{"keywords":["akhal-teke horse","akhal teke horse"]}'
```

That call returns one result with `"text":"akhal teke horse"` and
`"matchedInputs":["akhal-teke horse","akhal teke horse"]`. The result count
always matches the upstream response; gum adds no result objects. `--format raw`
returns the upstream body, which carries Google's own `closeVariants` and no
`matchedInputs`.

A worldwide figure cannot be scaled down to one country afterwards. On
September 17, 2026, historical metrics returned these average monthly searches:

| Keyword | Worldwide, all languages | United States, English |
| --- | --- | --- |
| horse breeds | 135,000 | 60,500 |
| horse insurance | 9,900 | 2,400 |

Across the terms checked that day, the United States share of the worldwide
figure ranged from 24 to 82 percent.

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

Sources checked September 7, 2026. Geo target and language ids checked
September 18, 2026.

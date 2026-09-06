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

Sources checked September 6, 2026.

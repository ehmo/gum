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

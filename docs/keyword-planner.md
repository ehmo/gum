---
title: Keyword Planner
description: "Find keyword ideas, historical search volume, and forecasts with Google Ads in gum."
---

# Keyword Planner

Use Keyword Planner to discover keywords, look up monthly search volume, or
forecast ad traffic. These are read operations under Google Ads. For clicks,
impressions, and rankings from your own site, use [Search Console](search-console.md).

## Set up access

Register your [Google Desktop OAuth client](auth.md), enable the Google Ads
API, and store your developer token. The signed-in Google account needs access
to the customer account. See [Google Ads setup](auth-guides/google-ads.md#google-ads-api)
for the credential steps.

```bash
gum login --service googleads
gum config set googleads.customer_id=1234567890
```

Replace `1234567890` with your 10-digit client account ID. If you access it
through a manager account, also set `googleads.login_customer_id` to the
manager ID. See [default account settings](auth-guides/google-ads.md#default-account).

## Choose location and language

Omitting location and language asks for worldwide data across all languages.
For US searches in English, set these defaults:

```bash
gum config set googleads.geo_target_constants=2840
gum config set googleads.language=1000
```

Arguments on an individual call override these defaults. See
[location and language settings](auth-guides/google-ads.md#default-location-and-language)
for other targets and environment variables.

## Find keyword ideas

Start from keywords, a landing-page URL, or both:

```bash
gum read googleads.keywordPlanIdeas.generateKeywordIdeas --args '{"keywords":["horse breeds"],"url":"https://example.com"}' --output json
```

Replace the seed keywords and URL with your topic and page. Results include
search volume, competition, and top-of-page bid ranges.

## Look up historical search volume

Use a fixed keyword list to get average monthly searches, monthly volumes,
competition, and bid ranges:

```bash
gum read googleads.keywordPlanIdeas.generateKeywordHistoricalMetrics --args '{"keywords":["horse breeds","horse care"]}' --output json
```

Google can merge close variants. Read the [merged keyword results notes](auth-guides/google-ads.md#merged-keyword-results)
before treating each input as a separate result.

## Forecast ad traffic

```bash
gum read googleads.keywordPlanIdeas.generateKeywordForecastMetrics --args '{"keywords":["horse care"],"maxCpcMicros":1000000,"matchType":"EXACT"}' --output json
```

The forecast defaults to tomorrow through 30 days out. Run
`gum describe googleads.keywordPlanIdeas.generateKeywordForecastMetrics` for
date fields and the full request contract.

## Related pages

- [Google Ads operations](services/googleads.md) for GAQL reporting, conversions, and mutations.
- [Search Console](search-console.md) for your site's organic search performance.
- [All services](services/README.md) to find another Google product.

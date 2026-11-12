---
title: Search Console guide
description: "Read organic search clicks, impressions, rankings, sitemaps, and URL inspection results with gum."
---

# Search Console

Use Search Console to read your site's organic search performance, inspect an
indexed URL, or manage sitemaps. For keyword ideas and market search volume,
use [Keyword Planner](keyword-planner.md).

## Set up access and find your property

Register your [Google Desktop OAuth client](auth.md) and enable the Search
Console API. The Google account must have access to the property. See
[Search Console auth setup](auth-guides/search-console.md) for scopes.

```bash
gum login --service searchconsole
gum read searchconsole.sites.list --output json
```

Copy `siteUrl` from that response. A domain property looks like
`sc-domain:example.com`; a URL property looks like `https://example.com/`.
Use the identifier exactly as returned, including the scheme and trailing slash
for a URL property.

## Read clicks, impressions, CTR, and position

Replace the property and date range below with yours. This example groups rows
by search query:

```bash
gum read searchconsole.searchanalytics.query --args '{"siteUrl":"sc-domain:example.com","startDate":"2026-10-01","endDate":"2026-10-31","dimensions":["query"],"rowLimit":1000}' --output json
```

Change `dimensions` to `["page"]` for landing pages, `["date"]` for a daily
report, or `["query","page"]` to combine them. Run
`gum describe searchconsole.searchanalytics.query` for filters, pagination,
and other dimensions.

## Inspect an indexed URL

```bash
gum read searchconsole.urlInspection.index.inspect --args '{"inspectionUrl":"https://example.com/article","siteUrl":"sc-domain:example.com"}' --output json
```

Replace both values with a URL and the property it belongs to.

## List or submit sitemaps

```bash
gum read searchconsole.sitemaps.list --args '{"siteUrl":"sc-domain:example.com"}' --output json
gum write searchconsole.sitemaps.submit --allow-write --args '{"siteUrl":"sc-domain:example.com","feedpath":"https://example.com/sitemap.xml"}'
```

Submitting a sitemap needs the full `webmasters` scope. Read operations use
`webmasters.readonly`. Sitemap deletion and property removal require the
[destructive confirmation flow](safety.md).

## Troubleshooting

- `SCOPE_MISSING`: authorize the scopes named by `gum describe <op_id>`.
- `AUTH_KEYCHAIN_UNAVAILABLE`: gum could not read credentials or the stored
  grant. Unlock the keychain or retry after host load drops. This does not
  establish that a scope is missing. See [Keychain call bounds](paths.md#keychain-call-bounds).
- `PERMISSION_DENIED`: check the signed-in account's access to the property and
  copy the exact `siteUrl` from `searchconsole.sites.list`.

## Related pages

- [All Search Console operations](services/searchconsole.md) for the complete reference.
- [Keyword Planner](keyword-planner.md) for keyword ideas, search volume, and ad forecasts.
- [All services](services/README.md) to find another Google product.

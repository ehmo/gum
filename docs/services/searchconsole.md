---
title: Search Console
description: "Search Console operations in gum's generated catalog."
service_group: "Search and media"
---

# Search Console

Search Console has 10 operations in gum's generated catalog. Start with search when you know the task, use describe to inspect request fields and scopes, then dispatch through the command that matches the operation risk class.

| Count | Value |
| --- | --- |
| Family | Search and media |
| Operations | 10 |
| Risk classes | 2 destructive, 6 read, 2 write |
| Auth strategies | 10 byo_oauth |

## Start here

```bash
gum search "search console"
gum describe searchconsole.searchanalytics.query
gum read searchconsole.searchanalytics.query --args '{"siteUrl":"<siteUrl>","startDate":"<startDate>","endDate":"<endDate>"}' --output json
```

For write-class operations, gum requires the write command and an explicit write gate:

```bash
gum describe searchconsole.sitemaps.submit
gum write searchconsole.sitemaps.submit --allow-write --args '{"siteUrl":"<siteUrl>","feedpath":"<feedpath>"}'
```

For destructive operations, run the call once for a confirmation envelope, review the target, then retry with the returned token:

```bash
gum destructive searchconsole.sitemaps.delete --args '{"siteUrl":"<siteUrl>","feedpath":"<feedpath>"}'
gum destructive searchconsole.sitemaps.delete --args '{"siteUrl":"<siteUrl>","feedpath":"<feedpath>"}' --confirmed --token '<confirmation_token>'
```

## Auth

Auth strategies in this service: 10 byo_oauth. Authenticate the strategy used by the operation you plan to call.

### Bring-your-own OAuth

1. In Google Cloud, enable Search Console API.
2. Configure the OAuth consent screen. Add your Google account as a test user when the app is still in testing mode.
3. Create an OAuth client ID with application type `Desktop app`.
4. Add the scopes this service needs to the consent screen.
5. Store the client in gum:

```bash
printf '%s' "$GOOGLE_OAUTH_CLIENT_SECRET" \
  | gum auth use-oauth-client --client-id "$GOOGLE_OAUTH_CLIENT_ID" --secret-stdin
```

6. Authorize this service:

```bash
gum login --service searchconsole
```

7. Verify the grant before dispatch:

```bash
gum auth status --scopes webmasters,webmasters.readonly
gum describe searchconsole.searchanalytics.query
```

Scopes used by these operations:

- `https://www.googleapis.com/auth/webmasters`
- `https://www.googleapis.com/auth/webmasters.readonly`

Service setup notes: [Search Console auth guide](../auth-guides/search-console.md).

## Operations

| Operation | Risk | Auth | Summary |
| --- | --- | --- | --- |
| `searchconsole.searchanalytics.query` | `read` | `byo_oauth` | Query Search Analytics impressions, clicks, CTR and position. JSON request body required (see args.body): startDate, endDate, dimensions, filters, rowLimit, etc. |
| `searchconsole.sitemaps.delete` | `destructive` | `byo_oauth` | Delete a submitted sitemap from a Search Console site. |
| `searchconsole.sitemaps.get` | `read` | `byo_oauth` | Retrieve information about a specific submitted sitemap. |
| `searchconsole.sitemaps.list` | `read` | `byo_oauth` | List sitemaps submitted for a Search Console site. |
| `searchconsole.sitemaps.submit` | `write` | `byo_oauth` | Submit a sitemap for a Search Console site (resubmitting an existing path refreshes processing). |
| `searchconsole.sites.add` | `write` | `byo_oauth` | Add a site to the user's set of Search Console properties (initiates ownership verification). |
| `searchconsole.sites.delete` | `destructive` | `byo_oauth` | Remove a site from the user's Search Console properties. |
| `searchconsole.sites.get` | `read` | `byo_oauth` | Retrieve information about a specific verified Search Console site. |
| `searchconsole.sites.list` | `read` | `byo_oauth` | List the verified sites the authenticated user has access to in Search Console. |
| `searchconsole.urlInspection.index.inspect` | `read` | `byo_oauth` | Inspect an indexed URL using the Search Console URL Inspection API. Requires JSON request body (args.body): inspectionUrl, siteUrl, languageCode (optional). |

## Next

- Use [API workflows](../api-workflows.md) for search, describe, invoke, and error handling.
- Use [Auth guides](../auth-guides/README.md) for service-specific Google setup.
- Use [Command index](../commands/README.md) for CLI flags and generated help.

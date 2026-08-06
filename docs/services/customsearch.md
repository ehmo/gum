---
title: Custom Search
description: "Custom Search operations in gum's generated catalog."
service_group: "Search and media"
---

# Custom Search

Custom Search has 1 operations in gum's generated catalog. Start with search when you know the task, use describe to inspect request fields and scopes, then dispatch through the command that matches the operation risk class.

| Count | Value |
| --- | --- |
| Family | Search and media |
| Operations | 1 |
| Risk classes | 1 read |
| Auth strategies | 1 api_key |

## Start here

```bash
gum search "custom search"
gum describe customsearch.cse.list
gum read customsearch.cse.list --args '{"fields":"id"}' --output json
```

## Auth

Auth strategies in this service: 1 api_key. Authenticate the strategy used by the operation you plan to call.

### API key

1. In Google Cloud, enable Custom Search API.
2. Create an API key. Restrict it to the API and to the hosts or referrers that should use it.
3. Store the key in gum:

```bash
printf '%s' "$GOOGLE_API_KEY" | gum auth use-api-key --stdin
```

4. Verify with a read operation:

```bash
gum describe customsearch.cse.list
gum read customsearch.cse.list --args '{"fields":"id"}' --output json
```

Service setup notes: [Custom Search auth guide](../auth-guides/maps-custom-search.md).

## Operations

| Operation | Risk | Auth | Summary |
| --- | --- | --- | --- |
| `customsearch.cse.list` | `read` | `api_key` | Run a Custom Search query (q) against a programmable search engine (cx). Returns web results; supports num, start, searchType=image, siteSearch, etc. |

## Next

- Use [API workflows](../api-workflows.md) for search, describe, invoke, and error handling.
- Use [Auth guides](../auth-guides/README.md) for service-specific Google setup.
- Use [Command index](../commands/README.md) for CLI flags and generated help.

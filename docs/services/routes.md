---
title: Routes
description: "Routes operations in gum's generated catalog."
service_group: "Ads and maps"
---

# Routes

Routes has 2 operations in gum's generated catalog. Start with search when you know the task, use describe to inspect request fields and scopes, then dispatch through the command that matches the operation risk class.

| Count | Value |
| --- | --- |
| Family | Ads and maps |
| Operations | 2 |
| Risk classes | 2 read |
| Auth strategies | 2 api_key |

## Start here

```bash
gum search "routes"
gum describe routes.computeRouteMatrix
gum read routes.computeRouteMatrix --args '{"fieldMask":"<fieldMask>"}' --output json
```

## Auth

Auth strategies in this service: 2 api_key. Authenticate the strategy used by the operation you plan to call.

### API key

1. In Google Cloud, enable Routes API.
2. Create an API key. Restrict it to the API and to the hosts or referrers that should use it.
3. Store the key in gum:

```bash
printf '%s' "$GOOGLE_API_KEY" | gum auth use-api-key --stdin
```

4. Verify with a read operation:

```bash
gum describe routes.computeRouteMatrix
gum read routes.computeRouteMatrix --args '{"fieldMask":"<fieldMask>"}' --output json
```

Service setup notes: [Routes auth guide](../auth-guides/maps-custom-search.md).

## Operations

| Operation | Risk | Auth | Summary |
| --- | --- | --- | --- |
| `routes.computeRouteMatrix` | `read` | `api_key` | Routes API distance/time matrix (args.body: origins[], destinations[], travelMode). Requires fieldMask. |
| `routes.computeRoutes` | `read` | `api_key` | Routes API directions between an origin and destination (args.body: origin, destination, travelMode). Requires fieldMask. |

## Next

- Use [API workflows](../api-workflows.md) for search, describe, invoke, and error handling.
- Use [Auth guides](../auth-guides/README.md) for service-specific Google setup.
- Use [Command index](../commands/README.md) for CLI flags and generated help.

---
title: Maps
description: "Maps operations in gum's generated catalog."
service_group: "Ads and maps"
---

# Maps

Maps has 3 operations in gum's generated catalog. Start with search when you know the task, use describe to inspect request fields and scopes, then dispatch through the command that matches the operation risk class.

| Count | Value |
| --- | --- |
| Family | Ads and maps |
| Operations | 3 |
| Risk classes | 3 read |
| Auth strategies | 3 api_key |

## Start here

```bash
gum search "maps"
gum describe maps.distancematrix.get
gum read maps.distancematrix.get --args '{"origins":"<origins>","destinations":"<destinations>"}' --output json
```

## Auth

Auth strategies in this service: 3 api_key. Authenticate the strategy used by the operation you plan to call.

### API key

1. In Google Cloud, enable the required Maps Platform APIs.
2. Create an API key. Restrict it to the API and to the hosts or referrers that should use it.
3. Store the key in gum:

```bash
printf '%s' "$GOOGLE_API_KEY" | gum auth use-api-key --stdin
```

4. Verify with a read operation:

```bash
gum describe maps.distancematrix.get
gum read maps.distancematrix.get --args '{"origins":"<origins>","destinations":"<destinations>"}' --output json
```

Service setup notes: [Maps auth guide](../auth-guides/maps-custom-search.md).

## Operations

| Operation | Risk | Auth | Summary |
| --- | --- | --- | --- |
| `maps.distancematrix.get` | `read` | `api_key` | Travel distance and time for a matrix of origins and destinations. |
| `maps.geocoding.geocode` | `read` | `api_key` | Convert an address to coordinates (address=) or coordinates to an address (latlng=). Optional: components, region, language. |
| `maps.timezone.get` | `read` | `api_key` | Return the time zone for a location at a given time (location=lat,lng; timestamp=Unix seconds). |

## Next

- Use [API workflows](../api-workflows.md) for search, describe, invoke, and error handling.
- Use [Auth guides](../auth-guides/README.md) for service-specific Google setup.
- Use [Command index](../commands/README.md) for CLI flags and generated help.

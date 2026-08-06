---
title: Indexing
description: "Indexing operations in gum's generated catalog."
service_group: "Search and media"
---

# Indexing

Indexing has 2 operations in gum's generated catalog. Start with search when you know the task, use describe to inspect request fields and scopes, then dispatch through the command that matches the operation risk class.

| Count | Value |
| --- | --- |
| Family | Search and media |
| Operations | 2 |
| Risk classes | 1 read, 1 write |
| Auth strategies | 2 byo_oauth |

## Start here

```bash
gum search "indexing"
gum describe indexing.urlNotifications.getMetadata
gum read indexing.urlNotifications.getMetadata --args '{"fields":"id"}' --output json
```

For write-class operations, gum requires the write command and an explicit write gate:

```bash
gum describe indexing.urlNotifications.publish
gum write indexing.urlNotifications.publish --allow-write --args '{"fields":"id"}'
```

## Auth

Auth strategies in this service: 2 byo_oauth. Authenticate the strategy used by the operation you plan to call.

### Bring-your-own OAuth

1. In Google Cloud, enable Indexing API.
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
gum login --service indexing
```

7. Verify the grant before dispatch:

```bash
gum auth status --scopes indexing
gum describe indexing.urlNotifications.getMetadata
```

Scopes used by these operations:

- `https://www.googleapis.com/auth/indexing`

Service setup notes: [Indexing auth guide](../auth-guides/README.md).

## Operations

| Operation | Risk | Auth | Summary |
| --- | --- | --- | --- |
| `indexing.urlNotifications.getMetadata` | `read` | `byo_oauth` | Fetch the most recent notification metadata gum sent Google for a URL (url query param). |
| `indexing.urlNotifications.publish` | `write` | `byo_oauth` | Notify Google that a URL was updated or deleted (args.body: url, type=URL_UPDATED\|URL_DELETED). |

## Next

- Use [API workflows](../api-workflows.md) for search, describe, invoke, and error handling.
- Use [Auth guides](../auth-guides/README.md) for service-specific Google setup.
- Use [Command index](../commands/README.md) for CLI flags and generated help.

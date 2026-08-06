---
title: Groups Settings
description: "Groups Settings operations in gum's generated catalog."
service_group: "Workspace administration"
---

# Groups Settings

Groups Settings has 2 operations in gum's generated catalog. Start with search when you know the task, use describe to inspect request fields and scopes, then dispatch through the command that matches the operation risk class.

| Count | Value |
| --- | --- |
| Family | Workspace administration |
| Operations | 2 |
| Risk classes | 1 read, 1 write |
| Auth strategies | 2 byo_oauth |

## Start here

```bash
gum search "groups settings"
gum describe groupssettings.groups.get
gum read groupssettings.groups.get --args '{"groupUniqueId":"<groupUniqueId>"}' --output json
```

For write-class operations, gum requires the write command and an explicit write gate:

```bash
gum describe groupssettings.groups.update
gum write groupssettings.groups.update --allow-write --args '{"groupUniqueId":"<groupUniqueId>"}'
```

## Auth

Auth strategies in this service: 2 byo_oauth. Authenticate the strategy used by the operation you plan to call.

### Bring-your-own OAuth

1. In Google Cloud, enable Groups Settings API.
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
gum login --service groupssettings
```

7. Verify the grant before dispatch:

```bash
gum auth status --scopes apps.groups.settings
gum describe groupssettings.groups.get
```

Scopes used by these operations:

- `https://www.googleapis.com/auth/apps.groups.settings`

Service setup notes: [Groups Settings auth guide](../auth-guides/admin-cloud-vault.md).

## Operations

| Operation | Risk | Auth | Summary |
| --- | --- | --- | --- |
| `groupssettings.groups.get` | `read` | `byo_oauth` | Fetch a Workspace group's settings by email (groupUniqueId): posting permissions, join policy, archiving, etc. |
| `groupssettings.groups.update` | `write` | `byo_oauth` | Replace a Workspace group's settings (args.body). |

## Next

- Use [API workflows](../api-workflows.md) for search, describe, invoke, and error handling.
- Use [Auth guides](../auth-guides/README.md) for service-specific Google setup.
- Use [Command index](../commands/README.md) for CLI flags and generated help.

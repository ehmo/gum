---
title: Cloud Identity
description: "Cloud Identity operations in gum's generated catalog."
service_group: "Workspace administration"
---

# Cloud Identity

Cloud Identity has 4 operations in gum's generated catalog. Start with search when you know the task, use describe to inspect request fields and scopes, then dispatch through the command that matches the operation risk class.

| Count | Value |
| --- | --- |
| Family | Workspace administration |
| Operations | 4 |
| Risk classes | 4 read |
| Auth strategies | 4 byo_oauth |

## Start here

```bash
gum search "cloud identity"
gum describe cloudidentity.groups.get
gum read cloudidentity.groups.get --args '{"name":"<name>"}' --output json
```

## Auth

Auth strategies in this service: 4 byo_oauth. Authenticate the strategy used by the operation you plan to call.

### Bring-your-own OAuth

1. In Google Cloud, enable Cloud Identity API.
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
gum login --service cloudidentity
```

7. Verify the grant before dispatch:

```bash
gum auth status --scopes cloud-identity.groups.readonly
gum describe cloudidentity.groups.get
```

Scopes used by these operations:

- `https://www.googleapis.com/auth/cloud-identity.groups.readonly`

Service setup notes: [Cloud Identity auth guide](../auth-guides/admin-cloud-vault.md).

## Operations

| Operation | Risk | Auth | Summary |
| --- | --- | --- | --- |
| `cloudidentity.groups.get` | `read` | `byo_oauth` | Fetch a group by resource name (groups/<id>). |
| `cloudidentity.groups.list` | `read` | `byo_oauth` | List groups under a parent (parent=customers/<id>; view=BASIC\|FULL). |
| `cloudidentity.groups.memberships.get` | `read` | `byo_oauth` | Fetch a single membership by resource name (groups/<id>/memberships/<id>). |
| `cloudidentity.groups.memberships.list` | `read` | `byo_oauth` | List the memberships of a Cloud Identity group (parent=groups/<id>). |

## Next

- Use [API workflows](../api-workflows.md) for search, describe, invoke, and error handling.
- Use [Auth guides](../auth-guides/README.md) for service-specific Google setup.
- Use [Command index](../commands/README.md) for CLI flags and generated help.

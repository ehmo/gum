---
title: Vault
description: "Vault operations in gum's generated catalog."
service_group: "Workspace administration"
---

# Vault

Vault has 6 operations in gum's generated catalog. Start with search when you know the task, use describe to inspect request fields and scopes, then dispatch through the command that matches the operation risk class.

| Count | Value |
| --- | --- |
| Family | Workspace administration |
| Operations | 6 |
| Risk classes | 1 destructive, 2 read, 3 write |
| Auth strategies | 6 byo_oauth |

## Start here

```bash
gum search "vault"
gum describe vault.matters.get
gum read vault.matters.get --args '{"matterId":"<matterId>"}' --output json
```

For write-class operations, gum requires the write command and an explicit write gate:

```bash
gum describe vault.matters.close
gum write vault.matters.close --allow-write --args '{"matterId":"<matterId>"}'
```

For destructive operations, run the call once for a confirmation envelope, review the target, then retry with the returned token:

```bash
gum destructive vault.matters.delete --args '{"matterId":"<matterId>"}'
gum destructive vault.matters.delete --args '{"matterId":"<matterId>"}' --confirmed --token '<confirmation_token>'
```

## Auth

Auth strategies in this service: 6 byo_oauth. Authenticate the strategy used by the operation you plan to call.

### Bring-your-own OAuth

1. In Google Cloud, enable Google Vault API.
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
gum login --service vault
```

7. Verify the grant before dispatch:

```bash
gum auth status --scopes ediscovery,ediscovery.readonly
gum describe vault.matters.get
```

Scopes used by these operations:

- `https://www.googleapis.com/auth/ediscovery`
- `https://www.googleapis.com/auth/ediscovery.readonly`

Service setup notes: [Vault auth guide](../auth-guides/admin-cloud-vault.md).

## Operations

| Operation | Risk | Auth | Summary |
| --- | --- | --- | --- |
| `vault.matters.close` | `write` | `byo_oauth` | Close an eDiscovery matter by matterId. |
| `vault.matters.create` | `write` | `byo_oauth` | Create a new eDiscovery matter (args.body: name, description). |
| `vault.matters.delete` | `destructive` | `byo_oauth` | Delete a matter by matterId (must be closed first). Destructive — requires confirmation per §6.1. |
| `vault.matters.get` | `read` | `byo_oauth` | Fetch a matter by matterId. |
| `vault.matters.list` | `read` | `byo_oauth` | List the eDiscovery matters the caller can access (state, view, pageSize). |
| `vault.matters.update` | `write` | `byo_oauth` | Update a matter's name/description by matterId. |

## Next

- Use [API workflows](../api-workflows.md) for search, describe, invoke, and error handling.
- Use [Auth guides](../auth-guides/README.md) for service-specific Google setup.
- Use [Command index](../commands/README.md) for CLI flags and generated help.

---
title: Admin SDK
description: "Admin SDK operations in gum's generated catalog."
service_group: "Workspace administration"
---

# Admin SDK

Admin SDK has 14 operations in gum's generated catalog. Start with search when you know the task, use describe to inspect request fields and scopes, then dispatch through the command that matches the operation risk class.

| Count | Value |
| --- | --- |
| Family | Workspace administration |
| Operations | 14 |
| Risk classes | 3 destructive, 6 read, 5 write |
| Auth strategies | 14 byo_oauth |

## Start here

```bash
gum search "admin sdk"
gum describe admin.directory.groups.get
gum read admin.directory.groups.get --args '{"groupKey":"<groupKey>"}' --output json
```

For write-class operations, gum requires the write command and an explicit write gate:

```bash
gum describe admin.directory.groups.insert
gum write admin.directory.groups.insert --allow-write --args '{"email":"<email>"}'
```

For destructive operations, run the call once for a confirmation envelope, review the target, then retry with the returned token:

```bash
gum destructive admin.directory.groups.delete --args '{"groupKey":"<groupKey>"}'
gum destructive admin.directory.groups.delete --args '{"groupKey":"<groupKey>"}' --confirmed --token '<confirmation_token>'
```

## Auth

Auth strategies in this service: 14 byo_oauth. Authenticate the strategy used by the operation you plan to call.

### Bring-your-own OAuth

1. In Google Cloud, enable Admin SDK API.
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
gum login --service admin
```

7. Verify the grant before dispatch:

```bash
gum auth status --scopes admin.directory.group,admin.directory.group.member,admin.directory.group.member.readonly,admin.directory.group.readonly,admin.directory.user,admin.directory.user.readonly
gum describe admin.directory.groups.get
```

Scopes used by these operations:

- `https://www.googleapis.com/auth/admin.directory.group`
- `https://www.googleapis.com/auth/admin.directory.group.member`
- `https://www.googleapis.com/auth/admin.directory.group.member.readonly`
- `https://www.googleapis.com/auth/admin.directory.group.readonly`
- `https://www.googleapis.com/auth/admin.directory.user`
- `https://www.googleapis.com/auth/admin.directory.user.readonly`

Service setup notes: [Admin SDK auth guide](../auth-guides/admin-cloud-vault.md).

## Operations

| Operation | Risk | Auth | Summary |
| --- | --- | --- | --- |
| `admin.directory.groups.delete` | `destructive` | `byo_oauth` | Delete a Workspace group. Destructive — requires confirmation per §6.1. |
| `admin.directory.groups.get` | `read` | `byo_oauth` | Fetch a single directory group by key (groupKey = id or email). |
| `admin.directory.groups.insert` | `write` | `byo_oauth` | Create a new Workspace group (args.body: email, name, description). |
| `admin.directory.groups.list` | `read` | `byo_oauth` | List the directory groups in the customer's Google Workspace. |
| `admin.directory.groups.update` | `write` | `byo_oauth` | Update a Workspace group by groupKey. |
| `admin.directory.members.delete` | `destructive` | `byo_oauth` | Remove a member from a Workspace group. Destructive — requires confirmation per §6.1. |
| `admin.directory.members.get` | `read` | `byo_oauth` | Fetch a single member of a Workspace directory group. |
| `admin.directory.members.insert` | `write` | `byo_oauth` | Add a member to a Workspace group (args.body: email, role). |
| `admin.directory.members.list` | `read` | `byo_oauth` | List the members of a Workspace directory group. |
| `admin.directory.users.delete` | `destructive` | `byo_oauth` | Delete a Workspace user account. Destructive — requires confirmation per §6.1. |
| `admin.directory.users.get` | `read` | `byo_oauth` | Fetch a single directory user account by key (userKey = id, primaryEmail, or alias). |
| `admin.directory.users.insert` | `write` | `byo_oauth` | Create a new Workspace user account (args.body: primaryEmail, name, password, …). |
| `admin.directory.users.list` | `read` | `byo_oauth` | List the directory user accounts in the customer's Google Workspace. |
| `admin.directory.users.update` | `write` | `byo_oauth` | Update a Workspace user account by userKey. |

## Next

- Use [API workflows](../api-workflows.md) for search, describe, invoke, and error handling.
- Use [Auth guides](../auth-guides/README.md) for service-specific Google setup.
- Use [Command index](../commands/README.md) for CLI flags and generated help.

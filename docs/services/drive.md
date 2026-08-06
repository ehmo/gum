---
title: Drive
description: "Drive operations in gum's generated catalog."
service_group: "Workspace documents"
---

# Drive

Drive has 15 operations in gum's generated catalog. Start with search when you know the task, use describe to inspect request fields and scopes, then dispatch through the command that matches the operation risk class.

| Count | Value |
| --- | --- |
| Family | Workspace documents |
| Operations | 15 |
| Risk classes | 2 destructive, 8 read, 5 write |
| Auth strategies | 15 byo_oauth |

## Start here

```bash
gum search "drive files"
gum describe drive.about.get
gum read drive.about.get --args '{"fields":"id"}' --output json
```

For write-class operations, gum requires the write command and an explicit write gate:

```bash
gum describe drive.files.copy
gum write drive.files.copy --allow-write --args '{"fileId":"<fileId>"}'
```

For destructive operations, run the call once for a confirmation envelope, review the target, then retry with the returned token:

```bash
gum destructive drive.files.delete --args '{"fileId":"<fileId>"}'
gum destructive drive.files.delete --args '{"fileId":"<fileId>"}' --confirmed --token '<confirmation_token>'
```

## Auth

Auth strategies in this service: 15 byo_oauth. Authenticate the strategy used by the operation you plan to call.

### Bring-your-own OAuth

1. In Google Cloud, enable Google Drive API.
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
gum login --service drive
```

7. Verify the grant before dispatch:

```bash
gum auth status --scopes drive,drive.readonly
gum describe drive.about.get
```

Scopes used by these operations:

- `https://www.googleapis.com/auth/drive`
- `https://www.googleapis.com/auth/drive.readonly`

Service setup notes: [Drive auth guide](../auth-guides/drive.md).

## Operations

| Operation | Risk | Auth | Summary |
| --- | --- | --- | --- |
| `drive.about.get` | `read` | `byo_oauth` | Fetch information about the user's Drive: storage quota, user, and import/export formats. |
| `drive.drives.get` | `read` | `byo_oauth` | Fetch a shared drive's metadata by id. |
| `drive.drives.list` | `read` | `byo_oauth` | List the shared drives the user can access. |
| `drive.files.copy` | `write` | `byo_oauth` | Create a copy of a Drive file. |
| `drive.files.create` | `write` | `byo_oauth` | Create a file or folder in Drive (metadata create; set mimeType=application/vnd.google-apps.folder for a folder). |
| `drive.files.delete` | `destructive` | `byo_oauth` | Permanently delete a Drive file (bypasses trash). Destructive — requires confirmation per §6.1. |
| `drive.files.export` | `read` | `byo_oauth` | Export a Google Workspace document (Doc/Sheet/Slides) to another MIME type (e.g. application/pdf). |
| `drive.files.get` | `read` | `byo_oauth` | Fetch a Drive file's metadata. Backs the drive_get_file convenience tool. |
| `drive.files.list` | `read` | `byo_oauth` | Search and list files in Google Drive. Backs the drive_find convenience tool. |
| `drive.files.update` | `write` | `byo_oauth` | Update a Drive file's metadata (rename, move via addParents/removeParents, star, trash flag). |
| `drive.permissions.create` | `write` | `byo_oauth` | Grant a permission on a Drive file or folder. Backs the drive_share convenience tool — requires confirmation per §6.1. |
| `drive.permissions.delete` | `destructive` | `byo_oauth` | Revoke a permission on a Drive file (unshare). Destructive — requires confirmation per §6.1. |
| `drive.permissions.get` | `read` | `byo_oauth` | Fetch a single permission on a Drive file by permission id. |
| `drive.permissions.list` | `read` | `byo_oauth` | List the permissions (sharing) on a Drive file or folder. |
| `drive.permissions.update` | `write` | `byo_oauth` | Change a permission's role on a Drive file (e.g. reader→writer). |

## Next

- Use [API workflows](../api-workflows.md) for search, describe, invoke, and error handling.
- Use [Auth guides](../auth-guides/README.md) for service-specific Google setup.
- Use [Command index](../commands/README.md) for CLI flags and generated help.

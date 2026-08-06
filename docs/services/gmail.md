---
title: Gmail
description: "Gmail operations in gum's generated catalog."
service_group: "Workspace communication"
---

# Gmail

Gmail has 30 operations in gum's generated catalog. Start with search when you know the task, use describe to inspect request fields and scopes, then dispatch through the command that matches the operation risk class.

| Count | Value |
| --- | --- |
| Family | Workspace communication |
| Operations | 30 |
| Risk classes | 6 destructive, 11 read, 13 write |
| Auth strategies | 30 byo_oauth |

## Start here

```bash
gum search "gmail messages"
gum describe gmail.users.drafts.get
gum read gmail.users.drafts.get --args '{"id":"<id>","userId":"me"}' --output json
```

For write-class operations, gum requires the write command and an explicit write gate:

```bash
gum describe gmail.users.drafts.create
gum write gmail.users.drafts.create --allow-write --args '{"userId":"me"}'
```

For destructive operations, run the call once for a confirmation envelope, review the target, then retry with the returned token:

```bash
gum destructive gmail.users.drafts.delete --args '{"id":"<id>","userId":"me"}'
gum destructive gmail.users.drafts.delete --args '{"id":"<id>","userId":"me"}' --confirmed --token '<confirmation_token>'
```

## Auth

Auth strategies in this service: 30 byo_oauth. Authenticate the strategy used by the operation you plan to call.

### Bring-your-own OAuth

1. In Google Cloud, enable Gmail API.
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
gum login --service gmail
```

7. Verify the grant before dispatch:

```bash
gum auth status --scopes https://mail.google.com/,gmail.compose,gmail.labels,gmail.metadata,gmail.modify,gmail.readonly,gmail.send,gmail.settings.basic
gum describe gmail.users.drafts.get
```

Scopes used by these operations:

- `https://mail.google.com/`
- `https://www.googleapis.com/auth/gmail.compose`
- `https://www.googleapis.com/auth/gmail.labels`
- `https://www.googleapis.com/auth/gmail.metadata`
- `https://www.googleapis.com/auth/gmail.modify`
- `https://www.googleapis.com/auth/gmail.readonly`
- `https://www.googleapis.com/auth/gmail.send`
- `https://www.googleapis.com/auth/gmail.settings.basic`

Service setup notes: [Gmail auth guide](../auth-guides/gmail.md).

## Operations

| Operation | Risk | Auth | Summary |
| --- | --- | --- | --- |
| `gmail.users.drafts.create` | `write` | `byo_oauth` | Creates a new draft with the DRAFT label. |
| `gmail.users.drafts.delete` | `destructive` | `byo_oauth` | Permanently delete a draft. Unrecoverable. |
| `gmail.users.drafts.get` | `read` | `byo_oauth` | Get a specific draft including its current message contents. |
| `gmail.users.drafts.list` | `read` | `byo_oauth` | List unsent draft messages in the user's mailbox. |
| `gmail.users.drafts.send` | `write` | `byo_oauth` | Send an existing draft. Deletes the draft and creates a sent message. |
| `gmail.users.drafts.update` | `write` | `byo_oauth` | Replace the contents of an existing draft. |
| `gmail.users.getProfile` | `read` | `byo_oauth` | Get the user's Gmail profile: email address, totals, history id. |
| `gmail.users.history.list` | `read` | `byo_oauth` | List the history of mailbox changes since a given historyId. Used for incremental sync. |
| `gmail.users.labels.create` | `write` | `byo_oauth` | Create a new label in the user's mailbox. |
| `gmail.users.labels.delete` | `destructive` | `byo_oauth` | Permanently delete a user-created label. Messages keep their other labels. |
| `gmail.users.labels.get` | `read` | `byo_oauth` | Get details for a single label by ID. |
| `gmail.users.labels.list` | `read` | `byo_oauth` | List labels in a Gmail mailbox. |
| `gmail.users.labels.patch` | `write` | `byo_oauth` | Partial-update a label's definition (only supplied fields are changed). |
| `gmail.users.labels.update` | `write` | `byo_oauth` | Replace a label's definition. Use patch for partial updates. |
| `gmail.users.messages.batchDelete` | `destructive` | `byo_oauth` | Permanently delete up to 1000 messages. Bypasses Trash; unrecoverable. |
| `gmail.users.messages.batchModify` | `write` | `byo_oauth` | Add or remove labels on up to 1000 messages in a single request. |
| `gmail.users.messages.delete` | `destructive` | `byo_oauth` | Permanently delete a message. Bypasses Trash; unrecoverable. |
| `gmail.users.messages.get` | `read` | `byo_oauth` | Get a specific message from a Gmail mailbox. |
| `gmail.users.messages.list` | `read` | `byo_oauth` | List message IDs in a Gmail mailbox. |
| `gmail.users.messages.modify` | `write` | `byo_oauth` | Add or remove labels on a single Gmail message. |
| `gmail.users.messages.send` | `write` | `byo_oauth` | Sends a Gmail message on behalf of the authenticated user. |
| `gmail.users.messages.trash` | `destructive` | `byo_oauth` | Moves a Gmail message to the Trash. Permanently deletes after 30 days. |
| `gmail.users.messages.untrash` | `write` | `byo_oauth` | Restore a previously trashed message from Trash back to the mailbox. |
| `gmail.users.settings.getVacation` | `read` | `byo_oauth` | Get vacation responder settings for the user's mailbox. |
| `gmail.users.settings.updateVacation` | `write` | `byo_oauth` | Replace vacation responder settings for the user's mailbox. |
| `gmail.users.threads.get` | `read` | `byo_oauth` | Get a specific thread, including all messages on it. |
| `gmail.users.threads.list` | `read` | `byo_oauth` | List threads in the user's mailbox. Threads group related messages. |
| `gmail.users.threads.modify` | `write` | `byo_oauth` | Add or remove labels on every message in a thread. |
| `gmail.users.threads.trash` | `destructive` | `byo_oauth` | Move every message in a thread to the Trash. Permanently deleted after 30 days. |
| `gmail.users.threads.untrash` | `write` | `byo_oauth` | Restore a previously trashed thread from Trash back to the mailbox. |

## Next

- Use [API workflows](../api-workflows.md) for search, describe, invoke, and error handling.
- Use [Auth guides](../auth-guides/README.md) for service-specific Google setup.
- Use [Command index](../commands/README.md) for CLI flags and generated help.

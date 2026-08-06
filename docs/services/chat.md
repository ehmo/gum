---
title: Chat
description: "Chat operations in gum's generated catalog."
service_group: "Workspace communication"
---

# Chat

Chat has 8 operations in gum's generated catalog. Start with search when you know the task, use describe to inspect request fields and scopes, then dispatch through the command that matches the operation risk class.

| Count | Value |
| --- | --- |
| Family | Workspace communication |
| Operations | 8 |
| Risk classes | 1 destructive, 5 read, 2 write |
| Auth strategies | 8 byo_oauth |

## Start here

```bash
gum search "chat"
gum describe chat.spaces.get
gum read chat.spaces.get --args '{"name":"<name>"}' --output json
```

For write-class operations, gum requires the write command and an explicit write gate:

```bash
gum describe chat.spaces.messages.create
gum write chat.spaces.messages.create --allow-write --args '{"parent":"<parent>"}'
```

For destructive operations, run the call once for a confirmation envelope, review the target, then retry with the returned token:

```bash
gum destructive chat.spaces.messages.delete --args '{"name":"<name>"}'
gum destructive chat.spaces.messages.delete --args '{"name":"<name>"}' --confirmed --token '<confirmation_token>'
```

## Auth

Auth strategies in this service: 8 byo_oauth. Authenticate the strategy used by the operation you plan to call.

### Bring-your-own OAuth

1. In Google Cloud, enable Google Chat API.
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
gum login --service chat
```

7. Verify the grant before dispatch:

```bash
gum auth status --scopes chat.memberships.readonly,chat.messages,chat.messages.readonly,chat.spaces.readonly
gum describe chat.spaces.get
```

Scopes used by these operations:

- `https://www.googleapis.com/auth/chat.memberships.readonly`
- `https://www.googleapis.com/auth/chat.messages`
- `https://www.googleapis.com/auth/chat.messages.readonly`
- `https://www.googleapis.com/auth/chat.spaces.readonly`

Service setup notes: [Chat auth guide](../auth-guides/chat.md).

## Operations

| Operation | Risk | Auth | Summary |
| --- | --- | --- | --- |
| `chat.spaces.get` | `read` | `byo_oauth` | Fetch a Chat space by resource name (spaces/AAA). |
| `chat.spaces.list` | `read` | `byo_oauth` | List the Chat spaces the caller is a member of. |
| `chat.spaces.members.list` | `read` | `byo_oauth` | List the members of a Chat space (parent=spaces/AAA). |
| `chat.spaces.messages.create` | `write` | `byo_oauth` | Post a message to a Chat space (parent=spaces/AAA; args.body.text). |
| `chat.spaces.messages.delete` | `destructive` | `byo_oauth` | Delete a Chat message by resource name. Destructive — requires confirmation per §6.1. |
| `chat.spaces.messages.get` | `read` | `byo_oauth` | Fetch a single Chat message by resource name (spaces/AAA/messages/BBB). |
| `chat.spaces.messages.list` | `read` | `byo_oauth` | List messages in a Chat space (parent=spaces/AAA). |
| `chat.spaces.messages.update` | `write` | `byo_oauth` | Update a Chat message's text/cards by resource name (requires updateMask). |

## Next

- Use [API workflows](../api-workflows.md) for search, describe, invoke, and error handling.
- Use [Auth guides](../auth-guides/README.md) for service-specific Google setup.
- Use [Command index](../commands/README.md) for CLI flags and generated help.

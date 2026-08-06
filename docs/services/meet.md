---
title: Meet
description: "Meet operations in gum's generated catalog."
service_group: "Workspace communication"
---

# Meet

Meet has 6 operations in gum's generated catalog. Start with search when you know the task, use describe to inspect request fields and scopes, then dispatch through the command that matches the operation risk class.

| Count | Value |
| --- | --- |
| Family | Workspace communication |
| Operations | 6 |
| Risk classes | 5 read, 1 write |
| Auth strategies | 6 byo_oauth |

## Start here

```bash
gum search "meet"
gum describe meet.conferenceRecords.get
gum read meet.conferenceRecords.get --args '{"name":"<name>"}' --output json
```

For write-class operations, gum requires the write command and an explicit write gate:

```bash
gum describe meet.spaces.create
gum write meet.spaces.create --allow-write --args '{"fields":"id"}'
```

## Auth

Auth strategies in this service: 6 byo_oauth. Authenticate the strategy used by the operation you plan to call.

### Bring-your-own OAuth

1. In Google Cloud, enable Google Meet API.
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
gum login --service meet
```

7. Verify the grant before dispatch:

```bash
gum auth status --scopes meetings.space.created,meetings.space.readonly
gum describe meet.conferenceRecords.get
```

Scopes used by these operations:

- `https://www.googleapis.com/auth/meetings.space.created`
- `https://www.googleapis.com/auth/meetings.space.readonly`

Service setup notes: [Meet auth guide](../auth-guides/classroom-forms-meet-script.md).

## Operations

| Operation | Risk | Auth | Summary |
| --- | --- | --- | --- |
| `meet.conferenceRecords.get` | `read` | `byo_oauth` | Fetch a conference record by resource name (conferenceRecords/<id>). |
| `meet.conferenceRecords.list` | `read` | `byo_oauth` | List past conference records for meetings the caller organized (filter, pageSize). |
| `meet.conferenceRecords.recordings.list` | `read` | `byo_oauth` | List the recordings of a conference record (parent=conferenceRecords/<id>). |
| `meet.conferenceRecords.transcripts.list` | `read` | `byo_oauth` | List the transcripts of a conference record (parent=conferenceRecords/<id>). |
| `meet.spaces.create` | `write` | `byo_oauth` | Create a new Meet meeting space; returns the meetingUri + space name. |
| `meet.spaces.get` | `read` | `byo_oauth` | Fetch a meeting space by resource name (spaces/<id>) or meeting code. |

## Next

- Use [API workflows](../api-workflows.md) for search, describe, invoke, and error handling.
- Use [Auth guides](../auth-guides/README.md) for service-specific Google setup.
- Use [Command index](../commands/README.md) for CLI flags and generated help.

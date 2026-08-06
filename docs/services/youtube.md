---
title: YouTube
description: "YouTube operations in gum's generated catalog."
service_group: "Search and media"
---

# YouTube

YouTube has 11 operations in gum's generated catalog. Start with search when you know the task, use describe to inspect request fields and scopes, then dispatch through the command that matches the operation risk class.

| Count | Value |
| --- | --- |
| Family | Search and media |
| Operations | 11 |
| Risk classes | 2 destructive, 7 read, 2 write |
| Auth strategies | 10 byo_oauth, 1 plugin_managed |

## Start here

```bash
gum search "youtube"
gum describe youtube.channels.list
gum read youtube.channels.list --args '{"part":"<part>"}' --output json
```

For write-class operations, gum requires the write command and an explicit write gate:

```bash
gum describe youtube.playlistItems.insert
gum write youtube.playlistItems.insert --allow-write --args '{"part":"<part>"}'
```

For destructive operations, run the call once for a confirmation envelope, review the target, then retry with the returned token:

```bash
gum destructive youtube.playlistItems.delete --args '{"id":"<id>"}'
gum destructive youtube.playlistItems.delete --args '{"id":"<id>"}' --confirmed --token '<confirmation_token>'
```

## Auth

Auth strategies in this service: 10 byo_oauth, 1 plugin_managed. Authenticate the strategy used by the operation you plan to call.

### Bring-your-own OAuth

1. In Google Cloud, enable YouTube Data API v3.
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
gum login --service youtube
```

7. Verify the grant before dispatch:

```bash
gum auth status --scopes youtube,youtube.readonly
gum describe youtube.channels.list
```

Scopes used by these operations:

- `https://www.googleapis.com/auth/youtube`
- `https://www.googleapis.com/auth/youtube.readonly`

Service setup notes: [YouTube auth guide](../auth-guides/youtube.md).

### Plugin-managed auth

1. No Google OAuth client, API key, or service account is configured through gum for these operations.
2. Confirm the plugin-backed operation is available:

```bash
gum plugin list
gum describe youtube.transcripts.get
```

3. Follow the plugin's upstream requirements, rate limits, and terms before calling it.
4. Verify with a read call:

```bash
gum read youtube.transcripts.get --args '{"video_id":"<video_id>"}' --output json
```

## Operations

| Operation | Risk | Auth | Summary |
| --- | --- | --- | --- |
| `youtube.channels.list` | `read` | `byo_oauth` | Fetch channel resources by id, forUsername, or mine=true (part=snippet,statistics,contentDetails). |
| `youtube.playlistItems.delete` | `destructive` | `byo_oauth` | Remove an item from a playlist by id. Destructive — requires confirmation per §6.1. |
| `youtube.playlistItems.insert` | `write` | `byo_oauth` | Add a video to a playlist (part=snippet; args.body: snippet.playlistId, snippet.resourceId.videoId). |
| `youtube.playlistItems.list` | `read` | `byo_oauth` | Fetch the items of a playlist (part=snippet,contentDetails; playlistId or id). |
| `youtube.playlists.delete` | `destructive` | `byo_oauth` | Delete a playlist by id. Destructive — requires confirmation per §6.1. |
| `youtube.playlists.insert` | `write` | `byo_oauth` | Create a new playlist (part=snippet,status; args.body: snippet.title, status.privacyStatus). |
| `youtube.playlists.list` | `read` | `byo_oauth` | Fetch playlist resources by id, channelId, or mine=true (part=snippet,contentDetails). |
| `youtube.search.list` | `read` | `byo_oauth` | Search YouTube for videos, channels, and playlists (part=snippet; q, type, channelId, order, maxResults, …). Costs 100 quota units. |
| `youtube.subscriptions.list` | `read` | `byo_oauth` | Fetch subscription resources (part=snippet; mine=true or channelId). |
| `youtube.transcripts.get` | `read` | `plugin_managed` | Fetch the auto-generated or human-authored transcript for a YouTube video by video_id. Backed by the bundled youtube-transcripts Shape 1 plugin. |
| `youtube.videos.list` | `read` | `byo_oauth` | Fetch video resources by id (part=snippet,contentDetails,statistics) or chart=mostPopular. |

## Next

- Use [API workflows](../api-workflows.md) for search, describe, invoke, and error handling.
- Use [Auth guides](../auth-guides/README.md) for service-specific Google setup.
- Use [Command index](../commands/README.md) for CLI flags and generated help.

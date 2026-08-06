---
title: Photos Library
description: "Photos Library operations in gum's generated catalog."
service_group: "Search and media"
---

# Photos Library

Photos Library has 6 operations in gum's generated catalog. Start with search when you know the task, use describe to inspect request fields and scopes, then dispatch through the command that matches the operation risk class.

| Count | Value |
| --- | --- |
| Family | Search and media |
| Operations | 6 |
| Risk classes | 5 read, 1 write |
| Auth strategies | 6 byo_oauth |

## Start here

```bash
gum search "photos library"
gum describe photoslibrary.albums.get
gum read photoslibrary.albums.get --args '{"albumId":"<albumId>"}' --output json
```

For write-class operations, gum requires the write command and an explicit write gate:

```bash
gum describe photoslibrary.albums.create
gum write photoslibrary.albums.create --allow-write --args '{"fields":"id"}'
```

## Auth

Auth strategies in this service: 6 byo_oauth. Authenticate the strategy used by the operation you plan to call.

### Bring-your-own OAuth

1. In Google Cloud, enable Photos Library API.
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
gum login --service photoslibrary
```

7. Verify the grant before dispatch:

```bash
gum auth status --scopes photoslibrary.appendonly,photoslibrary.readonly.appcreateddata
gum describe photoslibrary.albums.get
```

Scopes used by these operations:

- `https://www.googleapis.com/auth/photoslibrary.appendonly`
- `https://www.googleapis.com/auth/photoslibrary.readonly.appcreateddata`

Service setup notes: [Photos Library auth guide](../auth-guides/photos-library.md).

## Operations

| Operation | Risk | Auth | Summary |
| --- | --- | --- | --- |
| `photoslibrary.albums.create` | `write` | `byo_oauth` | Create a new album (args.body.album.title). |
| `photoslibrary.albums.get` | `read` | `byo_oauth` | Fetch an album by id. |
| `photoslibrary.albums.list` | `read` | `byo_oauth` | List the albums in the user's Photos library (pageSize, pageToken). |
| `photoslibrary.mediaItems.get` | `read` | `byo_oauth` | Fetch a single media item by id. |
| `photoslibrary.mediaItems.list` | `read` | `byo_oauth` | List the media items in the user's Photos library (pageSize, pageToken). |
| `photoslibrary.mediaItems.search` | `read` | `byo_oauth` | Search media items by album, date, or content category (args.body: albumId or filters). |

## Next

- Use [API workflows](../api-workflows.md) for search, describe, invoke, and error handling.
- Use [Auth guides](../auth-guides/README.md) for service-specific Google setup.
- Use [Command index](../commands/README.md) for CLI flags and generated help.

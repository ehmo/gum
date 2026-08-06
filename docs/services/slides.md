---
title: Slides
description: "Slides operations in gum's generated catalog."
service_group: "Workspace documents"
---

# Slides

Slides has 3 operations in gum's generated catalog. Start with search when you know the task, use describe to inspect request fields and scopes, then dispatch through the command that matches the operation risk class.

| Count | Value |
| --- | --- |
| Family | Workspace documents |
| Operations | 3 |
| Risk classes | 1 read, 2 write |
| Auth strategies | 3 byo_oauth |

## Start here

```bash
gum search "slides"
gum describe slides.presentations.get
gum read slides.presentations.get --args '{"presentationId":"<presentationId>"}' --output json
```

For write-class operations, gum requires the write command and an explicit write gate:

```bash
gum describe slides.presentations.batchUpdate
gum write slides.presentations.batchUpdate --allow-write --args '{"presentationId":"<presentationId>"}'
```

## Auth

Auth strategies in this service: 3 byo_oauth. Authenticate the strategy used by the operation you plan to call.

### Bring-your-own OAuth

1. In Google Cloud, enable Google Slides API.
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
gum login --service slides
```

7. Verify the grant before dispatch:

```bash
gum auth status --scopes presentations,presentations.readonly
gum describe slides.presentations.get
```

Scopes used by these operations:

- `https://www.googleapis.com/auth/presentations`
- `https://www.googleapis.com/auth/presentations.readonly`

Service setup notes: [Slides auth guide](../auth-guides/docs-sheets-slides.md).

## Operations

| Operation | Risk | Auth | Summary |
| --- | --- | --- | --- |
| `slides.presentations.batchUpdate` | `write` | `byo_oauth` | Apply a batch of edit requests to a presentation (add slides, insert text/shapes/images, formatting). The core Slides editing op. |
| `slides.presentations.create` | `write` | `byo_oauth` | Create a new Google Slides presentation. |
| `slides.presentations.get` | `read` | `byo_oauth` | Fetch compact Slides presentation metadata and page summaries. Backs the slides_get convenience tool. |

## Next

- Use [API workflows](../api-workflows.md) for search, describe, invoke, and error handling.
- Use [Auth guides](../auth-guides/README.md) for service-specific Google setup.
- Use [Command index](../commands/README.md) for CLI flags and generated help.

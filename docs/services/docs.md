---
title: Docs
description: "Docs operations in gum's generated catalog."
service_group: "Workspace documents"
---

# Docs

Docs has 3 operations in gum's generated catalog. Start with search when you know the task, use describe to inspect request fields and scopes, then dispatch through the command that matches the operation risk class.

| Count | Value |
| --- | --- |
| Family | Workspace documents |
| Operations | 3 |
| Risk classes | 1 read, 2 write |
| Auth strategies | 3 byo_oauth |

## Start here

```bash
gum search "docs documents"
gum describe docs.documents.get
gum read docs.documents.get --args '{"documentId":"<documentId>"}' --output json
```

For write-class operations, gum requires the write command and an explicit write gate:

```bash
gum describe docs.documents.batchUpdate
gum write docs.documents.batchUpdate --allow-write --args '{"documentId":"<documentId>","requests":[]}'
```

## Auth

Auth strategies in this service: 3 byo_oauth. Authenticate the strategy used by the operation you plan to call.

### Bring-your-own OAuth

1. In Google Cloud, enable Google Docs API.
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
gum login --service docs
```

7. Verify the grant before dispatch:

```bash
gum auth status --scopes documents,documents.readonly
gum describe docs.documents.get
```

Scopes used by these operations:

- `https://www.googleapis.com/auth/documents`
- `https://www.googleapis.com/auth/documents.readonly`

Service setup notes: [Docs auth guide](../auth-guides/docs-sheets-slides.md).

## Operations

| Operation | Risk | Auth | Summary |
| --- | --- | --- | --- |
| `docs.documents.batchUpdate` | `write` | `byo_oauth` | Apply a batch of edit requests to a Google Doc (insert/replace text, formatting, tables, images). The core Docs editing op. |
| `docs.documents.create` | `write` | `byo_oauth` | Create a Google Doc. Request body lives in args.document. Backs the docs_create convenience tool. |
| `docs.documents.get` | `read` | `byo_oauth` | Fetch a Google Doc by document ID. Backs the docs_get convenience tool. |

## Next

- Use [API workflows](../api-workflows.md) for search, describe, invoke, and error handling.
- Use [Auth guides](../auth-guides/README.md) for service-specific Google setup.
- Use [Command index](../commands/README.md) for CLI flags and generated help.

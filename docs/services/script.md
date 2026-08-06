---
title: Apps Script
description: "Apps Script operations in gum's generated catalog."
service_group: "Workspace documents"
---

# Apps Script

Apps Script has 5 operations in gum's generated catalog. Start with search when you know the task, use describe to inspect request fields and scopes, then dispatch through the command that matches the operation risk class.

| Count | Value |
| --- | --- |
| Family | Workspace documents |
| Operations | 5 |
| Risk classes | 3 read, 2 write |
| Auth strategies | 5 byo_oauth |

## Start here

```bash
gum search "apps script"
gum describe script.projects.deployments.list
gum read script.projects.deployments.list --args '{"scriptId":"<scriptId>"}' --output json
```

For write-class operations, gum requires the write command and an explicit write gate:

```bash
gum describe script.projects.create
gum write script.projects.create --allow-write --args '{"fields":"id"}'
```

## Auth

Auth strategies in this service: 5 byo_oauth. Authenticate the strategy used by the operation you plan to call.

### Bring-your-own OAuth

1. In Google Cloud, enable Apps Script API.
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
gum login --service script
```

7. Verify the grant before dispatch:

```bash
gum auth status --scopes script.deployments,script.projects,script.projects.readonly
gum describe script.projects.deployments.list
```

Scopes used by these operations:

- `https://www.googleapis.com/auth/script.deployments`
- `https://www.googleapis.com/auth/script.projects`
- `https://www.googleapis.com/auth/script.projects.readonly`

Service setup notes: [Apps Script auth guide](../auth-guides/classroom-forms-meet-script.md).

## Operations

| Operation | Risk | Auth | Summary |
| --- | --- | --- | --- |
| `script.projects.create` | `write` | `byo_oauth` | Create a new (standalone) Apps Script project (args.body.title). |
| `script.projects.deployments.list` | `read` | `byo_oauth` | List the deployments of an Apps Script project. |
| `script.projects.get` | `read` | `byo_oauth` | Fetch an Apps Script project's metadata by scriptId. |
| `script.projects.getContent` | `read` | `byo_oauth` | Fetch the source files of an Apps Script project. |
| `script.projects.updateContent` | `write` | `byo_oauth` | Replace the source files of an Apps Script project (args.body.files). |

## Next

- Use [API workflows](../api-workflows.md) for search, describe, invoke, and error handling.
- Use [Auth guides](../auth-guides/README.md) for service-specific Google setup.
- Use [Command index](../commands/README.md) for CLI flags and generated help.

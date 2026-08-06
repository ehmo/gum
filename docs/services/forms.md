---
title: Forms
description: "Forms operations in gum's generated catalog."
service_group: "Workspace documents"
---

# Forms

Forms has 5 operations in gum's generated catalog. Start with search when you know the task, use describe to inspect request fields and scopes, then dispatch through the command that matches the operation risk class.

| Count | Value |
| --- | --- |
| Family | Workspace documents |
| Operations | 5 |
| Risk classes | 3 read, 2 write |
| Auth strategies | 5 byo_oauth |

## Start here

```bash
gum search "forms"
gum describe forms.forms.get
gum read forms.forms.get --args '{"formId":"<formId>"}' --output json
```

For write-class operations, gum requires the write command and an explicit write gate:

```bash
gum describe forms.forms.batchUpdate
gum write forms.forms.batchUpdate --allow-write --args '{"formId":"<formId>"}'
```

## Auth

Auth strategies in this service: 5 byo_oauth. Authenticate the strategy used by the operation you plan to call.

### Bring-your-own OAuth

1. In Google Cloud, enable Google Forms API.
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
gum login --service forms
```

7. Verify the grant before dispatch:

```bash
gum auth status --scopes forms.body,forms.body.readonly,forms.responses.readonly
gum describe forms.forms.get
```

Scopes used by these operations:

- `https://www.googleapis.com/auth/forms.body`
- `https://www.googleapis.com/auth/forms.body.readonly`
- `https://www.googleapis.com/auth/forms.responses.readonly`

Service setup notes: [Forms auth guide](../auth-guides/classroom-forms-meet-script.md).

## Operations

| Operation | Risk | Auth | Summary |
| --- | --- | --- | --- |
| `forms.forms.batchUpdate` | `write` | `byo_oauth` | Apply a batch of edits to a form (add/update/delete items, update form info/settings). The core Forms editing op. |
| `forms.forms.create` | `write` | `byo_oauth` | Create a new Google Form (args.body.info.title). Returns the formId. |
| `forms.forms.get` | `read` | `byo_oauth` | Fetch a form's structure (items, questions, settings) by formId. |
| `forms.forms.responses.get` | `read` | `byo_oauth` | Fetch a single form response by responseId. |
| `forms.forms.responses.list` | `read` | `byo_oauth` | List the submitted responses for a form (optional filter, pageSize, pageToken). |

## Next

- Use [API workflows](../api-workflows.md) for search, describe, invoke, and error handling.
- Use [Auth guides](../auth-guides/README.md) for service-specific Google setup.
- Use [Command index](../commands/README.md) for CLI flags and generated help.

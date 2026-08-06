---
title: Sheets
description: "Sheets operations in gum's generated catalog."
service_group: "Workspace documents"
---

# Sheets

Sheets has 9 operations in gum's generated catalog. Start with search when you know the task, use describe to inspect request fields and scopes, then dispatch through the command that matches the operation risk class.

| Count | Value |
| --- | --- |
| Family | Workspace documents |
| Operations | 9 |
| Risk classes | 3 read, 6 write |
| Auth strategies | 9 byo_oauth |

## Start here

```bash
gum search "sheets values"
gum describe sheets.spreadsheets.get
gum read sheets.spreadsheets.get --args '{"spreadsheetId":"<spreadsheetId>"}' --output json
```

For write-class operations, gum requires the write command and an explicit write gate:

```bash
gum describe sheets.spreadsheets.batchUpdate
gum write sheets.spreadsheets.batchUpdate --allow-write --args '{"spreadsheetId":"<spreadsheetId>","requests":[]}'
```

## Auth

Auth strategies in this service: 9 byo_oauth. Authenticate the strategy used by the operation you plan to call.

### Bring-your-own OAuth

1. In Google Cloud, enable Google Sheets API.
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
gum login --service sheets
```

7. Verify the grant before dispatch:

```bash
gum auth status --scopes spreadsheets,spreadsheets.readonly
gum describe sheets.spreadsheets.get
```

Scopes used by these operations:

- `https://www.googleapis.com/auth/spreadsheets`
- `https://www.googleapis.com/auth/spreadsheets.readonly`

Service setup notes: [Sheets auth guide](../auth-guides/docs-sheets-slides.md).

## Operations

| Operation | Risk | Auth | Summary |
| --- | --- | --- | --- |
| `sheets.spreadsheets.batchUpdate` | `write` | `byo_oauth` | Apply structural/formatting edits to a spreadsheet (add sheets, format cells, charts, conditional formatting). |
| `sheets.spreadsheets.create` | `write` | `byo_oauth` | Create a new Google Spreadsheet. |
| `sheets.spreadsheets.get` | `read` | `byo_oauth` | Fetch a spreadsheet's metadata, sheets, and (optionally) cell data. |
| `sheets.spreadsheets.values.append` | `write` | `byo_oauth` | Append rows of values after a table in a spreadsheet range. |
| `sheets.spreadsheets.values.batchGet` | `read` | `byo_oauth` | Read values from several ranges of a spreadsheet in one call. |
| `sheets.spreadsheets.values.batchUpdate` | `write` | `byo_oauth` | Write values to several ranges of a spreadsheet in one call. |
| `sheets.spreadsheets.values.clear` | `write` | `byo_oauth` | Clear the values from a spreadsheet range (keeps formatting). |
| `sheets.spreadsheets.values.get` | `read` | `byo_oauth` | Read values from a Google Sheets range. Backs the sheets_read convenience tool. |
| `sheets.spreadsheets.values.update` | `write` | `byo_oauth` | Write values to a Google Sheets range. Backs the sheets_write convenience tool. |

## Next

- Use [API workflows](../api-workflows.md) for search, describe, invoke, and error handling.
- Use [Auth guides](../auth-guides/README.md) for service-specific Google setup.
- Use [Command index](../commands/README.md) for CLI flags and generated help.

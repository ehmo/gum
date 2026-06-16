# Sheets quickstart

Read and update Google Sheets values through `gum`. Sheets ops resolve to the
typed `sheets/v4` REST SDK; auth flows through the same chain as every
Workspace op (spec §7).

## Auth setup (one-time)

```
printf '%s' '<client_secret>' | gum auth use-oauth-client \
  --client-id <client_id> --secret-stdin
gum auth login --scope https://www.googleapis.com/auth/spreadsheets.readonly
```

Required scopes for the v0.1.0 Sheets roster:

| Op                                   | Scope                                                |
|--------------------------------------|------------------------------------------------------|
| `sheets.spreadsheets.values.get`     | `https://www.googleapis.com/auth/spreadsheets.readonly` |
| `sheets.spreadsheets.values.update`  | `https://www.googleapis.com/auth/spreadsheets`       |

`spreadsheets.values.batchGet`/`batchUpdate` are not in the v0.1.0 typed
roster; call them through `gum http get|post` until the canonical bindings
land.

## First op: read a range

```
gum read sheets.spreadsheets.values.get \
   spreadsheetId=<spreadsheet_id> range="Sheet1!A1:C10"
```

The `range` argument uses A1 notation: `SheetName!A1:C10`. Quote the sheet
name if it contains spaces: `range="'Q3 Plan'!A1:C10"`.

## Common patterns

**Read a whole sheet by name**
```
gum read sheets.spreadsheets.values.get spreadsheetId=<id> range="Sheet1"
```

**Choose how values render**

Pass `valueRenderOption` to control formatting:

| Value                  | Effect                                                 |
|------------------------|--------------------------------------------------------|
| `FORMATTED_VALUE`      | (default) numbers/dates as they appear in the UI       |
| `UNFORMATTED_VALUE`    | raw numbers/dates (e.g. serial dates as floats)        |
| `FORMULA`              | cell formulas (`=SUM(...)`) instead of computed values |

```
gum read sheets.spreadsheets.values.get spreadsheetId=<id> range="Sheet1!A:A" \
   valueRenderOption=UNFORMATTED_VALUE
```

**Write a range** (write — overwrites cells)
```
gum write sheets.spreadsheets.values.update \
   spreadsheetId=<id> range="Sheet1!A1:B2" valueInputOption=USER_ENTERED \
   values='[["name","value"],["alpha",42]]'
```

`valueInputOption=USER_ENTERED` parses `=SUM(...)` as formulas and `2026-05-24`
as a date — the same way a human typing into the UI would. Use `RAW` to insert
strings verbatim with no parsing.

**Append rows** (not in v0.1.0 roster)
```
gum http post sheets/v4/spreadsheets/<id>/values/Sheet1!A:B:append?valueInputOption=USER_ENTERED \
   --body '{"values": [["bravo", 17]]}'
```

## Output shapes you'll see

- `values.get`    → `{range, majorDimension, values: [["A1","B1"],["A2","B2"]]}`
- `values.update` → `{spreadsheetId, updatedRange, updatedRows, updatedColumns, updatedCells}`

`values.get` omits the `values` field entirely when the range is empty — don't
assume it's always present. Always default-coerce in jq:
`jq '.values // [] | .[]'`.

`gum search sheets` returns every op_id in the indexed Sheets surface with
ranking. `gum describe sheets.spreadsheets.values.get` shows the full ABI.

## Range syntax

| Form                  | Meaning                                                |
|-----------------------|--------------------------------------------------------|
| `Sheet1!A1:C10`       | a specific rectangle                                   |
| `Sheet1!A:A`          | entire column A                                        |
| `Sheet1!1:1`          | entire row 1                                           |
| `Sheet1`              | every populated cell on the sheet                      |
| `'Q3 Plan'!A1`        | a single cell on a sheet whose name contains a space   |

The API trims trailing empty rows/columns automatically on `get`. `update`
does not — it writes exactly the rectangle you specify and pads short rows
with empty strings.

## Errors

- `AUTH_SCOPE_MISSING` — your stored grant lacks `auth/spreadsheets`. Re-run
  `gum auth login --scope https://www.googleapis.com/auth/spreadsheets` with
  the broader scope.
- `INVALID_ARGUMENT` — malformed A1 range (e.g. unquoted sheet name with a
  space) or unrecognised `valueInputOption`.
- `NOT_FOUND` — spreadsheet id doesn't exist or the principal can't see it.
- `RATE_LIMITED` — Sheets returned 429 (per-user-per-100s quota is the usual
  culprit). The dispatcher honours `Retry-After`.

## Related

- `gum://help/auth` — full credential chain
- `gum://help/field-masks` — projection grammar
- `gum://help/toon-format` — output encoding

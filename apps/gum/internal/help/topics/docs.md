# Docs quickstart

Get and create Google Docs through `gum`. Docs ops resolve to the typed
`docs/v1` REST SDK; auth flows through the same chain as every Workspace op
(spec §7).

## Auth setup (one-time)

```
printf '%s' '<client_secret>' | gum auth use-oauth-client \
  --client-id <client_id> --secret-stdin
gum auth login --scope https://www.googleapis.com/auth/documents.readonly
```

Required scopes for the v0.1.0 Docs roster:

| Op                       | Scope                                                |
|--------------------------|------------------------------------------------------|
| `docs.documents.get`     | `https://www.googleapis.com/auth/documents.readonly` |
| `docs.documents.create`  | `https://www.googleapis.com/auth/documents`          |

Body edits beyond `create` (text inserts, formatting, replaceAll) require
`docs.documents.batchUpdate`, which is not in the v0.1.0 typed roster — call
it through `gum http post` until the canonical binding lands.

## First op: read a document

```
gum read docs.documents.get documentId=<doc_id>
```

Returns the full structured document. Be warned: even a one-paragraph doc
serialises to multiple KB of nested paragraph/`textRun` elements. Project
upstream with `fields=` when you only need part of the tree.

```
gum read docs.documents.get documentId=<doc_id> \
   fields="title,documentId,body(content(paragraph(elements(textRun(content)))))"
```

## Common patterns

**Create a blank doc**
```
gum write docs.documents.create title="Q3 plan"
```

Returns the new `documentId` plus the empty body skeleton. The doc lives in
the principal's Drive root by default.

**Extract plain-text from a doc**

The Docs API returns structured JSON, never plain text. To get plain text:

```
gum read docs.documents.get documentId=<doc_id> format=json \
  | jq -r '.body.content[] | .paragraph?.elements[]?.textRun?.content // empty'
```

**Send to a specific Drive folder**

`docs.documents.create` itself does not accept a parent folder. Create the
doc, then move it with Drive:

```
DOC_ID=$(gum write docs.documents.create title="Q3 plan" format=json | jq -r .documentId)
gum write drive.files.update fileId=$DOC_ID addParents=<folder_id>
```

## Output shapes you'll see

- `documents.get`    → `{documentId, title, body: {content: [...]}, revisionId, suggestionsViewMode}`
- `documents.create` → `{documentId, title, body, revisionId}`

`gum search docs` returns every op_id in the indexed Docs surface with
ranking. `gum describe docs.documents.get` shows the full ABI.

## Structured-content walk

The `body.content[]` array is a flat list of `StructuralElement`s. Each entry
is one of:

| Variant            | Field                                            |
|--------------------|--------------------------------------------------|
| Paragraph          | `paragraph.elements[].textRun.content`           |
| Table              | `table.tableRows[].tableCells[].content[]`       |
| Section break      | `sectionBreak`                                   |
| Table of contents  | `tableOfContents.content[]`                      |

A `textRun` is the leaf: `{content: "...", textStyle: {...}}`. Concatenate
every `textRun.content` to recover plain text.

## Errors

- `AUTH_SCOPE_MISSING` — your stored grant lacks `auth/documents` (or
  `.readonly`). Re-run `gum auth login --scope <scope-url>` with the broader
  scope.
- `NOT_FOUND` — document id doesn't exist or the principal can't see it.
  Docs doesn't distinguish; check the share state.
- `PERMISSION_DENIED` — write-shaped op without write scope, or doc is owned
  by someone who hasn't granted edit access.
- `RATE_LIMITED` — Docs returned 429. The dispatcher honours `Retry-After`.

## Related

- `gum://help/auth` — full credential chain
- `gum://help/field-masks` — projection grammar
- `gum://help/toon-format` — output encoding

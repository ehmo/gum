# TOON output format

TOON (Token-Oriented Object Notation) is GUM's canonical wire format for
tabular responses. It packs JSON-equivalent rows into a CSV-style body
with a YAML-style header, producing ~3-5× fewer tokens than JSON for the
same data (spec §9.0).

## Anatomy

```
op: gmail.users.messages.list
variant: gmail.v1.rest.users.messages.list
format_version: 1
fields: id,thread_id,sender,subject
count: 3

m_001,t_001,alice@example.com,"Re: lunch?"
m_002,t_001,bob@example.com,"Re: lunch?"
m_003,t_002,carol@example.com,Status update
```

Two sections separated by a blank line:

1. **Header** — YAML-style `key: value` per line. Required keys: `op`,
   `variant`, `count`, `fields`, `format_version`. Optional:
   `next_page_token`.
2. **Body** — RFC 4180-style CSV rows, **no CSV header row** (field order
   is given by the header's `fields` value).

## Field encoding

- **String** — bare unless it contains `,`, `"`, or newline; quoted with
  `"..."` otherwise; internal `"` escaped as `""`.
- **Number / bool** — bare, no quotes.
- **Null** — empty field (two consecutive commas).
- **Nested object** — encoded as a JSON literal inside double quotes:
  `"{""id"":1,""name"":""x""}"`. Use sparingly; the gain is mostly lost
  when nesting goes more than one level deep.

## Empty-body and omitted-count sentinels

- `count: 0` with no body section is the **empty result** sentinel.
- A body section with `{}` on its first line is the **omitted_count**
  sentinel for ops where the upstream API does not provide a total count
  (the body still carries rows; the header's `count` reflects the rows
  returned, not the total available).

## Parsing TOON

The TOON parser lives at `internal/output/toon/` and is the canonical
reference implementation. Plugins that want to emit TOON should depend on
that package rather than rolling their own — the format is small but the
quoting rules trip up hand-written parsers.

Decoding into Go:

```go
import "github.com/ehmo/gum/internal/output/toon"

doc, err := toon.Decode(rawBytes)
if err != nil {
    return err
}
fmt.Println(doc.Header.Op, len(doc.Rows))
```

## When NOT to use TOON

- **Single object responses** (not row-shaped) — pass through as JSON.
- **Binary payloads** — return as `application/octet-stream` with a
  resource link.
- **Deeply nested structures** — JSON or markdown render better. TOON's
  encoding-overhead penalty dominates the savings.

The dispatch kernel picks the format from the active expression override's
`format` key. The default for Tier A list ops is `toon`; the default for
get-one ops is `json`.

## Errors

- `TOON_VERSION_UNSUPPORTED` — header `format_version` not in `{1}`.
- `TOON_FIELDS_MISMATCH` — a body row has more or fewer fields than the
  header's `fields` value declared.

See `gum://help/field-masks` for the projection step that shapes the data
before TOON serialisation.

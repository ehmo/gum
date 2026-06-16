# Upstream projection and field-mask behaviour

GUM applies projection in two stages (spec §9.1):

1. **Upstream projection** — a Google REST field-mask sent to the API in
   the `fields=` query parameter (or the gRPC equivalent). The upstream
   server returns only the requested fields, saving bytes on the wire.
2. **Expression projection** — a GUM DSL projection applied to the
   upstream response after parsing. This handles renames, joins, and
   shape transforms the field-mask cannot express.

The two stages compose. A profile override may set either or both.

## Field-mask syntax

GUM accepts the standard Google REST field-mask grammar:

```
messages(id,subject,sender)         // select fields
messages(*),next_page_token         // wildcard inside a sub-object
items/payload/headers(name,value)   // nested path
items(id,*)                         // id plus everything else
```

Whitespace is ignored. Commas separate sibling fields; parens introduce a
sub-selection; `/` walks nested objects; `*` selects every field at that
level.

## Field-mask modes

`field_mask_mode` in the expression profile selects how the mask is applied:

- **`upstream_only`** (default) — pass the mask verbatim to the upstream
  API; trust the API to return only those fields.
- **`local_only`** — fetch the full response, then prune locally. Used for
  ops that ignore `fields=` (rare).
- **`dual_fetch`** — issue both a masked and an unmasked request, diff the
  two for projection-coverage telemetry, return the masked response. Used
  during catalog generation to detect ops where the API silently ignores
  the mask.

## Coverage validation

`gum profile validate` runs every override's field-mask against the catalog's
declared `output_schema` for the op. A mask that references a field absent
from the schema fails validation with `FIELD_MASK_UNKNOWN_FIELD`. This
catches typos before they reach production.

## Interaction with the expression DSL

The expression DSL operates on the field-mask-projected response. Inside
the DSL:

- `items` refers to the field-mask-selected `items` array.
- `_full` (special) refers to the **unmasked** response in `dual_fetch`
  mode only. Outside `dual_fetch`, referencing `_full` is an error.
- `_fields` exposes the resolved field-mask string so DSL projections can
  introspect.

DSL reference: `gum://help/code-mode` for the alternative scripting path
and the project's `expression-profile-dsl.md` for the full grammar.

## Errors

- `FIELD_MASK_INVALID_SYNTAX` — parser rejected the mask. Envelope includes
  the column index where parsing failed.
- `FIELD_MASK_UNKNOWN_FIELD` — mask references a field absent from the
  op's declared `output_schema`.
- `FIELD_MASK_UPSTREAM_IGNORED` — `dual_fetch` mode detected the upstream
  returned more fields than the mask requested. Warning only, not fatal.

See `gum://help/toon-format` for how the projected response is serialised
back to the host client.

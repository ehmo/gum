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

The API defines this grammar and enforces it. GUM forwards the mask you
give it as the `fields` argument and does not parse it first, so a
malformed mask is rejected upstream, not by GUM. GUM parses masks only at
catalog build time, to check the curated `default_fields` it ships.

## Field-mask modes

`field_mask_mode` in the expression profile selects how the mask is applied:

- **`upstream`** (default) — pass the mask verbatim to the upstream API;
  trust the API to return only those fields.
- **`none`** — send no mask. The upstream returns the full response and
  host-side shaping prunes it. Use this when a profile's `recovery` needs
  pre-mask data.
- **`dual_fetch`** — send the masked request for the response, then send a
  second request with no mask whose body becomes the recovery artifact.
  Both requests count against rate limits, quota, and the audit log, and
  the second one is logged with `dual_fetch: true`. The mode is allowed
  only on variants with `risk_class = "read"` and
  `annotations.idempotent = true`.

## Where a mask comes from

Three sources, narrowest first:

1. The caller: `--fields` on the CLI, a positional `fields=`, or the same
   argument from MCP. An explicit caller mask always wins.
2. The active expression profile's `field_mask`.
3. The variant's curated `default_fields`.

`--no-field-mask` and `field_mask_mode = "none"` each suppress 2 and 3
entirely. The chosen mask is injected before the cache lookup, so two
calls that differ only by mask do not share one cached body.

Shell completion for `--fields` offers each top-level selector of the
op's `default_fields`, plus the whole mask. It is a starting point, not
validation: GUM does not check a mask against the op's `output_schema`.

## What `gum profile validate` checks

Structure (`profile.Parse`) and cross-field semantics
(`ValidateSemantics`). With `--variant <id>` it additionally runs the
`strip_nulls` safety check against that variant's
`null_elision_safe_fields`. It does not check field-mask coverage.

## Interaction with the expression DSL

The expression DSL operates on the field-mask-projected response, so
`items` inside the DSL is the field-mask-selected `items` array. A field
the upstream mask excluded is gone before the DSL runs, and a
`keep_fields` entry naming it keeps nothing.

DSL reference: `gum://help/code-mode` for the alternative scripting path
and the project's `expression-profile-dsl.md` for the full grammar.

## Errors

GUM has no field-mask error code. A malformed or unknown-field mask
reaches the API and comes back as that API's error.

One field-mask failure is gum's own:

- `INVALID_ARGS` with `field: "field_mask_mode"` — a profile selected
  `dual_fetch` for a variant that is not read plus idempotent. Raised
  before any upstream request.

See `gum://help/toon-format` for how the projected response is serialised
back to the host client.

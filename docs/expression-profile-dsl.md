# Expression Profile DSL

This document is normative for GUM expression profiles. `spec.md` defines runtime architecture and output-envelope effects; this file defines semantic validation for catalog, plugin, user-global, and project-local profile files. `docs/expression-profile-dsl.json` is the structural grammar only and is not a complete validator for catalog-aware, NFC-aware, or runtime-safety rules.

## Validation Layers

Expression-profile validation is deliberately split:

- `docs/expression-profile-dsl.json` validates TOML-to-JSON structural shape: known fields, primitive types, enum literals, and object/array layout.
- `docs/expression-profile-dsl.md` validates semantic authoring rules: catalog key resolution, override precedence, NFC-normalized `on_empty` length, one-level inheritance, `strip_nulls` safety coverage, `dual_fetch` risk/idempotence gates, and `tee_mode` failure semantics.
- `spec.md` validates runtime consequences: dispatch lifecycle step boundaries, `ExpressionMeta`, `ParallelResults`, recovery artifacts, and MCP/CLI envelope shapes.

A Go implementation MUST run both the structural validator and the semantic validator before accepting catalog, plugin, user-global, or project-local profiles. Passing the JSON Schema alone is insufficient.

## File Shape

Profile files contain one or both top-level tables:

- `[output_profiles]` for profile definitions.
- `[override_bindings]` for attaching an existing profile name to an op_id or variant_id.

An override-bindings-only file is valid when every referenced profile resolves through the standard hierarchy. This allows a project to rebind a catalog-embedded or user-global profile without redefining it.

```toml
[output_profiles."_base.list_ops"]
format = "toon"
strip_nulls = true
collapse_arrays = { max_items = 20 }
recovery = "local_artifact"

[output_profiles."gmail.messages.list.v1"]
inherits = "_base.list_ops"
field_mask = "nextPageToken,messages(id,threadId)"
truncate_strings = { default_chars = 500, fields = { snippet = 180 } }
on_empty = "No matching messages."
```

Project-local files live at `.gum/profiles/<profile-name>.toml`. User-global files live at `~/.config/gum/profiles/<profile-name>.toml`. Embedded catalog profiles are generated into `internal/embedded/catalog.json` / `catalog.bin`.

One file MAY define several profiles, so the loader does not stop at the file named after the profile: it reads `<profile-name>.toml` first, then every other `*.toml` in the same directory in name order, and takes the first `[output_profiles."<name>"]` table that matches. A malformed file in the search path fails the load rather than being skipped.

A catalog-embedded profile ships as a body with no `[output_profiles]` header, because the catalog already knows its name. Files in that bare-key shape hold exactly one profile and are still accepted; the two shapes MUST NOT be mixed in one file.

`gum profile test <path>` needs `--name <profile>` when the file defines more than one profile. With one definition the flag is optional.

**Variant override bindings (`[override_bindings]`).** Project-local and user-global profile files MAY include an `[override_bindings]` table that attaches a profile to one or more operations or variants explicitly, overriding the catalog-declared default profile binding. This is the runtime extensibility surface for profile authors: a new profile can be attached to existing variants without recompiling the catalog or modifying the embedded `catalog.json`.

The table maps `op_id` (or `variant_id`, for variant-precise bindings) to a profile name string. The referenced profile MUST resolve through the standard three-level hierarchy (project-local → user-global → catalog-embedded) — it does not need to be defined in the same file.

```toml
[output_profiles."gmail.messages.list.lean"]
inherits = "_base.list_ops"
field_mask = "messages(id,subject)"

[override_bindings]
"gmail.users.messages.list" = "gmail.messages.list.lean"
"gmail.v1.rest.users.messages.list" = "gmail.messages.list.lean"
```

Validation rules for `[override_bindings]`:

1. The map key MUST be a defined `op_id` or `variant_id` in the catalog view used by the caller (post-§5.1 alias normalization): the active session catalog snapshot for MCP sessions, or the one-shot startup snapshot for CLI commands. Inventory-only plugin variants (`installed_pending_restart`, `needs_configuration`, or `quarantined`) are not valid override targets until activation. A `variant_id` key wins over an `op_id` key when both target the same resolved variant. Unresolved keys fail with `OVERRIDE_BINDING_INVALID`.
2. The map value is a profile-name string. The referenced profile MUST resolve through the three-level hierarchy; dangling profile names fail with `OVERRIDE_BINDING_INVALID`.
3. `_base.*` profiles ARE valid override targets (resolved through the same hierarchy and applied as the effective profile).
4. Project-local override files win over user-global override files when both name the same key.
5. `gum profile validate` MUST enforce the above; `OVERRIDE_BINDING_INVALID` is the single structural-violation code per §7.
6. A profile file MUST contain at least one of `[output_profiles]` or `[override_bindings]`. An empty file, or a file containing only unknown top-level tables, fails schema validation.

In MCP mode, project-local lookup is rooted by the MCP `roots/list` result when the client supports roots. Only `file://` roots are valid for project-local profile lookup in v0.1.0. Multiple file roots require `_meta.gumRoot` to disambiguate; absent or non-negotiated `_meta.gumRoot` fails with `PROJECT_ROOT_REQUIRED` and no project-local override is applied. If roots are unavailable, project-local lookup is disabled and resolution falls back to user-global then catalog-embedded profiles. MCP mode never reads `GUM_PROJECT_ROOT` or `$PWD`. §9.2's `--allow-implicit-project-root` opt-in is unimplemented: no flag parses it and nothing sets `_profile_resolution_warning`, so `"implicit_project_root"` never appears in an envelope (gum-zy45).

Resolution order is project-local, then user-global, then catalog-embedded. First matching profile name wins for declared fields; undeclared fields inherit from the next lower-precedence source.

## Field Reference

| Field | Type | Default | Semantics |
|---|---|---|---|
| `format` | `toon`, `csv`, `json`, `markdown` | `toon` | Final wire encoding. `toon` is valid only for uniform array-like results. |
| `field_mask` | string | variant `default_fields` | Upstream projection expression. Syntax is provider-specific, usually Google `fields`. |
| `field_mask_mode` | `upstream`, `dual_fetch`, `none` | `upstream` | `upstream` masks the main request; `none` disables upstream masking; `dual_fetch` masks the main request and issues a second unmasked request that feeds the stage-9 artifact, at the cost of a second billed call. |
| `projection` | array of strings | `[]` | Host-side key allowlist applied before `keep_fields`. Flat key names only, no dot paths: it keeps the named keys of a top-level object, or of each element of a top-level array, and leaves nested maps alone. Empty keeps every key. Use `keep_fields` for nested paths. |
| `keep_fields` | array of strings | `[]` | Recursive post-upstream allowlist. Dot paths address nested fields. |
| `drop_fields` | array of strings | `[]` | Recursive post-upstream denylist. Applied after `keep_fields`. |
| `strip_nulls` | boolean | `false` | Remove null, empty string, empty object, and empty array fields only where the selected variant declares `null_elision_safe_fields`. |
| `flatten` | boolean | `false` | Unwrap common envelopes such as `{items:[...]}`, `{data:[...]}`, or provider-specific configured wrappers. |
| `flatten_singletons` | boolean | `false` | Unwrap a single-element array to the value it contains. Applied after `flatten`, which unwraps envelopes. |
| `collapse_arrays` | object | off | Truncate arrays and emit `omitted_count`. A per-call `max_items` override wins over this value. |
| `truncate_strings` | object | off | Truncate long strings and emit truncation metadata. |
| `dedupe` | object | off | Collapse duplicate rows by stable key fields. The surviving row carries `occurrence_count`. |
| `sort_by` | string | none | Sort the record array ascending by this key. The sort is stable, so equal keys keep upstream order; numbers compare numerically, strings lexically, a row missing the key sorts ahead of every row that has one, and a non-object element is never reordered. Applied after `dedupe` and before `limit`. |
| `limit` | integer >= 0 | `0` | Maximum records retained in the record array. `0` means no limit. Applied after `sort_by`. Removed rows count toward `_expression.omitted_count` and appear in the shaping notice. |
| `omit_zero_counts` | boolean | `false` | Drop integer fields whose value is `0` while encoding. It is an encoder option, not a shaping stage: it changes the bytes at stage 8 and applies only to `format = "toon"`. |
| `recovery` | `none`, `local_artifact`, `resource_link` | `none` | `local_artifact` writes filesystem tee only; `resource_link` writes filesystem tee and advertises `gum://results/<hash>` unconditionally in MCP mode. Advertised recovery links appear both as `_expression.full_result_resource` in `structuredContent` and as one MCP `resource_link` content block in the tool result `content[]`. CLI calls omit the resource link and use `full_result_path`. |
| `inherits` | string | none | One-level base profile. Bases may be named `_base.*` by convention. |
| `on_empty` | string, max 500 Unicode codepoints | none | Message surfaced whenever the shaped result carries no records, whether shaping removed them or the upstream response was already empty. Spec §9.4 requires `gum.search_apis` to answer a zero-hit query with its `on_empty` string, and §9.1 discriminator 5 pairs the message with `intentional_zero_max_items` on an empty upstream, so the message cannot mark shaping loss on its own. `_expression.result_count` and `_expression.omitted_count` are the discriminator: §9.1 rule 3 reads `(0, 0)` as "the upstream API returned zero results" and `(0, N)` as "results existed but were shaped away". Strings exceeding 500 Unicode codepoints (NFC normalized) fail `gum profile validate` with `ON_EMPTY_TOO_LONG`. The limit keeps the message inside reasonable token bounds and prevents a profile field from being repurposed as a freeform documentation blob. **NFC normalization ordering (normative)**: `on_empty` MUST be NFC-normalized before the 500-codepoint check is applied. `gum profile validate` applies NFC normalization before performing the length check; raw-input strings that exceed 500 codepoints after NFC normalization fail with `ON_EMPTY_TOO_LONG`. This ordering matters for inputs containing decomposed Unicode that would change codepoint count between pre- and post-normalization forms. **Propagated form (normative)**: the value stored at runtime in `_expression.on_empty_message` is the **NFC-normalized form**, not the raw TOML author value. This is also the form passed through the runtime in single-op responses, in `gum_parallel` per-result envelopes, and in any `gum://results/{hash}` artifact. The raw author value is not preserved in runtime output. `expression-profile-dsl.json` deliberately omits a literal `maxLength: 500` on the `on_empty` property because JSON Schema `maxLength` counts raw codepoints without NFC normalization; `gum profile validate` is the canonical validator. |
| `tee_mode` | `off`, `failures`, `always` | `always` when `recovery != none`, else `off` | Controls filesystem tee writes. `recovery="resource_link"` requires `tee_mode="always"`; `gum profile validate`, catalog build, and §9.2 profile resolution reject `resource_link` with `tee_mode="off"` or `"failures"` using `PROFILE_TEE_MODE_CONFLICT`. `off`: never write a tee artifact. `always`: write on every result for which the active expression profile is lossy (default whenever `recovery != none`), and on every trigger listed under `failures` below, because the enum is ordered `off` < `failures` < `always`. `failures` (normative definition): write a tee artifact only when the upstream HTTP response carries a 4xx or 5xx status (including HTTP 429 from the upstream server itself in §3.1 step 7, which IS a failures-tee trigger), a transport-level or post-headers body-delivery error occurs (including, but not limited to, timeout, connection refused, DNS failure, TLS error, body-read EOF, chunked-transfer-encoding parse error, content-length mismatch, gzip/zstd decompression failure), or the upstream returns a structured error envelope that `internal/dispatch` maps to an error result. Expression-pipeline shaping that produces an empty result (e.g., `on_empty` fires, `collapse_arrays.max_items=0`) is NOT a failure for `tee_mode` purposes; the pipeline succeeded. **Pre-upstream dispatch errors are excluded (normative)**: every error that fires in §3.1 lifecycle steps 1–6 (i.e., before step 7 "Executor call" issues the upstream request) is NOT a `tee_mode = "failures"` trigger, because no upstream response payload exists to artifact and writing a zero-byte artifact would corrupt the `gum://results/{hash}` reverse-lookup. The dispatcher MUST skip the tee write for these paths regardless of `tee_mode` value. The §3.1 pre-step-7 error codes excluded by this rule are (non-exhaustive enumeration, anchored on §3.1 steps; the §3.1 step boundary is authoritative): step 2 — `OP_NOT_FOUND`, `VARIANT_NOT_FOUND`, `VARIANT_QUARANTINED`, `AMBIGUOUS_VARIANT`; step 3 — `UNSUPPORTED_CAPABILITY`; step 4 — `RISK_TOOL_MISMATCH`; step 5 — `AUTH_REQUIRED`, `SCOPE_MISSING`; step 6 — `RATE_LIMITED` (ONLY the in-process token-bucket pre-rejection variant; any `RATE_LIMITED` derived from a §3.1 step 7 upstream response — whether the upstream returned HTTP 429, 503+`Retry-After`, or any other status that `internal/dispatch` maps to `RATE_LIMITED` — is NOT excluded and triggers the failures tee under the positive 4xx/5xx and structured-error-envelope clauses above), `REQUIRES_CONFIRMATION`, `CONFIRMATION_TOKEN_INVALID`, `INVALID_ARGS` (pre-flight validation). Any future §7 error code whose firing point is in §3.1 steps 1–6 is also excluded by this rule; the lifecycle-step boundary, not the code list, is the contract. Catalog-build sanitizer rejections and profile-validate-time errors are also excluded because they occur before any §3.1 lifecycle starts. |

## Sub-Fields

`collapse_arrays`:

| Field | Type | Required | Semantics |
|---|---|---:|---|
| `max_items` | integer >= 0 | yes | Maximum array items retained after shaping. `0` is valid only when `on_empty` is set. A caller MAY override this value for one invocation with the CLI `--max-items` flag or the MCP `max_items` argument; the override replaces this cap and `"all"` skips stage 5 entirely. |

`truncate_strings`:

| Field | Type | Required | Semantics |
|---|---|---:|---|
| `default_chars` | integer >= 1 | no | Default maximum characters for fields not listed in `fields`. |
| `fields` | map string -> integer >= 1 | no | Per-field character limits keyed by field name or dot path. |

`dedupe`:

| Field | Type | Required | Semantics |
|---|---|---:|---|
| `by` | array of strings | yes | Stable key fields. All listed fields must exist in the shaped row type or validation fails. |

**Occurrence counts (normative).** The surviving row of a collapsed group MUST carry `occurrence_count`, the number of rows that shared its key, so a caller reading one row can tell how many stood behind it (spec §9.1 stage 7). The field MUST be written only when that number is above 1, so a result with no duplicates keeps the upstream row shape. A row that already carries an `occurrence_count` field keeps the upstream value: the annotation reports loss and MUST NOT cause it.

## Processing Order

The pipeline order is fixed:

1. `field_mask`
2. `projection`, then `keep_fields` / `drop_fields`
3. `strip_nulls`
4. `flatten`, then `flatten_singletons`
5. `collapse_arrays`
6. `truncate_strings`
7. `dedupe`, then `sort_by`, then `limit`
8. `format`, with `omit_zero_counts` as a `toon` encoder option
9. `artifact`

Stages 2-7 operate on parsed Go `map[string]any` / `[]any` data. Stage 8 emits bytes. Stage 9 writes the post-stage-1 tree, except under `field_mask_mode = "dual_fetch"`, where it writes the body of the unmasked second request instead.

**Record array (normative).** Stage 7 operates on the shaped body's record array, which is the top-level value when it is an array, and otherwise the first present key among `items`, `data`, `messages` and `results` in a top-level object, falling back to that object's single array-valued field. Both shapes occur in practice: a Google list response is an object such as `{"messages":[...],"nextPageToken":"..."}`, and stage 5 rewrites a top-level array into `{"items":[...],"omitted_count":N}`. An object with two or more array-valued fields and none of the named keys has no single record array, so stage 7 MUST leave the body unchanged rather than select one of them. Stage 7 and `_expression.result_count` MUST read the same key, so the notice and the envelope describe one array.

**Removed-row notice (normative).** `dedupe` and `limit` remove whole records and write no count into the body the way `collapse_arrays` does. The presentation layer MUST state how many rows each stage removed in its shaping notice; otherwise a shortened result reads as a complete one.

**Absent key fields (normative).** A row that carries none of the `dedupe.by` fields is not keyable and MUST pass through. Keying such a row on all-absent fields gives every row the same key, which would reduce the whole result set to one row when a profile names a field the body does not contain. A row that carries at least one `by` field is keyed on all of them, with an absent field contributing a null.

## Validation Rules

- A lossy profile MUST set `recovery` to `local_artifact` or `resource_link` unless the variant explicitly declares `raw_result_allowed=true` with a token-budget exception.
- `strip_nulls=true` requires the selected variant or plugin tool to declare `null_elision_safe_fields` covering every field that can be elided. Missing coverage fails with `PROFILE_STRIP_NULLS_UNSAFE`. The safe list is variant state, not profile state, so `gum profile validate` runs this check only when `--variant <variant_id>` binds one; an unbound run reports the check as skipped.
- `field_mask_mode = "dual_fetch"` is valid only for variants with `risk_class = "read"` and `annotations.idempotent = true`; it is rejected for every write or destructive variant even when the upstream operation is idempotent. An ineligible variant fails with `INVALID_ARGS` and `field: "field_mask_mode"` before any upstream request. On an eligible variant the second request is issued only when a mask actually reached the wire and a stage-9 artifact would be written; see spec.md §9.1 for the full activation conditions.
- `recovery = "none"` with lossy stages is rejected for catalog and plugin profiles. User overrides may set it, but GUM emits the recovery-disable warning defined in `spec.md`.
- Inheritance is one level. If the base itself declares `inherits`, the base's parent is ignored.
- Circular inheritance is a load error.
- Unknown fields are validation errors.
- `_base.*` profiles are abstract by convention and MUST NOT be assigned directly to catalog variants by `cmd/gen-catalog`.

## Test Format

Profile files may include `[[tests]]` entries. Catalog and plugin profiles that ship in the repository SHOULD include at least one fixture-backed test. Release-gated profiles MUST include one.

```toml
[[tests]]
name = "gmail list compact"
profile = "gmail.messages.list.v1"
fixture = "testdata/gmail/messages_list_full.json"
expect_format = "toon"
expect_max_tokens = 600
expect_lossy = true
expect_result_count = 20
expect_omitted_count = 80
expect_fields = ["id", "threadId"]
```

`gum profile validate` checks schema and inheritance. `gum profile test` additionally runs fixtures through the expression pipeline and verifies token counts with `cl100k_base`.

Both counts describe the shaped output, not the fixture (normative). `expect_result_count` is the length of the shaped record array: the top-level array, or in a top-level object the first of `items`, `data`, `messages`, `results`, and otherwise the object's single array-valued field. `expect_omitted_count` is the sum of the omitted-count fields stage 5 wrote: `omitted_count` for a collapsed bare array, and `<key>_omitted_count` for each collapsed field of an object. A profile whose default format is `toon` is re-shaped as JSON to read the two counts, so the stages are identical and only the encoder differs.

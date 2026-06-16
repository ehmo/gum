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

Project-local files live at `.gum/profiles/<profile-name>.toml`. User-global files live at `~/.config/gum/profiles/<profile-name>.toml`. Embedded catalog profiles are generated into `gen/catalog.json` / `catalog.bin`.

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

In MCP mode, project-local lookup is rooted by the MCP `roots/list` result when the client supports roots. Only `file://` roots are valid for project-local profile lookup in v0.1.0. Multiple file roots require `_meta.gumRoot` to disambiguate; absent or non-negotiated `_meta.gumRoot` fails with `PROJECT_ROOT_REQUIRED` and no project-local override is applied. If roots are unavailable, project-local lookup is disabled by default and resolution falls back to user-global then catalog-embedded profiles. `GUM_PROJECT_ROOT` / `$PWD` are used in MCP mode only when the operator starts the server with `--allow-implicit-project-root`, in which case GUM emits `_profile_resolution_warning: "implicit_project_root"` and records the chosen root in the audit log and `_expression.project_root_uri`.

Resolution order is project-local, then user-global, then catalog-embedded. First matching profile name wins for declared fields; undeclared fields inherit from the next lower-precedence source.

## Field Reference

| Field | Type | Default | Semantics |
|---|---|---|---|
| `format` | `toon`, `csv`, `json`, `markdown` | `toon` | Final wire encoding. `toon` is valid only for uniform array-like results. |
| `field_mask` | string | variant `default_fields` | Upstream projection expression. Syntax is provider-specific, usually Google `fields`. |
| `field_mask_mode` | `upstream`, `dual_fetch`, `none` | `upstream` | `upstream` masks the main request; `dual_fetch` also performs an unmasked recovery fetch and is valid only for variants with `risk_class="read"` and `annotations.idempotent=true`; `none` disables upstream masking. |
| `keep_fields` | array of strings | `[]` | Recursive post-upstream allowlist. Dot paths address nested fields. |
| `drop_fields` | array of strings | `[]` | Recursive post-upstream denylist. Applied after `keep_fields`. |
| `strip_nulls` | boolean | `false` | Remove null, empty string, empty object, and empty array fields only where the selected variant declares `null_elision_safe_fields`. |
| `flatten` | boolean | `false` | Unwrap common envelopes such as `{items:[...]}`, `{data:[...]}`, or provider-specific configured wrappers. |
| `collapse_arrays` | object | off | Truncate arrays and emit `omitted_count`. |
| `truncate_strings` | object | off | Truncate long strings and emit truncation metadata. |
| `dedupe` | object | off | Collapse duplicate rows by stable key fields. |
| `recovery` | `none`, `local_artifact`, `resource_link` | `none` | `local_artifact` writes filesystem tee only; `resource_link` writes filesystem tee and advertises `gum://results/<hash>` unconditionally in MCP mode. Advertised recovery links appear both as `_expression.full_result_resource` in `structuredContent` and as one MCP `resource_link` content block in the tool result `content[]`. CLI calls omit the resource link and use `full_result_path`. |
| `inherits` | string | none | One-level base profile. Bases may be named `_base.*` by convention. |
| `on_empty` | string, max 500 Unicode codepoints | none | Message surfaced when shaping removes all records from a non-empty upstream response. Strings exceeding 500 Unicode codepoints (NFC normalized) fail `gum profile validate` with `ON_EMPTY_TOO_LONG`. The limit keeps the message inside reasonable token bounds and prevents a profile field from being repurposed as a freeform documentation blob. **NFC normalization ordering (normative)**: `on_empty` MUST be NFC-normalized before the 500-codepoint check is applied. `gum profile validate` applies NFC normalization before performing the length check; raw-input strings that exceed 500 codepoints after NFC normalization fail with `ON_EMPTY_TOO_LONG`. This ordering matters for inputs containing decomposed Unicode that would change codepoint count between pre- and post-normalization forms. **Propagated form (normative)**: the value stored at runtime in `_expression.on_empty_message` is the **NFC-normalized form**, not the raw TOML author value. This is also the form passed through the runtime in single-op responses, in `gum_parallel` per-result envelopes, and in any `gum://results/{hash}` artifact. The raw author value is not preserved in runtime output. `expression-profile-dsl.json` deliberately omits a literal `maxLength: 500` on the `on_empty` property because JSON Schema `maxLength` counts raw codepoints without NFC normalization; `gum profile validate` is the canonical validator. |
| `tee_mode` | `off`, `failures`, `always` | `always` when `recovery != none`, else `off` | Controls filesystem tee writes. `recovery="resource_link"` requires `tee_mode="always"`; `gum profile validate`, catalog build, and plugin install reject `resource_link` with `tee_mode="off"` or `"failures"` using `PROFILE_TEE_MODE_CONFLICT`. `off`: never write a tee artifact. `always`: write on every result for which the active expression profile is lossy (default whenever `recovery != none`). `failures` (normative definition): write a tee artifact only when the upstream HTTP response carries a 4xx or 5xx status (including HTTP 429 from the upstream server itself in §3.1 step 7, which IS a failures-tee trigger), a transport-level or post-headers body-delivery error occurs (including, but not limited to, timeout, connection refused, DNS failure, TLS error, body-read EOF, chunked-transfer-encoding parse error, content-length mismatch, gzip/zstd decompression failure), or the upstream returns a structured error envelope that `internal/dispatch` maps to an error result. Expression-pipeline shaping that produces an empty result (e.g., `on_empty` fires, `collapse_arrays.max_items=0`) is NOT a failure for `tee_mode` purposes; the pipeline succeeded. **Pre-upstream dispatch errors are excluded (normative)**: every error that fires in §3.1 lifecycle steps 1–6 (i.e., before step 7 "Executor call" issues the upstream request) is NOT a `tee_mode = "failures"` trigger, because no upstream response payload exists to artifact and writing a zero-byte artifact would corrupt the `gum://results/{hash}` reverse-lookup. The dispatcher MUST skip the tee write for these paths regardless of `tee_mode` value. The §3.1 pre-step-7 error codes excluded by this rule are (non-exhaustive enumeration, anchored on §3.1 steps; the §3.1 step boundary is authoritative): step 2 — `OP_NOT_FOUND`, `VARIANT_NOT_FOUND`, `VARIANT_QUARANTINED`, `AMBIGUOUS_VARIANT`; step 3 — `UNSUPPORTED_CAPABILITY`; step 4 — `RISK_TOOL_MISMATCH`; step 5 — `AUTH_REQUIRED`, `SCOPE_MISSING`; step 6 — `RATE_LIMITED` (ONLY the in-process token-bucket pre-rejection variant; any `RATE_LIMITED` derived from a §3.1 step 7 upstream response — whether the upstream returned HTTP 429, 503+`Retry-After`, or any other status that `internal/dispatch` maps to `RATE_LIMITED` — is NOT excluded and triggers the failures tee under the positive 4xx/5xx and structured-error-envelope clauses above), `REQUIRES_CONFIRMATION`, `CONFIRMATION_TOKEN_INVALID`, `INVALID_ARGS` (pre-flight validation). Any future §7 error code whose firing point is in §3.1 steps 1–6 is also excluded by this rule; the lifecycle-step boundary, not the code list, is the contract. Catalog-build sanitizer rejections and profile-validate-time errors are also excluded because they occur before any §3.1 lifecycle starts. |

## Sub-Fields

`collapse_arrays`:

| Field | Type | Required | Semantics |
|---|---|---:|---|
| `max_items` | integer >= 0 | yes | Maximum array items retained after shaping. `0` is valid only when `on_empty` is set. |

`truncate_strings`:

| Field | Type | Required | Semantics |
|---|---|---:|---|
| `default_chars` | integer >= 1 | no | Default maximum characters for fields not listed in `fields`. |
| `fields` | map string -> integer >= 1 | no | Per-field character limits keyed by field name or dot path. |

`dedupe`:

| Field | Type | Required | Semantics |
|---|---|---:|---|
| `by` | array of strings | yes | Stable key fields. All listed fields must exist in the shaped row type or validation fails. |

## Processing Order

The pipeline order is fixed:

1. `field_mask`
2. `keep_fields` / `drop_fields`
3. `strip_nulls`
4. `flatten`
5. `collapse_arrays`
6. `truncate_strings`
7. `dedupe`
8. `format`
9. `artifact`

Stages 2-7 operate on parsed Go `map[string]any` / `[]any` data. Stage 8 emits bytes. Stage 9 writes the post-stage-1 tree, or the unmasked dual-fetch result when `field_mask_mode = "dual_fetch"`.

## Validation Rules

- A lossy profile MUST set `recovery` to `local_artifact` or `resource_link` unless the variant explicitly declares `raw_result_allowed=true` with a token-budget exception.
- `strip_nulls=true` requires the selected variant or plugin tool to declare `null_elision_safe_fields` covering every field that can be elided. Missing coverage fails with `PROFILE_STRIP_NULLS_UNSAFE`.
- `field_mask_mode = "dual_fetch"` is valid only for variants with `risk_class = "read"` and `annotations.idempotent = true`; it is rejected for every write or destructive variant even when the upstream operation is idempotent.
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

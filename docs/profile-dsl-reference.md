# Expression Profile DSL — Author Reference

This is the **author-facing** companion to `docs/expression-profile-dsl.md` (the normative specification) and `docs/expression-profile-dsl.json` (the structural JSON Schema). Where the spec defines *what* the validator and runtime do, this reference shows *how to write profiles* with worked examples for every operator, the operator composability matrix, and the full set of validator error codes with example triggers.

Read order for new authors:
1. **This document** — the field-by-field walkthrough with input/output examples.
2. `docs/expression-profile-dsl.md` — normative semantics, override-binding rules, and the §3.1 lifecycle boundary for `tee_mode = "failures"`.
3. `docs/plugin-contract.md` §profiles — how to ship a profile inside a plugin manifest.

`apps/gum/internal/output/profile/applier.go` and two tests beside it cite this document by section. The `gum://help/profiles` topic covers the same operators and does not link here.

---

## 1. Quick start: from raw response to shaped output

The pipeline order is fixed (`docs/expression-profile-dsl.md` §Processing Order). Stages 1–7 transform a parsed Go tree; stage 8 emits bytes; stage 9 writes the recovery artifact.

```text
upstream JSON ──[1 field_mask]── parsed tree
                 [2 projection, keep/drop_fields]
                 [3 strip_nulls]
                 [4 flatten, flatten_singletons]
                 [5 collapse_arrays]
                 [6 truncate_strings]
                 [7 dedupe, sort_by, limit]
                ── [8 format + omit_zero_counts] ── bytes ── client
                ── [9 artifact] ── disk (recovery)
```

A profile is valid as long as **every stage with side effects** is declared in the operator catalogue and the **recovery contract** (rule below) is satisfied.

### Recovery contract (binding for every lossy profile)

If any of stages 2–7 modify the data observably (drop fields, truncate strings, collapse arrays, dedupe rows), the profile is **lossy** and MUST set `recovery = "local_artifact"` or `recovery = "resource_link"`. The only exception is when the catalog variant declares `raw_result_allowed = true` with an explicit token-budget exception. Violations are rejected by `gum profile validate` and by `cmd/gen-catalog` at catalog build time.

---

## 2. Operator catalogue

Each entry lists: **type signature** (input tree shape → output tree shape), **purpose**, **example profile fragment**, and **before/after for a representative input**.

### 2.1 `field_mask` — upstream projection

- **Signature**: passed to the upstream provider; does not transform the local tree. The local tree is whatever the upstream returns after applying the mask.
- **Type**: `string`
- **Default**: variant `default_fields`
- **Purpose**: reduce the size of the upstream HTTP response *before* it hits the wire.

```toml
[output_profiles."gmail.list.compact"]
field_mask = "nextPageToken,messages(id,threadId)"
recovery = "local_artifact"
```

Before (without mask): the Gmail `messages.list` response would return every field per message (~300 bytes/row). After: only `id` and `threadId` (~40 bytes/row).

**Modes** (`field_mask_mode`):

| Value | Effect | Allowed on |
|---|---|---|
| `upstream` (default) | Mask applied to the wire request. Recovery artifact contains the masked tree. | any variant |
| `dual_fetch` | Applies the mask on the wire and issues a second unmasked request whose body feeds the recovery artifact. Both requests count against rate limits, quota, and the audit log. Use `none` instead when one unmasked request is enough. | only `risk_class = "read"` + `annotations.idempotent = true`; the second request is also skipped when no mask reached the wire or no artifact would be written |
| `none` | No mask sent to upstream; the local pipeline still runs. | any variant |

### 2.2 `projection` — top-level key allowlist

- **Signature**: `tree → tree`, keeping only the named keys.
- **Type**: array of flat key names. Dot paths are not read; use `keep_fields` for nested fields.
- **Scope**: the keys of a top-level object, or the keys of each element of a top-level array. It does not descend into nested maps.
- **Order**: runs before `keep_fields`, at the head of stage 2.

```toml
[output_profiles."gmail.list.ids"]
projection = ["messages", "nextPageToken"]
recovery = "local_artifact"
```

| Input | Output |
|---|---|
| `{"messages":[{"id":"m1"}],"nextPageToken":"t","resultSizeEstimate":1}` | `{"messages":[{"id":"m1"}],"nextPageToken":"t"}` |

### 2.3 `keep_fields` / `drop_fields` — post-upstream projection

- **Signature**: `tree → tree` with deep field filtering.
- **Type**: arrays of dot-paths.
- **Order**: `keep_fields` runs first; `drop_fields` runs after (so `drop_fields` can prune fields a generous `keep_fields` left in).

```toml
[output_profiles."drive.list.lean"]
keep_fields = ["files.id", "files.name", "files.mimeType"]
drop_fields = ["files.modifiedTime"]
recovery = "local_artifact"
```

| Input | Output |
|---|---|
| `{"files":[{"id":"a","name":"x","mimeType":"text","modifiedTime":"2026-05-23","size":42}]}` | `{"files":[{"id":"a","name":"x","mimeType":"text"}]}` |

### 2.4 `strip_nulls` — empty-value elision

- **Signature**: `tree → tree`, removing `null`, `""`, `{}`, `[]` *only* on paths the variant declared `null_elision_safe_fields` for.
- **Default**: `false`.
- **Failure mode**: missing safety coverage fails validation with `PROFILE_STRIP_NULLS_UNSAFE` (see §3).
- **Binding**: the safe list lives on the catalog variant, not in the profile file, so `gum profile validate <path>` cannot run this check on its own. Pass `--variant <variant_id>` to bind one. Without it the command reports that the check was skipped and validates everything else.

```toml
[output_profiles."gmail.message.compact"]
strip_nulls = true
recovery = "local_artifact"
```

| Input | Output (assuming `payload.headers` is safe-marked) |
|---|---|
| `{"id":"m1","payload":{"headers":[],"snippet":""}}` | `{"id":"m1","payload":{}}` |

### 2.5 `flatten` — envelope unwrap

- **Signature**: `tree → tree` unwrapping a single-key wrapper such as `{items:[...]}`, `{data:[...]}`, or a variant-configured custom wrapper.
- **Default**: `false`.

```toml
[output_profiles."calendar.events.flat"]
flatten = true
recovery = "local_artifact"
```

| Input | Output |
|---|---|
| `{"items":[{"id":"e1"},{"id":"e2"}]}` | `[{"id":"e1"},{"id":"e2"}]` |

### 2.6 `flatten_singletons` — single-element array unwrap

- **Signature**: `tree → tree`, replacing a one-element array with the element.
- **Default**: `false`.
- **Order**: runs after `flatten`, which unwraps envelopes by key. The two are independent: `flatten` removes a wrapper, this removes an array of length one.

```toml
[output_profiles."calendar.event.single"]
flatten = true
flatten_singletons = true
recovery = "local_artifact"
```

| Input | Output |
|---|---|
| `{"items":[{"id":"e1"}]}` | `{"id":"e1"}` |

### 2.7 `collapse_arrays` — bounded list truncation

- **Signature**: `[...]` (array) → `[...truncated]` with sibling `omitted_count`.
- **Type**: object with required `max_items: int >= 0`.

```toml
[output_profiles."mail.list.top20"]
collapse_arrays = { max_items = 20 }
recovery = "local_artifact"
```

| Input (100-element array) | Output |
|---|---|
| `[m1, m2, ..., m100]` | `{"items":[m1..m20], "omitted_count": 80}` |

`max_items = 0` is valid only when `on_empty` is set (the profile is telling the runtime "always collapse to empty and use this message").

A caller can replace this cap for one invocation with the CLI `--max-items` flag or the MCP `max_items` argument. `--max-items all` skips stage 5, so every element survives. The override never enters the cache key or the args hash.

### 2.8 `truncate_strings` — long-string clamp

- **Signature**: `tree → tree`, clamping strings to a default character limit with per-field overrides.
- **Type**: object with optional `default_chars: int >= 1` and optional `fields: map<string,int>`.

```toml
[output_profiles."gmail.list.short"]
truncate_strings = { default_chars = 500, fields = { snippet = 180 } }
recovery = "local_artifact"
```

| Field | Limit applied |
|---|---|
| `snippet` | 180 chars |
| `subject` | 500 chars (default) |
| `body.text` | 500 chars (default) |

Truncated values are followed by sibling metadata `<field>_truncated = true`. A
value that fits inside its limit gets no sibling, so the flag's presence is the
signal. An upstream field of the same name is overwritten only when this stage
actually clamped its sibling.

The limit counts the appended `…`, so a field limited to 180 characters is
delivered in at most 180 characters. A string inside an array has no field name
and therefore no sibling flag; the ellipsis is its only signal.

### 2.9 `dedupe` — stable-key row collapse

- **Signature**: `[row, row, ...]` → `[unique_row, ...]`, keyed by listed fields.
- **Type**: object with required `by: [string]`.

```toml
[output_profiles."contacts.unique"]
dedupe = { by = ["email"] }
recovery = "local_artifact"
```

| Input | Output |
|---|---|
| `[{"email":"a@x","ts":1},{"email":"a@x","ts":2},{"email":"b@x","ts":3}]` | `[{"email":"a@x","ts":1},{"email":"b@x","ts":3}]` |

Dedupe is *stable*: the first occurrence wins. A `dedupe.by` key that no row carries is not a validation error: `gum profile validate` has no row type to check it against and accepts the profile. At runtime the missing key reads as the empty value, so every row with that key absent collapses to one.

### 2.10 `sort_by` — record array ordering

- **Signature**: `[row, ...] → [row, ...]`, ascending by one key.
- **Type**: string, a flat key of the row object.
- **Stability**: the sort is stable, so rows with equal keys keep upstream order. Numbers compare numerically, strings lexically. A row that lacks the key sorts as if its value were null, which is ahead of every present value. A non-object element is never reordered.
- **Order**: runs after `dedupe` and before `limit`, so `limit` keeps the top N by this key.

```toml
[output_profiles."gmail.list.newest"]
sort_by = "internalDate"
limit = 20
recovery = "local_artifact"
```

| Input | Output |
|---|---|
| `[{"id":"b","ts":2},{"id":"a","ts":1}]` | `[{"id":"a","ts":1},{"id":"b","ts":2}]` |

### 2.11 `limit` — record count cap

- **Signature**: `[row, ...] → [row, ...]`, keeping the first N.
- **Type**: integer >= 0. `0` means no cap.
- **Accounting**: the rows it removes reach `_expression.omitted_count` and the shaping notice. `limit` writes no count into the body the way `collapse_arrays` does, so without the notice a shortened result reads as a complete one.
- **Difference from `collapse_arrays`**: `collapse_arrays` caps every array in the tree and a caller can override it per call with `--max-items`. `limit` caps only the record array and has no per-call override.

```toml
[output_profiles."drive.list.first10"]
limit = 10
recovery = "local_artifact"
```

### 2.12 `format` — wire encoding

- **Signature**: `tree → bytes`.
- **Allowed values**: `toon`, `csv`, `json`, `markdown`.
- **Default**: `toon`.
- **Constraint**: `toon` is valid only for *uniform array-like results* (homogeneous shape; the validator detects this from the variant's declared output schema).

```toml
[output_profiles."gmail.list.toon"]
format = "toon"
recovery = "local_artifact"
```

### 2.13 `omit_zero_counts` — drop zero integers at encode time

- **Signature**: `tree → bytes`, an option on the stage-8 encoder rather than a shaping stage of its own.
- **Default**: `false`.
- **Scope**: the TOON encoder only. With `format = "csv"`, `"json"` or `"markdown"` the key has no effect.
- **Effect**: an integer field whose value is `0` is left out of the encoded bytes. The shaped tree still carries it, so the stage-9 recovery artifact is unaffected.

```toml
[output_profiles."ads.metrics.lean"]
format = "toon"
omit_zero_counts = true
recovery = "local_artifact"
```

### 2.14 `recovery` — artifact policy

- **Allowed values**: `none`, `local_artifact`, `resource_link`.
- **`tee_mode` interaction**: see §2.15.
- **MCP visibility**:
  - `local_artifact` → filesystem tee at `<XDG_DATA_HOME>/gum/<profile>/tee/<YYYY-MM-DD>/<op_id>/<hash>.json.gz` (`~/.local/share/...` by default); CLI sees `full_result_path`; MCP sees `_expression.full_result_path` only.
  - `resource_link` → filesystem tee + one MCP `resource_link` content block with URI `gum://results/<hash>`, plus `_expression.full_result_resource` in `structuredContent`.

### 2.15 `tee_mode` — when to write the artifact

- **Allowed values**: `off`, `failures`, `always`.
- **Default**: `always` when `recovery != none`; `off` otherwise.
- **Constraint**: `recovery = "resource_link"` *requires* `tee_mode = "always"` (validator rejects `resource_link` + `off`/`failures` with `PROFILE_TEE_MODE_CONFLICT`).
- **`failures` boundary**: the artifact is written only when an upstream HTTP error fires in dispatch step 7 (executor call). Pre-step-7 errors (validation, auth, in-process rate-limit) are skipped. See `docs/expression-profile-dsl.md` §`tee_mode` for the full enumeration.

### 2.16 `on_empty` — empty-result hint

- **Type**: string, max 500 NFC-normalized Unicode codepoints.
- **Triggered**: when the post-shaping result is empty *but* the upstream response was non-empty (so the user knows it's a shaping artifact, not "the server returned nothing").
- **Storage**: the runtime stores the **NFC-normalized form** in `_expression.on_empty_message`. The raw author value is not preserved.

```toml
[output_profiles."mail.list"]
collapse_arrays = { max_items = 20 }
on_empty = "No matching messages."
recovery = "local_artifact"
```

### 2.17 `inherits` — one-level base profile

- **Type**: string (profile name).
- **Depth**: exactly one level. If the named base also declares `inherits`, the *grandparent* is ignored.
- **Failure modes**: circular inheritance and dangling base names are validate-time errors.

```toml
[output_profiles."_base.list_ops"]
format = "toon"
strip_nulls = true
recovery = "local_artifact"

[output_profiles."gmail.list.v1"]
inherits = "_base.list_ops"
field_mask = "messages(id,subject)"
on_empty = "No mail."
```

`_base.*` profiles are abstract by convention and MUST NOT be bound directly to a catalog variant by `cmd/gen-catalog`.

---

## 3. Validator error catalogue

Every code listed below is raised by `gum profile validate` and / or `cmd/gen-catalog` and / or `gum plugin install`. The "trigger" column shows a minimal profile snippet that fires the code.

"Profile resolution" in the "Where raised" column means the check also runs when the §9.2 resolver loads a project-local or user-global profile, so a file that never sees `gum profile validate` is still rejected before it shapes a response.

| Error code | Where raised | Trigger |
|---|---|---|
| `PROFILE_STRIP_NULLS_UNSAFE` | `gum profile validate --variant`, build | `strip_nulls = true` on a variant that does not list every elidable field under `null_elision_safe_fields`. Needs a bound variant; see §2.4. |
| `PROFILE_TEE_MODE_CONFLICT` | validate, profile resolution | `recovery = "resource_link"` together with `tee_mode = "off"` or `tee_mode = "failures"`. |
| `ON_EMPTY_TOO_LONG` | validate, profile resolution | `on_empty` value exceeding 500 codepoints **after NFC normalization**. |
| `OVERRIDE_BINDING_INVALID` | validate | `[override_bindings]` row whose key is not a known `op_id` / `variant_id` in the active catalog view, or whose value names a profile that doesn't resolve. |
| `PROFILE_NO_FIXTURES` | `gum profile test` | Profile names no `[[tests]]` entries and `--input` was not passed. |
| `PROFILE_FIXTURE_FAILED` | `gum profile test` | One or more `[[tests]]` fixtures exceeded `expect_max_tokens`, missed `expect_result_count`, etc. |
| `PROFILE_GOLDEN_MISMATCH` | `gum profile test --golden` | Fixture output diverged from the named golden file. |
| `PROFILE_NOT_FOUND` | runtime resolver, `gum profile test --name` | A profile-name reference (in catalog binding, override, or `inherits`) does not resolve through project-local → user-global → catalog-embedded, or `--name` names a definition the given file does not carry. |
| `PROFILE_AMBIGUOUS` | `gum profile test` | The file defines more than one profile and no `--name <profile>` said which to run. |

The runtime never returns a `RESOURCE_NOT_FOUND` for an unresolved profile; it returns `PROFILE_NOT_FOUND` so callers can distinguish "the URL is wrong" from "the profile binding is wrong".

### Shadowing warning: `OVERRIDE_DISABLES_LOSSY_STAGE`

This one is a warning, not an error. It never blocks a call. `gum profile validate` and the runtime profile loader compare a project-local or user-global override against the catalog-embedded profile it displaces, and report each loss-driving field the override gives up:

| Field | Trigger |
|---|---|
| `recovery` | non-`none` in the catalog, `none` or absent in the override |
| `field_mask_mode` | `upstream` or `dual_fetch` in the catalog, `none` in the override |
| `strip_nulls` | `true` in the catalog, `false` in the override |
| `collapse_arrays` | set in the catalog, absent from the override |
| `truncate_strings` | set in the catalog, absent from the override |
| `dedupe` | set in the catalog, absent from the override |

Absence counts for `recovery` because resolution swaps the whole profile rather than merging the two bodies, and an empty `recovery` leaves no artifact. `field_mask_mode` reads the other way: empty means `upstream`, so omitting it keeps the mask and stays silent.

The line goes to stderr, or to the log for `gum mcp --stdio`, where stdout belongs to the transport:

```text
OVERRIDE_DISABLES_LOSSY_STAGE Warning: profile override for gmail.messages.list sets recovery=none, removing a lossy-compression stage (was resource_link). Token savings for this op may be reduced. Pass --no-warn-lossy to suppress.
```

Both shadow shapes warn: a definition that reuses a catalog-embedded profile's name, and an `[override_bindings]` row that attaches a weaker profile to an op. A binding is reported under the op it binds. `--no-warn-lossy` suppresses the warning; `--no-warn-recovery` is a deprecated alias.

A session that engages such an override is outside the ≥80% savings envelope (spec §9.2, "Savings-claim scope"). The warning is how an operator sees that happen.

---

## 4. Operator composability matrix

Rows are the *prior* stage; columns are the *next* stage. ✓ = composes (the typical case); ⚠ = composes but with a constraint noted below; — = disallowed by the pipeline (the order is fixed, so most cells are vacuously valid as long as both operators are declared in the same profile).

|              | strip_nulls | flatten | collapse_arrays | truncate_strings | dedupe | format |
|---|---|---|---|---|---|---|
| **field_mask** | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| **keep/drop_fields** | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| **strip_nulls** | — | ✓ | ✓ | ✓ | ⚠ A | ✓ |
| **flatten** | — | — | ✓ | ✓ | ✓ | ✓ |
| **collapse_arrays** | — | — | — | ✓ | ⚠ B | ✓ |
| **truncate_strings** | — | — | — | — | ✓ | ✓ |
| **dedupe** | — | — | — | — | — | ✓ |

**Constraint A.** `strip_nulls` followed by `dedupe.by = ["x"]` is only meaningful if `x` is *not* one of the fields elided by `strip_nulls`; the validator rejects the combination when `x` is in `null_elision_safe_fields` (otherwise the dedupe key vanishes for some rows).

**Constraint B.** `collapse_arrays.max_items = N` followed by `dedupe` collapses *within* the truncated window. The remaining `omitted_count` is unchanged; dedupe operates only on retained rows. Dedupe-before-collapse is not built. It would need a transformer pipeline, and no profile declares one.

**The five stage-mates.** The matrix lists one operator per stage. `projection` runs at the head of stage 2, `flatten_singletons` at the tail of stage 4, `sort_by` then `limit` at the tail of stage 7, and `omit_zero_counts` inside stage 8. Each composes with every earlier stage the same way its stage-mate does. Two orderings matter within a stage: `projection` runs before `keep_fields`, so a key `projection` drops cannot be named by a later `keep_fields` path; `sort_by` runs before `limit`, so `limit` keeps the top N by the sort key rather than the first N upstream sent.

---

## 5. Worked end-to-end profile

Putting it all together for a Gmail message-list endpoint:

```toml
[output_profiles."_base.list_ops"]
format = "toon"
strip_nulls = true
collapse_arrays = { max_items = 20 }
recovery = "local_artifact"

[output_profiles."gmail.messages.list.v1"]
inherits = "_base.list_ops"
field_mask = "nextPageToken,messages(id,threadId,snippet,from,subject,internalDate)"
truncate_strings = { default_chars = 500, fields = { snippet = 180 } }
on_empty = "No matching messages."

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

Pipeline trace for a 100-message response:

1. Upstream returns ~100 messages with the fields requested by `field_mask`.
2. No `keep/drop_fields` declared — tree unchanged.
3. `strip_nulls` removes `snippet: ""` and similar empties on safe-marked paths.
4. No `flatten` declared — tree unchanged.
5. `collapse_arrays` keeps the first 20, emits `omitted_count: 80`.
6. `truncate_strings` clamps each `snippet` to 180 chars, others to 500.
7. No `dedupe` declared — order preserved.
8. `format = "toon"` emits the bytes.
9. `recovery = "local_artifact"` writes the post-step-1 tree (the original 100-message tree projected by `field_mask`) to `~/.local/share/gum/<profile>/tee/<YYYY-MM-DD>/gmail.users.messages.list/<hash>.json.gz`.

`gum profile test <profile-path>` runs the fixture through stages 1–8 and checks every `expect_*` assertion with `cl100k_base` for `expect_max_tokens`.

---

## 6. Cross-references

- Normative spec: `docs/expression-profile-dsl.md`
- JSON Schema: `docs/expression-profile-dsl.json`
- Runtime envelope effects (`_expression.*`, `_expression.full_result_resource`): spec §9.0–9.2
- Plugin-shipped profile rules: `docs/plugin-contract.md` §profiles
- In-binary help topic: `gum://help/profiles`

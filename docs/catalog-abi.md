# Catalog ABI

This document is normative for GUM catalog evolution. `spec.md` remains the full product contract; this file isolates the extension ABI that must stay stable as new Google APIs, interfaces, and plugins are added.

## Versioned Artifacts

| Artifact | Version field | Loader behavior |
|---|---|---|
| Embedded catalog | `catalog_schema_version` | Reject unsupported future versions with `CATALOG_SCHEMA_UNSUPPORTED`. |
| Operation record | `op_schema_version` | Reject unsupported major shape changes. |
| Variant record | `variant_schema_version` | Reject unsupported major shape changes. |
| Plugin manifest | `manifest_schema_version` | Reject unsupported versions with `PLUGIN_MANIFEST_SCHEMA_UNSUPPORTED`. |
| Plugin runtime catalog | `plugin_catalog_schema_version` | Reject unsupported versions with `PLUGIN_CATALOG_SCHEMA_UNSUPPORTED`. |
| Plugin lockfile | `plugins_lock_schema_version` | Reject unsupported versions with `PLUGIN_LOCK_SCHEMA_UNSUPPORTED`. |
| Plugin state | `plugin_state_schema_version` | Reject unsupported versions with `PLUGIN_STATE_SCHEMA_UNSUPPORTED`. |
| Expression profile DSL | JSON Schema `$id` ending in `.vN.json` (current: `expression-profile-dsl.v1.json`) | Validate before build/install/runtime load. Incompatible DSL semantics bump the major suffix. |

Additive fields are allowed when older binaries can ignore them without changing dispatch behavior. Incompatible field semantics require a version increment.

## Stable Identifiers

- `op_id` is the stable LLM-facing capability identity.
- `variant_id` is the executable backend identity.
- `profile` is a user/account configuration context.
- `output_profile` names an expression-profile DSL record.
- `schema_ref` names a JSON Schema 2020-12 document. Served schema refs MUST match `^[a-z0-9][a-z0-9._-]{0,127}$`, MUST NOT contain `..`, and MUST NOT contain raw or percent-encoded path separators or control characters.
- `capabilities[]` is a closed enum except for `x-*` metadata-only atoms on schema-only variants.
- `auth_strategy` and `auth_components[]` describe how a variant is authorized.
  `auth_strategy` uses the closed enum owned by `spec.md` §7
  (`gum_oauth`, `byo_oauth`, `adc`, `service_account`, `api_key`, `compound`,
  `plugin_managed`, `none`). `auth_components[]` uses the standardized
  component kinds in `spec.md` §7 and fails build/install with
  `AUTH_COMPONENT_UNKNOWN` for unknown non-`x-*` values. Secret component
  values are never stored in `catalog.json`; the catalog stores descriptors and
  setup hints only. Variants using `auth_strategy="gum_oauth"` may reference
  only scopes listed as active, verified, project-ready, and live-canary-passing
  in `apps/gum/internal/embedded/data/auth-managed-scopes.v1.json`; otherwise
  generation fails with `GUM_OAUTH_SCOPE_NOT_MANAGED` or
  `GUM_OAUTH_MANAGED_CLIENT_NOT_READY`.
- `null_elision_safe_fields` names fields where an expression profile may erase null, empty-string, empty-object, or empty-array values. The optional `"*"` value means whole-response elision has been curator-reviewed.

## Variant Lifecycle

New variants may be added for API versions, interfaces, SDK backends, plugins, or capability implementations without changing `op_id`.

Deprecated variants stay invokable by explicit `variant_id` for 90 days unless quarantined. If replaced, `superseded_variant_ids` maps old id to new id. Removed variants return `VARIANT_NOT_FOUND`; they must not silently fall back to the default variant. Quarantined variants return `VARIANT_QUARANTINED`.

## Capability Policy

Unknown executable capability atoms fail closed with `UNKNOWN_CAPABILITY`. Experimental atoms must be prefixed `x-` and may only appear on variants with `execution_support = "schema_only"`.

Adding a new executable capability class requires generator validation, executor support, describe/invoke behavior, documentation, and fixture-backed tests. The full normative checklist lives in `spec.md` §5.8; the test-matrix gates live in `docs/test-matrix.md`.

## Admin Write Policy

Admin SDK variants with `service = "admin"` and `risk_class != "read"` MUST
carry `admin_policy`. The field is additive for non-Admin and read-only
variants, but required for Admin writes because ordinary write/destructive risk
classes do not express tenant blast radius.

`admin_policy.blast_radius` is a closed enum:

| Value | Meaning | Catalog eligibility |
| --- | --- | --- |
| `admin_fixture_write` | Mutates only a deterministic GUM fixture user, group, or member. | Eligible for the broad preview. |
| `admin_reversible_write` | Mutates a real directory object with a bounded rollback path. | Schema-only or future release only. |
| `admin_high_blast_write` | Can affect tenant access, privileges, security, domains, retention, billing, or many users. | Excluded from the embedded catalog. |

For `admin_fixture_write`, the variant MUST also set
`fixture_ownership_required=true`, `fixture_marker_prefix="gum-fixture-"`, and
non-empty `fixture_resource_keys`. The generator and catalog validation reject
Admin write/destructive variants that omit `admin_policy` or use an unknown
blast radius.

## Output-Profile Safety

Variants that allow `strip_nulls=true` in their assigned expression profile MUST declare `null_elision_safe_fields`. The generator and plugin installer reject profiles that strip fields outside this list with `PROFILE_STRIP_NULLS_UNSAFE`. Omit the field, or set it to an empty array, when explicit nulls or empty values carry semantic meaning for the operation.

## Service Root Extension Point

Variant records MAY include an optional `service_root_template` string field starting in v0.4.0. In v0.1.0-v0.3.x this field is reserved: `cmd/gen-catalog` MUST reject first-party or plugin manifests that set it with `SERVICE_ROOT_TEMPLATE_DEFERRED`, and runtime dispatch always uses the discovery-derived `rootUrl` / `servicePath` already recorded in variant metadata.

**v0.1-v0.3 boundary.** Standard Google public endpoints whose discovery docs
already carry the correct `rootUrl` / `servicePath` are in scope. Sovereign,
government, private-service-connect, or universe-domain variants that require
substituting a profile-specific host are explicitly out of scope until v0.4.0.
They may be described as schema-only roadmap candidates, but they MUST NOT be
advertised as executable variants before `service_root_template` support lands.
This calibrates the "easy expansion" claim: adding public-endpoint API versions
is catalog-only when the capability/backend class exists; adding endpoint-family
selection is a runtime dispatch feature, not a manifest-only change.

Future v0.4.0 shape (illustrative only; invalid in v0.1.0-v0.3.x):

```jsonc
{
  "variant_id": "gmail.v1.rest.users.messages.list",
  "service_root_template": "https://gmail.{universe_domain}",
  ...
}
```

The placeholder `{universe_domain}` is the universe domain string (e.g., `googleapis.com`, `googleapis.us`, or a sovereign-cloud domain). When `service_root_template` is absent, the runtime uses the discovery-derived `rootUrl` / `servicePath` already recorded in the variant metadata; implementers MUST NOT synthesize hostnames from API names. When v0.4.0 enables this field, the runtime substitutes the active profile's configured `universe_domain` (default `"googleapis.com"`) before constructing the request URL.

This field is additive once v0.4.0 support lands: catalogs generated before this field was defined load correctly with the default behavior. Universe-domain support is therefore a manifest-and-catalog change, not an ABI-breaking schema version increment, but it is not active before v0.4.0.

`service_root_template` is validated at catalog-build time: the template MUST contain exactly one `{universe_domain}` placeholder and MUST begin with `https://`. Build fails with `SERVICE_ROOT_TEMPLATE_INVALID` on violation.

## Backend Kind

`backend_kind` is a **closed enum** with the same extension rules as `capabilities[]`:

| Value | Transport | ABI stability |
|---|---|---|
| `typed-rest-sdk` | `google.golang.org/api` typed client | stable |
| `discovery-rest` | Raw HTTP from discovery doc | stable |
| `raw-http` | Arbitrary HTTP, no discovery doc | stable |
| `grpc-sdk` | `cloud.google.com/go` gRPC client | stable |
| `mcp-plugin` | Shape 1 MCP subprocess | stable |
| `grpc-plugin` | Shape 2 gRPC subprocess | ABI-stable; runtime availability deferred to v0.4.0 |
| `google-ads-sdk` | Google Ads API (`googleads.googleapis.com`) REST; injects the secret `developer-token` header server-side | stable |
| `x-*` | Experimental; `execution_support = "schema_only"` required | unstable |

**Adding a new `backend_kind` value** requires a PR that:

1. Adds the value to this table with status `stable`.
2. Adds a corresponding executor implementation under `internal/adapters/*`. `internal/mcp` may register MCP surfaces for that backend but MUST NOT own executor logic.
3. Updates `cmd/gen-catalog` validation to accept the new value.
4. Adds at least one fixture-backed executor contract test (`TestBackendKind<Name>`).
5. Updates `spec.md` §5.1 variant shape examples if the new kind requires new manifest fields.

**Unknown `backend_kind` at build time**: `cmd/gen-catalog` MUST reject a manifest entry whose `backend_kind` is not in this table and is not prefixed `x-`, with error `UNKNOWN_BACKEND_KIND: '<value>' is not a known backend_kind; use 'x-<name>' for experimental kinds with execution_support = "schema_only"`.

**Unknown `backend_kind` at runtime**: The catalog loader treats an unrecognized `backend_kind` (one not in the enum above and not prefixed `x-`) as `UNSUPPORTED_CAPABILITY` with the §5.8 loader-incompatible discriminator (`loader_kind="backend_kind"`) and returns that error before any upstream call. An `x-*` backend_kind variant with `execution_support = "schema_only"` is loadable and describable but not executable; an invocation attempt returns `UNSUPPORTED_CAPABILITY` with `unsupported_capabilities`.

## Interface Kind

`interface_kind` is a **closed enum** describing the external interface shape exposed by a variant:

| Value | Meaning | ABI stability |
|---|---|---|
| `discovery-rest` | Google Discovery REST method | stable |
| `grpc` | Protobuf/gRPC method through a Go SDK | stable |
| `plugin-mcp` | Shape 1 MCP subprocess tool | stable |
| `plugin-grpc` | Shape 2 GUM gRPC subprocess method | ABI-stable; runtime availability deferred to v0.4.0 |
| `sdk-native` | Non-discovery native Go SDK surface such as GenAI or Maps | stable |
| `x-*` | Experimental; `execution_support = "schema_only"` required | unstable |

Adding a new stable `interface_kind` value follows the same PR requirements as `backend_kind`: update this table, generator validation, runtime loader behavior, docs, and a fixture-backed contract test. Unknown non-`x-*` interface kinds fail build/install with `UNKNOWN_INTERFACE_KIND`; runtime loaders fail closed with `UNSUPPORTED_CAPABILITY` using `loader_kind="interface_kind"`.

**`interface_kind` extension procedure (normative).** Promoting an `x-*` experimental interface kind to a stable value (or adding a new stable kind without an experimental precursor) is a multi-step PR sequence:

1. Land the experimental `x-<name>` value first in a PR that adds the row to this table with `ABI stability: unstable`, ships catalog records using it under `execution_support = "schema_only"`, and adds at least one fixture-backed test exercising the schema-only path.
2. Implement the runtime adapter for the kind under `internal/adapters/<kind>/`. The adapter MUST accept the binding schema, validate selector fields, and route invocations through the dispatch kernel.
3. In a separate PR, promote the kind by renaming `x-<name>` to `<name>` in this table (drop the `x-` prefix), set its `ABI stability` column to `stable`, register it in `cmd/gen-catalog`'s closed-enum validator, and flip the catalog records' `execution_support` from `schema_only` to `executable`. The promotion PR MUST update `docs/test-matrix.md` to add a `TestInterfaceKind<Name>` row, ship a fixture-backed contract test for the executable path, and update `spec.md` §5.4.2 if the new kind imposes a new capability-class requirement.
4. Removing or renaming a stable `interface_kind` value requires a deprecation cycle: the old value remains in the table marked `deprecated; superseded by <new>` for at least one minor release with `execution_support` retained, then drops out.

Catalog rebuilds during the promotion window MUST treat the experimental `x-<name>` and the stable `<name>` as distinct values; the migration is not silent. `TestInterfaceKindClosedEnum` enforces the closed-enum membership at build time.

## Backend Binding Schemas

Every executable variant MUST include exactly one nested `binding` object matching its `backend_kind`. The binding object is the data contract between `cmd/gen-catalog`, `internal/catalog`, and `internal/adapters/*`. Backend-specific fields MUST live inside `binding`; top-level variant fields are limited to common identity, lifecycle, risk, confirmation policy, capability, scope, and output-profile metadata. A binding is valid only when all referenced schema refs resolve and the named adapter exists in the adapter registry. Runtime loaders reject executable variants that omit `binding`, carry more than one binding, or place backend-specific fields at the variant top level with `CATALOG_SCHEMA_UNSUPPORTED`.

Common binding fields:

| Field | Type | Required | Semantics |
|---|---|---:|---|
| `binding_schema_version` | integer | yes | Starts at 1 per backend binding kind. Unsupported future versions fail with `BINDING_SCHEMA_UNSUPPORTED`. |
| `adapter_key` | string | yes | Stable registry key implemented by `internal/adapters/*`; adding a new key requires adapter code and a same-PR test. |
| `operation_key` | string | yes | Adapter-local operation identifier; stable across catalog rebuilds. |
| `request_ref` | string | yes | JSON Schema ref for normalized input args; same safe grammar and collision rules as `schema_ref`. |
| `response_ref` | string | yes | JSON Schema ref for normalized result before expression-profile shaping; same safe grammar and collision rules as `schema_ref`. |

`typed-rest-sdk` and `discovery-rest` bindings use REST fields inside `binding`: `go_pkg`, `go_call`, `http.method`, `http.path`, params, and optional service metadata. `raw-http` bindings use the same `http` object but have no typed `go_call`.

`grpc-sdk` binding object:

```jsonc
{
  "binding_schema_version": 1,
  "adapter_key": "spanner.grpc",
  "operation_key": "google.spanner.v1.Spanner.ExecuteSql",
  "go_pkg": "cloud.google.com/go/spanner",
  "proto_service": "google.spanner.v1.Spanner",
  "proto_method": "ExecuteSql",
  "request_type": "google.spanner.v1.ExecuteSqlRequest",
  "response_type": "google.spanner.v1.ResultSet",
  "request_ref": "spanner.execute_sql.request",
  "response_ref": "spanner.execute_sql.response",
  "routing_headers": ["database"]
}
```

**`routing_headers` invariant (normative).** The `routing_headers` array enumerates the names of request-message fields whose runtime values are extracted by the dispatch layer and added to the outbound gRPC metadata under the canonical `x-goog-request-params` header per [AIP-4222](https://google.aip.dev/4222). The list MUST satisfy all of the following:

1. **Closed alphabet.** Each entry MUST be a non-empty string matching `^[a-z][a-zA-Z0-9_.]{0,127}$`. Entries are JSON field paths into the resolved request object using dotted notation for nested fields (e.g., `database`, `parent`, `instance.config.name`); array indexing is NOT supported (gRPC routing headers do not address into repeated fields).
2. **Existence and presence.** Every entry MUST correspond to a field path that exists in the resolved `request_ref` JSON Schema (post-`$ref` resolution). `cmd/gen-catalog` MUST validate this at catalog-build time and fail the build with `GRPC_ROUTING_HEADER_NOT_FOUND` if a listed path does not resolve. Optional fields are allowed; the dispatcher MUST omit the header at runtime when the field is absent or empty rather than emit an empty value.
3. **No duplicates.** Duplicate entries fail the build with `GRPC_ROUTING_HEADER_DUPLICATE`. Order is preserved for deterministic header emission (the dispatch layer concatenates header parameters in the order listed).
4. **Bindings that do not require routing headers.** Many `grpc-sdk`
   operations do not require explicit routing headers because the
   underlying `cloud.google.com/go` SDK already derives the correct
   metadata from the request payload. For these operations,
   `routing_headers` MUST be omitted (not present as an empty array).
   `cmd/gen-catalog` MUST fail the build with
   `GRPC_ROUTING_HEADER_NOT_REQUIRED` if a variant declares
   `"routing_headers": []` — the empty-array form is a curator mistake;
   absence is the correct encoding.
5. **Stability under catalog regeneration.** Once a variant ships with a non-empty `routing_headers` list, subsequent catalog regenerations MUST NOT silently drop entries. If an upstream service redefinition removes a routing-header field, the curator MUST update the override file explicitly (per the §5.4.1 expansion checklist); `cmd/gen-catalog` will fail the build until the override is resolved.

`TestGrpcRoutingHeaderInvariant` (in `internal/catalog/grpc_routing_test.go`) verifies points 1–4 on a fixture set covering both present and omitted forms. Point 5 is enforced by the gen-catalog override diffing pass.

`sdk-native` binding object:

```jsonc
{
  "binding_schema_version": 1,
  "adapter_key": "genai.models",
  "operation_key": "models.generateContent",
  "go_pkg": "google.golang.org/genai",
  "go_call": "Models.GenerateContent",
  "request_ref": "genai.models.generate_content.request",
  "response_ref": "genai.models.generate_content.response",
  "sdk_resource": "models",
  "sdk_method": "generateContent"
}
```

`mcp-plugin` binding object:

```jsonc
{
  "binding_schema_version": 1,
  "adapter_key": "plugin.shape1-mcp",
  "operation_key": "flights_search",
  "request_ref": "flights.search.request",
  "response_ref": "flights.search.response",
  "plugin_name": "google-flights",
  "tool_name": "flights_search"
}
```

`grpc-plugin` binding object:

```jsonc
{
  "binding_schema_version": 1,
  "adapter_key": "plugin.shape2-grpc",
  "operation_key": "gum.plugins.flights.v1.Flights.Search",
  "request_ref": "flights.search.request",
  "response_ref": "flights.search.response",
  "plugin_name": "google-flights",
  "rpc_service": "gum.plugins.flights.v1.Flights",
  "rpc_method": "Search"
}
```

`gum plugin install` materializes exactly one of these binding objects under each resolved plugin variant's `binding` field in `plugin-catalog.json`. For Shape 1 MCP plugins, `tool_name` is the live MCP tool name exposed by the subprocess and `operation_key` equals `tool_name`. For Shape 2 gRPC plugins, `operation_key` equals `<rpc_service>.<rpc_method>`. Missing or malformed selector fields fail build/install with `PLUGIN_BINDING_INVALID`, except that the third-party Shape 2 install gate runs earlier before v0.4.0 and returns `PLUGIN_SHAPE_UNSUPPORTED` for third-party `grpc-plugin` manifests regardless of selector completeness.

Expansion rule: adding a new `grpc-sdk` or `sdk-native` variant for an existing `adapter_key` and existing capability classes is catalog-only. Adding a new `adapter_key`, changing a binding schema version, or adding a new backend binding kind is not catalog-only; it requires adapter implementation, generator validation, documentation, and a fixture-backed contract test in the same PR.

### Binding-version migration

When a change to an existing backend binding kind's field semantics is incompatible (i.e., old binaries cannot ignore the change without altering dispatch behavior), the curator MUST:

1. Increment `binding_schema_version` for all variants using that backend kind in `gen/catalog.json` and any plugin manifests in the same PR.
2. Update the adapter implementation in `internal/adapters/<kind>/` to handle the new version. The adapter MUST continue to handle the previous version for at least one release cycle (binary compatibility window), returning `BINDING_SCHEMA_UNSUPPORTED` for any version it cannot process. For the purposes of this window, a release cycle is the span between two consecutive minor version increments of the host binary (e.g., v0.1.0 → v0.2.0); third-party plugin authors can therefore safely target the host's current and previous minor versions.
3. Update `docs/catalog-abi.md` (this file) with a changelog entry in the affected backend kind's row (e.g., "v2: added `routing_timeout_ms` field").
4. The "who decides" rule: additive fields that old binaries safely ignore do NOT require a version bump; the curator MAY add them at any version. Semantic changes to existing fields (renamed, re-typed, or changed semantics) ALWAYS require a bump. When in doubt, bump — the compatibility window is cheap and the downgrade path (unknown version → `BINDING_SCHEMA_UNSUPPORTED`) is fail-closed.

5. **Patch-version prohibition (normative).** `binding_schema_version` is an **integer**, not a semver triple. Patch-level changes (the third semver component) are not representable in this field and are forbidden as a migration vehicle: a curator MUST NOT attempt to encode a backward-compatible additive-field change as a "v1.0.1 patch bump" by inserting a decimal point or a string suffix. Either the change is additive and ignorable by old binaries (rule 4 above; no version bump), or it is semantic and requires a major-version bump to `binding_schema_version + 1` (this section). The build rejects non-integer `binding_schema_version` values with `BINDING_SCHEMA_UNSUPPORTED`.

Existing `TestBackendBinding<Name>` rows in `docs/test-matrix.md` MUST be updated to cover both the old and new binding schema version in the same PR (spec.md §5.4.1 step 4 references this procedure).

## Catalog capability atoms (normative reservations)

Some catalog fields are reserved as *capability atoms* — small, well-named slots that exist today only as metadata so future runtime features can light up without a wire-shape change. They are deliberately conservative; adding one requires a spec patch.

| Atom | Field path | Type | v0.1.0 runtime | Future runtime |
|---|---|---|---|---|
| `x-sovereign-endpoint` | `variant.binding.x-sovereign-endpoint` (optional, string or null) | string | Inert. Generators MAY populate it from discovery doc `rootUrl` overrides for known sovereign hosts (`googleapis.us`, `googleapis.de`, etc.); runtime IGNORES the value and always uses the default `googleapis.com` request URL. | v0.4.0 universe-domain support consumes this atom together with the `service_root_template` field (§Service Root Extension Point) to dispatch sovereign-cloud variants without a catalog regeneration. |
| `stub_expires` | `variant.stub_expires` (optional, RFC 3339 timestamp string) | string | Inert. Curators MAY set this on schema-only experimental variants to signal a stub-expiry deadline; the daily catalog regeneration CI emits a warning when a stub has expired but does not fail the build. | v0.2.0+ catalog-build pipeline MAY graduate this to a hard build failure once expired-stub backfill has a documented owner. |

Both atoms are reserved-but-inert in v0.1.0: their **schema slots** are part of the Catalog ABI (loaders MUST accept them without error and MUST NOT use them); their **semantics** activate in the version listed in the "Future runtime" column. Adding a third capability atom requires a spec.md patch plus a row here.

## Schema Refs

Embedded first-party schemas and bundled-plugin request/response schemas live under `gen/schemas/`. Runtime-installed third-party plugin request/response schemas live under the active profile's copied `plugin-schemas/` store by SHA-256. Plugin manifests declare a bundle-level `schema_ref`; build/install derives `request_ref = "<schema_ref>.request"` and `response_ref = "<schema_ref>.response"` by extracting `$defs.request` and `$defs.response` as specified in `spec.md` §8.2. `gum.describe_op`, `gum://op/{id}`, and `gum://variant/{id}` expose served request/response refs only. Full JSON Schema bodies are served exclusively through `gum://schema/{ref}`, which resolves embedded refs first, then active profile-local plugin schemas, and never asks a live plugin subprocess for schema. The selected profile's full inventory MUST contain no divergent schema-ref collisions: reuse of a ref is allowed only when JCS-canonical schema-body digests match across embedded, active, pending-restart, needs-configuration, and quarantined plugin schemas; otherwise build/install fails with `SCHEMA_REF_COLLISION`. Inactive plugin refs are inventory metadata only and MUST resolve as `RESOURCE_NOT_FOUND` through `gum://schema/{ref}` until activation.

## Resolved Catalog

The persisted resolved catalog for a profile is:

1. embedded `gen/catalog.json`
2. selected profile's `plugin-catalog.json`

Profile plugin overlays take precedence by `variant_id`. Expression-profile overrides are resolved separately by `docs/expression-profile-dsl.md` and are not part of the catalog ABI. Cross-profile plugin variants, credentials, cache, audit logs, and rate limiter state never merge.

The persisted plugin registry ABI is the three-file generation set defined in `spec.md` §8.7:

- `plugin-catalog.json`: resolved variant records and copied request/response schema hashes.
- `plugins.lock`: package source/ref/checksum plus normalized executable binding.
- `plugin-state.json`: installed/activated/configuration/quarantine lifecycle state.

All three files carry the same `install_generation` and `install_txid`; no single file is authoritative if generations disagree. Startup recovery selects the newest complete shared generation. Startup activation writes are persisted `plugin-state.json` transactions under `plugins.install.lock`, not in-memory derivations.

Runtime uses two views of this data:

- **Inventory registry**: live `plugin-catalog.json` plus `plugin-state.json`. Metadata resources such as `gum://plugins` and `gum://plugin/{name}` read this live view so installs are visible immediately. `gum://op/{id}` and `gum://variant/{id}` are normally active-snapshot resources, but they may consult inventory only to return status-only inactive-plugin responses (`installed_pending_restart` or `needs_configuration`) or quarantine resource errors (`VARIANT_QUARANTINED`) defined in `spec.md`; they MUST NOT expose full inactive or quarantined variant schemas before activation.
- **Active session catalog snapshot**: embedded `gen/catalog.json` plus plugin variants whose registry entry has `activated_at` set and not later than the current MCP server's `session_started_at`, and whose state is neither `quarantined` nor `needs_configuration`. Search, describe, completions for operations/variants, invoke, code mode, and normal full `gum://op/{id}` / `gum://variant/{id}` dispatch metadata use this snapshot for the lifetime of the MCP server. A standalone CLI process first marks install-valid, non-quarantined, configured plugins as activated with its `process_started_at` timestamp, then takes the same one-shot snapshot for that command.

`gum plugin install` writes `installed_at` and leaves `activated_at` null. On MCP server startup, the host marks install-valid, non-quarantined, configured plugins as activated by setting `activated_at` to the server's `session_started_at`. On standalone CLI startup, the host performs the same activation step with the CLI process timestamp before command resolution, so new CLI invocations see previously installed configured plugins without a persistent server restart. This timestamp contract is the deterministic source for `active` versus `installed_pending_restart`; implementations MUST NOT infer activation from file modification time.

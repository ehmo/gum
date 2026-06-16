# Plugin Contract

This document is normative for GUM plugin authoring and install-time validation. It is the plugin-authoring entry point; `docs/catalog-abi.md` owns the runtime catalog state model, and `spec.md` §13 owns MCP resource wire behavior.

## Companion Contracts

Plugin authors must also follow these supporting contracts:

- `spec.md` §8 for manifest field semantics, host services by shape, canary grammar and re-ingestion, crash/quarantine behavior, atomic registry updates, and install/restart semantics.
- `spec.md` §13 for MCP inventory resource-template wire behavior.
- `docs/expression-profile-dsl.md` and `docs/expression-profile-dsl.json` for output-profile syntax, validation, tests, and MCP-root-based project-local lookup.
- `docs/catalog-abi.md` for stable IDs, `backend_kind`, capability atoms, variant lifecycle, and `null_elision_safe_fields`.
- `docs/test-matrix.md` for required proof artifacts.

The author-facing walkthrough — manifest field-by-field, wire ABI with worked JSON-RPC exchanges, packaging layouts, install workflow, and an end-to-end `hello` plugin — is `docs/plugin-author-guide.md`. Read this contract for the *what is required*; read the author guide for *how to ship one*.

## Plugin Shapes

| Shape | Status | Expansion role |
|---|---|---|
| Shape 1: MCP subprocess | v0.1 compatibility bridge | Accepts existing FastMCP/Python plugins. The plugin owns HTTP/TLS/cookies/retry/rate-limit internals and is fully trusted as user-level code. Not policy-complete. |
| Shape 2: gRPC subprocess | v0.4 target model | Host provides HTTP client, cookie jar, retry, rate limiter, credential access, cache, logging, and optional headless browser. This is the future preferred unofficial expansion substrate once the public SDK/proto is frozen. |

For v0.1.0 through v0.3.x, Shape 1 is the only supported external authoring path. Shape 1 plugins are allowed only when their manifest declares the infrastructure they own, ships schemas/output profiles/canaries, and accepts the trust warning. Shape 2 is the long-term unofficial expansion substrate, but it is not an authoring contract until v0.4.0 freezes the public plugin SDK and proto.

Before v0.4.0, `gum plugin install` rejects third-party Shape 2 manifests with `PLUGIN_SHAPE_UNSUPPORTED`. This includes `[plugin].shape = "grpc-subprocess"` and any `[[tools]]` record with `backend_kind = "grpc-plugin"`. The Shape 2 gate runs immediately after structural manifest parsing has identified `shape` and `backend_kind`, wins over `PLUGIN_BINDING_INVALID`, and happens before schema copy, executable staging, canary execution, or registry writes. Shape 2 binding examples in the docs are ABI fixtures only, not an external authoring surface for v0.1.0-v0.3.x.

### Shape 2 Notes (normative for future Shape 2 authors, v0.4.0+)

Shape 2 plugins are Go subprocesses that communicate with the host via GUM's plugin gRPC interface. Until v0.4.0 publishes a public importable SDK/proto package, plugin authors MUST NOT depend on GUM `internal/...` packages or treat any local development path as stable. A canonical `go.work` stub and public module path will be published when the Shape 2 interface is frozen.

## Manifest ABI

Every plugin manifest MUST declare:

- top-level `manifest_schema_version` (sibling of `[plugin]`, not inside it)
- `[plugin]` name, version, description, `namespace_owner`, shape, command, license, ToS status, and risk. `command` is an install-time selector; non-dev runtime execution always uses the normalized executable binding recorded in the selected profile's `plugins.lock`.
- `[package]` source, ref, checksum
- `[requirements]` rate policy, cache TTL, canary, credential/env needs (see **needs_user_creds denylist** and **credential descriptors** below)
- one or more `[[tools]]` records with `op_id`, `variant_id`,
  `backend_kind`, `interface_kind`, `risk_class`, `capabilities`,
  `scopes`, `schema_ref`, `output_profile`, optional
  `confirmation_policy`, and the binding-kind
  selector fields required by `docs/catalog-abi.md` (`tool_name` for
  `mcp-plugin`; `rpc_service` and `rpc_method` for bundled `grpc-plugin`
  ABI fixtures and future Shape 2 manifests). The
  manifest supplies selector inputs; build/install materializes the
  resolved variant's nested `binding` object.
- `null_elision_safe_fields` when the referenced `output_profile` uses `strip_nulls=true`

Unsupported `manifest_schema_version`, missing `manifest_schema_version` on a third-party manifest, or `manifest_schema_version` placed inside `[plugin]` fails before subprocess start with `PLUGIN_MANIFEST_SCHEMA_UNSUPPORTED`.

Missing or malformed plugin binding selector fields fail before
subprocess start with `PLUGIN_BINDING_INVALID`. For Shape 1 MCP
plugins, `tool_name` is required. For bundled ABI fixtures and future
v0.4.0+ Shape 2 manifests, `backend_kind = "grpc-plugin"` requires
`rpc_service` and `rpc_method`. Third-party v0.1.0-v0.3.x manifests
that declare Shape 2 are rejected earlier with `PLUGIN_SHAPE_UNSUPPORTED`,
regardless of selector completeness.

`confirmation_policy` is optional and defaults to `none`. The only non-default
v0.1 value is `high_stakes_write`, valid only for `risk_class = "write"` tools.
It makes `gum.write` and `gum call --risk=write` require user confirmation
before dispatch while preserving MCP `destructiveHint=false`.

**`needs_user_creds` denylist (normative).** The `[requirements].needs_user_creds` field lists environment variable names that the host MUST pass through to the plugin subprocess from the user's environment. To prevent plugin authors from siphoning GUM's own configuration, credentials, or operational state into a plugin's address space:

1. Variable names matching the case-sensitive prefix `GUM_` are PROHIBITED in `needs_user_creds`. Listing one fails build/install with `PLUGIN_ENV_PROHIBITED: needs_user_creds entry '<name>' on plugin '<plugin>' is a prohibited env var name.` (single canonical message form, shared with spec.md §8.1; applies to both the `GUM_` prefix rule and the exact-name denylist).
2. The denylist is enforced from a single curated in-binary source of truth shared by `cmd/gen-catalog`, `gum plugin install`, and runtime env scrubbing. It may be embedded via `go:embed` or compiled as a constant slice, but behavior must be identical in all three paths. The list contains, at minimum: the `GUM_` prefix rule, exact names `GOOGLE_APPLICATION_CREDENTIALS` (use catalog-managed ADC instead), `OPENAI_API_KEY`, `ANTHROPIC_API_KEY`, and any env var beginning with `_GUM`. Future additions require a normative spec patch.
3. The validation runs at both build time (catalog-bundled plugins) and install time (runtime `gum plugin install`). Bypassing it via runtime env injection by the host is PROHIBITED; the dispatch layer MUST scrub the plugin subprocess environment of any denylisted variable regardless of manifest declarations.
4. `TestPluginEnvProhibited` (in `internal/plugin/manifest_test.go`) MUST verify rejection on a fixture manifest that lists `GUM_PROFILE` in `needs_user_creds`, asserting `PLUGIN_ENV_PROHIBITED` is returned and the subprocess is never started.

**Credential descriptors (normative).** `needs_user_creds` is a raw env allowlist for process launch; it is not safe UX copy. Any manifest with non-empty `needs_user_creds` MUST also declare `[requirements].credential_descriptors`, one descriptor per env var, with fields `alias`, `env`, `kind`, `display_name`, and `setup_hint` as specified in `spec.md` §8.2. Inventory resources, `AUTH_REQUIRED` messages, and setup prompts use aliases/display names/hints only. Missing, duplicate, or extra descriptor entries fail build/install with `PLUGIN_CREDENTIAL_DESCRIPTOR_INVALID`.

Plugins that require product setup beyond a secret value (for example a Google
Ads developer token, customer ID, manager login customer ID, billing-enabled
account, or user-owned OAuth client) MUST also declare auth prerequisites using
the `auth_strategy` / `auth_components[]` taxonomy from `spec.md` §7. Secret
components are collected by `gum plugin setup <name>` and stored in the OS
keychain. External components are displayed as checklist items that GUM cannot
complete. A plugin like Google Keyword Planner is therefore `compound`, not
ordinary OAuth: setup must collect/store the token-like fields and explicitly
tell the user which Ads account/billing/access-level prerequisites remain
outside GUM. When a `compound` plugin needs the user's Google access token, it
uses the reserved `google_access_token` component and receives only the
short-lived access token documented in `spec.md` §7; the host never forwards
refresh tokens, service-account material, ADC files, or unrelated GUM
credentials. `plugin_managed` means the plugin owns its upstream auth stack and
does not receive GUM's official Google OAuth flow. Setup canaries for compound
plugins MUST validate that the selected credential subject can actually access
the declared account identifiers; storing syntactically valid secrets is not
enough to clear `needs_configuration`.

If an output profile strips null or empty values, the manifest's tool record must declare the exact dot paths where that elision is safe, for example `null_elision_safe_fields = ["price.currency", "segments[].aircraft"]`. Use `"*"` only for curator-reviewed whole-response elision. Missing or insufficient declarations fail install/build validation with `PROFILE_STRIP_NULLS_UNSAFE`.

## Schema Refs

`schema_ref` resolves to `schemas/<schema_ref>.json` inside the plugin
artifact or bundled plugin directory. The document is a JSON Schema
2020-12 bundle and MUST contain object-valued `$defs.request` and
`$defs.response`. Build/install derives the resolved binding refs as
`request_ref = "<schema_ref>.request"` and
`response_ref = "<schema_ref>.response"`; plugin manifests do not
declare these refs directly in v0.1.0. The ref strings MUST match the
safe served-ref grammar in `spec.md` §8.2 before any path is constructed;
path separators, traversal markers, URI-encoded separators, and control
characters fail with `PLUGIN_SCHEMA_REF_INVALID`. Bundled plugin
request/response schemas are copied into `gen/schemas/` at build time.
Runtime-installed third-party request/response schemas are copied into
the active profile's plugin schema store as
`plugin-schemas/<request_ref>.<sha256>.json` and
`plugin-schemas/<response_ref>.<sha256>.json`; the corresponding
`schema_hashes` are recorded in `plugin-catalog.json`.

Missing or invalid refs, or bundles missing `$defs.request` or
`$defs.response`, fail with `PLUGIN_SCHEMA_REF_INVALID`. A plugin
whose schema ref collides with any schema ref already present in the selected
profile's full inventory (embedded catalog plus active, pending-restart,
needs-configuration, and quarantined plugin schemas) with a different
JCS-canonical schema digest fails install with `SCHEMA_REF_COLLISION`;
identical-body reuse is allowed.

## Runtime Registry

Installed plugin variants are recorded in `~/.local/share/gum/<profile>/plugin-catalog.json`, a versioned JSON object:

```json
{
  "plugin_catalog_schema_version": 1,
  "updated_at": "2026-05-19T00:00:00Z",
  "variants": []
}
```

Updates use the full-state install transaction in `spec.md`: `plugin-catalog.json`, `plugins.lock`, and `plugin-state.json` are staged and published together under one profile-scoped generation. Runtime visibility, activation timestamps, inventory-vs-active snapshot reads, and quarantine precedence are owned by the runtime catalog state model in `docs/catalog-abi.md`; this document does not restate that state machine.

## Deterministic Discovery

MCP clients enumerate plugins via:

- `gum://plugins`
- `gum://plugin/{name}`

CLI users use `gum plugin list|info`. Search may surface plugin operations, but search phrasing is not the inventory contract.

Plugin inventory status is a closed enum: `active`, `installed_pending_restart`, `needs_configuration`, or `quarantined`. In v0.1.0, a plugin installed while an MCP server is already running is inventory-only in that session with status `installed_pending_restart`; its operations are not searchable, operation-completable, describable as active, usable from code mode, or invokable until the MCP server restarts and marks it activated. A credentialed plugin installed without required credentials is `needs_configuration`: install validation succeeded, live canary was skipped, and the user must provide the declared credentials and run `gum canary --plugin=<name> --live` before activation. The restart affects operation reachability through the existing Tier A/meta-tool surface only; plugin variants never add individual MCP tools in v0.1.0. Standalone CLI commands see install-valid configured plugins on their next process start. If a plugin is both quarantined and another inactive state, `quarantined` wins in every MCP and CLI surface.

`gum://plugin/{name}` metadata is assembled from the selected profile's `plugin-catalog.json` plus `plugin-state.json`; the same profile's `plugins.lock` is consulted for package source/ref/checksum fields and the runtime executable binding (`executable_path`, `executable_sha256`, `argv_normalized`, `install_root`). Lock lookups are keyed by `(profile, plugin_name)` and MUST NOT cross profile boundaries. If those sources disagree, runtime status from `plugin-state.json` wins for quarantine/retry state, variant records from `plugin-catalog.json` win for dispatch metadata, and lockfile package fields are surfaced with `metadata_warning: "lock_catalog_mismatch"`. The resource shape is fixed in `spec.md` §13 and includes safe credential descriptors for `needs_configuration`; raw env var names must never appear in this resource. Users configure missing plugin credentials through `gum plugin setup <name>`, which prompts using descriptor display names/setup hints, stores secrets in the OS keychain for the active profile, and runs a live canary before clearing `needs_configuration`.

For non-dev profiles, the launched executable must be inside the host-managed install root derived from the verified artifact. Runtime PATH-only lookup, shell wrappers, and fresh `uvx`/package-manager resolution are prohibited. GUM re-verifies `executable_sha256` before each spawn; mismatch quarantines the plugin and refuses execution. Source-specific normalization is fixed: PyPI commands resolve to a console script inside the profile-scoped virtualenv; GitHub release and Git sources must declare one executable artifact path inside the unpacked install root; local paths are dev-only.

## Reserved Namespaces

Plugins may not claim Google-owned prefixes (`gmail`, `drive`, `calendar`, etc.) unless they are bundled first-party plugins reviewed with the host. The check runs at build time and install time. A conflict fails with `PLUGIN_NAMESPACE_CONFLICT`.

Third-party plugins must also declare `namespace_owner` in the manifest. The
owner is a reverse-DNS or package-registry publisher identity displayed at
install time and recorded in the selected profile's `plugins.lock`. A non-dev profile rejects any plugin
whose op_id prefix is already owned by a different `namespace_owner`; local
development may bypass only with `--dev-allow-namespace-conflict`.

## Canaries

Every plugin declares a canary. Relative date specifiers are preferred. Live canary failures soft-quarantine the plugin rather than blocking the registry write, but failed-canary plugins are not searchable, invokable, or auto-started until a later canary pass or explicit trusted unquarantine. Missing required user credentials skip the live canary and record `needs_configuration` instead of quarantine; after credentials are present, `gum canary --plugin=<name> --live` is the only path that clears that state.

Use `gum canary --plugin=<name> --live --canary-args='key=value ...'` to rerun or override canary parameters.

## Output Profiles and Tests

Each plugin tool must declare `output_profile` unless the variant explicitly declares `raw_result_allowed=true` with a token-budget exception. Plugin profile files are validated with `gum profile validate`; fixture-backed profiles are tested with `gum profile test`. Lossy plugin profiles must keep `recovery` enabled and must declare `null_elision_safe_fields` when `strip_nulls=true`.

## Trust Posture

Shape 1 install is equivalent to running arbitrary user-level code. GUM verifies package and executable checksums, displays ToS/risk, narrows env vars, validates schemas/output, and enforces declared `network=false` plus `fs_write_dir` with the OS sandbox backend on supported macOS/Linux hosts. That confinement is a defense-in-depth boundary, not a multi-tenant sandbox or a substitute for trusting the installed package. Unsupported sandbox platforms fail closed for enforced plugin execution. Shape 1 plugins do not implicitly inherit GUM's official Google OAuth credential; the only host-mediated Google-token path is the explicit `compound` `google_access_token` forwarding rule in `spec.md` §7, and that rule forwards a short-lived access token only. `plugin_managed` plugins own their own auth. Shape 2 narrows ambient authority through host services, but still is not a multi-tenant sandbox.

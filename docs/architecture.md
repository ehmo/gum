---
title: gum architecture overview
audience: new contributors
status: living document
---

# gum architecture overview

gum is a single Go binary that exposes the same dispatch kernel via two
presentation layers: a Cobra CLI and an MCP stdio server. Everything else —
catalog, auth, cache, output shaping — sits behind that kernel.

This doc is the 1-page mental model. The normative contracts it summarizes are
[`catalog-abi.md`](catalog-abi.md),
[`expression-profile-dsl.md`](expression-profile-dsl.md),
[`plugin-contract.md`](plugin-contract.md), and
[`test-matrix.md`](test-matrix.md).

## Component map

```text
                          +-----------------------------+
   stdin/stdout MCP -->   |        internal/mcp         |
                          |  (handlers, schemas, tools) |
                          +--------------+--------------+
                                         |
                          +--------------+--------------+
        cobra args -->    |        internal/cli         |
                          |   (root, subcommands, IO)   |
                          +--------------+--------------+
                                         |
                                         v
                          +-----------------------------+
                          |     internal/dispatch       |
                          |  9-step invocation kernel   |  <-- ONLY entrypoint
                          +--------+--------+-----------+
                                   |        |
                +------------------+        +------------------+
                v                                              v
   +--------------------------+                  +---------------------------+
   |    internal/catalog      |                  |     internal/adapters     |
   |  (Op/Variant schema,     |                  |  rest.typed-rest-sdk,     |
   |   alias resolution,      |                  |  rest.raw-http, code.risor|
   |   risk-class metadata)   |                  |  plugin.mcp, googleads.*  |
   +--------------------------+                  +---------------------------+
                |                                              ^
                |                                              |
                v                                              |
   +--------------------------+                  +---------------------------+
   | internal/embedded, embed |                  |       internal/auth       |
   |  (catalog.json, roster,  |                  |  BYO OAuth + ADC +        |
   |   managed-scopes, bm25)  |                  |  keychain + composite     |
   +--------------------------+                  +---------------------------+
                                                              |
                          +--------------+--------------+     |
                          |    internal/cache (ETag,    |<----+
                          |    SQLite, semantic)        |
                          +-----------------------------+
                                         |
                          +--------------+--------------+
                          |   internal/output           |
                          |  (toon, profile DSL, jcs,   |
                          |   fieldmask, gain ledger)   |
                          +-----------------------------+
                                         |
                          +--------------+--------------+
                          |   internal/sandbox/risor    |
                          |   (gum.code script host)    |
                          +-----------------------------+
```

**Layering rule (spec §14, enforced by `TestNoCyclicImports`)**:
`internal/dispatch` is the leaf of the internal import graph. Nothing it
imports imports it back. Its direct imports are `internal/cache`,
`internal/catalog`, `internal/fsatomic`, `internal/output/gain`,
`internal/output/jcs`, `internal/output/profile`, and
`internal/output/tee`; it imports nothing
from `internal/auth`, `internal/profile`, or
`internal/pluginenv`, and nothing from `internal/cli`, `internal/mcp`, or
`internal/adapters`. Behaviour it does not own arrives through constructor
injection on `DispatcherConfig`, which is why `internal/auth` may import
kernel types to implement `dispatch.AuthResolver` without creating a cycle.
Only `internal/cli`, `internal/mcp`, `cmd/`, and the two code-mode host
files under `internal/adapters` may call the dispatcher entrypoint.

## The 9-step dispatch lifecycle

Every invocation — CLI or MCP — flows through these steps in order. Every
step is a method on `*dispatcher` under `internal/dispatch/`; all but the
policy kernel live in `lifecycle.go`, and `dispatchSteps` is the driver.

| # | Step | File / function | What happens |
|---|---|---|---|
| 1 | parse + validate | `lifecycle.go: parseAndValidate` | resolve op alias via `Op.DeprecatedOpIDs`; check required/unknown/type-error args; build canonical `args_hash` (SHA-256 over JCS-canonical args) |
| 2 | policy kernel | `policy.go: evaluatePolicy` | enforce profile `AllowOps` / `DenyOps`; risk-class hierarchy gate (read < write < destructive); check `confirmed` for destructive ops; check `AllowedScopes` against the variant's required scopes |
| 3 | variant routing | `lifecycle.go: resolveVariant` | pick variant by ABI-stable resolution (explicit `variant_id` pin → declared `default_variant_id` → drop quarantined → highest stability group → `preferredInterfaceKinds` tie-break → ambiguity error); apply VariantQuarantined / VariantDeprecated annotations. Sub-steps 3a-pre to 3c run the §5.8 capability gate, resolve the catalog-embedded profile, apply the `dual_fetch` eligibility gate, and compute the upstream field-mask projection |
| 4 | auth resolve | `lifecycle.go: resolveAuth` | composite resolver picks by the variant's `auth_strategy` (`byo_oauth`, `gum_oauth`, `adc`, `api_key`, `service_account_key`, `compound`, `plugin_managed`, `none`); produces the `auth_subject_fingerprint` that step 5 keys on |
| 5 | cache check | `lifecycle.go: cacheCheck` | §10.3 semantic lookup on `(op_id, variant_id, args_canonical, fields, auth_subject_fingerprint)`; a hit still runs step 8. On a miss, step 5b consults the §10.2 HTTP/ETag cache and sends `If-None-Match` |
| 6 | rate limit | `lifecycle.go: tokenBucketStep` | per-(profile, project) token bucket, stored by `internal/auth/persistent_bucket.go`; runs only on a cache miss, so hits do not spend quota |
| 7 | execute | `lifecycle.go: executeAdapter` | adapter dispatch (`rest.typed-rest-sdk`, `rest.raw-http`, `code.risor`, `plugin.mcp`, `googleads.*`); panic recovery via `audit.go: recoverAdapterPanic`. Sub-steps 7a1 to 7c handle the 304 revalidation branch, the §9.1 unmasked second fetch, the cache store, the ETag store, and the tee artifact write |
| 8 | shape | `lifecycle.go: shapeResponse` | apply expression-profile pipeline; field-mask projection; TOON encoding; recovery link emission |
| 9 | record + return | `lifecycle.go: recordAndReturn` | structured-error sanitization; gain-ledger append; audit-log emit; emit final envelope to caller |

Auth runs before the cache on purpose. Spec §3.1 numbers the cache lookup 4
and auth 5, but the §10.3 cache key includes the auth-subject fingerprint,
which is unknown until auth resolves. With the spec order the lookup keyed on
an empty fingerprint while the step-7b store keyed on the resolved one, so an
authenticated read never hit. Resolving auth first also makes a cache hit
require proving you are the principal whose response is cached. The function
doc comments in `lifecycle.go` still carry the spec numbering; `dispatchSteps`
is the execution order.

Steps that may short-circuit:

- **Step 1**: alias not found → `OP_NOT_FOUND`; invalid args → `INVALID_ARGS`
- **Step 2**: policy denial → `POLICY_DENIED`, `REQUIRES_CONFIRMATION`, `SCOPE_MISSING`, `RISK_TOOL_MISMATCH`
- **Step 3**: ambiguity → `AMBIGUOUS_VARIANT`; quarantined → `VARIANT_QUARANTINED`; unsupported capability class → `UNSUPPORTED_CAPABILITY`
- **Step 4**: missing credentials → `AUTH_REQUIRED`; fingerprint not the one the profile recorded → `AUTH_SUBJECT_MISMATCH`
- **Step 5**: semantic hit returns the stored body through step 8; ETag 304 returns cached; a miss continues
- **Step 6**: rate exhausted after retry → `RATE_LIMITED`
- **Step 7**: adapter panic → recovered into `SERVICE_DOWN` with a sanitized stack in the log and an audit entry; oversize body → `RESPONSE_TOO_LARGE`
- **Step 8**: profile error → returns the raw upstream payload with a warning envelope (lossy projection only)

## Where to add things

| Adding... | Goes in |
|---|---|
| A new Google API op | `cmd/gen-catalog/overrides.toml` then `go run ./cmd/gen-catalog` |
| A new adapter (e.g. gRPC for a service) | `internal/adapters/` (own subpackage for a heavy client) + register the `adapter_key` in `defaultAdapters()` in `cmd/gum/root.go` |
| A new policy gate | `internal/dispatch/policy.go`, or `lifecycle.go` between existing steps |
| A new output stage | `internal/output/profile/` with a §9 DSL stage entry |
| A new convenience tool | `internal/embedded/data/tier-a-roster.v1.json` + `internal/mcp/schemas.go` |
| A new release-gate test | `internal/securityscan/` + `docs/test-matrix.md` row |
| A new auth strategy | `internal/auth/` + register in `NewDefaultCompositeResolver` |

## Cross-cutting invariants

- **Single binary, no CGo** — `TestReleaseBinaryNoCGo` in `internal/securityscan`
  + `build-matrix` workflow enforce this on every PR.
- **No silent stdout before initialized** — MCP stdio framing test
  `TestStdioFramingClean` (spec §13.1).
- **Single-profile per process** — the confirmation token binds `profile_name`,
  so a token minted under one profile is refused under another
  (spec §6.1.2 Profile binding).
- **Stable error code set** — `internal/dispatch/errors.go` defines 32
  codes; spec §7 enumerates 30 of them. `POLICY_DENIED` and
  `RESPONSE_TOO_LARGE` ship and are reachable but are absent from the spec
  list. New codes require a spec amendment.

## Further reading

- [`docs/test-matrix.md`](test-matrix.md) — every release-gate test and its purpose
- [`docs/catalog-abi.md`](catalog-abi.md) — catalog binary ABI for plugins
- [`docs/expression-profile-dsl.md`](expression-profile-dsl.md) — §9 output DSL
- [`docs/plugin-contract.md`](plugin-contract.md) — §8 plugin host contract
- [`CONTRIBUTING.md`](https://github.com/ehmo/gum/blob/main/CONTRIBUTING.md) — conventional commits + release process

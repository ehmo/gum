---
title: gum architecture overview
audience: new contributors
status: living document (v0.1.0)
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
   |   alias resolution,      |                  |  code.risor,              |
   |   risk-class metadata)   |                  |  long-tail HTTP, gRPC ...|
   +--------------------------+                  +---------------------------+
                |                                              ^
                |                                              |
                v                                              |
   +--------------------------+                  +---------------------------+
   |    internal/embedded     |                  |       internal/auth       |
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
`internal/dispatch` is the leaf of the internal import graph. Everything
else may import it; it imports nothing from `internal/auth`, `cache`,
`profiles`, `output`, `usage`, or `pluginenv` — those are passed in via
constructor injection on `DispatcherConfig`.

## The 9-step dispatch lifecycle

Every invocation — CLI or MCP — flows through these steps in order. Each
step has a single owner file under `internal/dispatch/`.

| # | Step | File / function | What happens |
|---|---|---|---|
| 1 | parse + validate | `lifecycle.go: parseAndValidate` | resolve op alias via `Op.DeprecatedOpIDs`; check required/unknown/type-error args; build canonical `args_hash` (SHA-256 over JCS-canonical args) |
| 2 | policy kernel | `lifecycle.go: evaluatePolicy` | enforce profile `AllowOps` / `DenyOps`; risk-class hierarchy gate (read < write < destructive); check `confirmed` for destructive ops; check `AllowedScopes` against the variant's required scopes |
| 3 | variant routing | `lifecycle.go: resolveVariant` | pick variant by ABI-stable resolution (declared default → alias scan → ambiguity error); apply VariantQuarantined / VariantDeprecated annotations |
| 4 | cache lookup | `cache.go: cacheLookup` | compute cache key `(op_id, variant_id, args_canonical, auth_subject_fingerprint)`; if ETag cache hit send `If-None-Match`; on 304 return cached |
| 5 | auth resolve | `auth.go: resolveCredentials` | composite resolver picks BYO OAuth → ADC → workload identity per the variant's `auth_strategy`; fingerprint is hashed into the cache key from step 4 |
| 6 | rate limit | `ratelimit.go: acquireToken` | per-(profile, project) token bucket; structured 429 retry with jitter |
| 7 | execute | `executor.go: executeAdapter` | adapter dispatch (rest.typed-rest-sdk, code.risor, long-tail HTTP, gRPC); panic recovery via `recoverAdapterPanic` |
| 8 | shape | `shape.go: shapeResponse` | apply expression-profile pipeline (8 stages); field-mask projection; TOON encoding; tee artifact write; recovery link emission |
| 9 | return + ledger | `lifecycle.go: emitResult` | structured-error sanitization; cache populate (ETag); gain-ledger append; audit-log emit; emit final envelope to caller |

Steps that may short-circuit:

- **Step 1**: alias not found → `OP_NOT_FOUND`; invalid args → `INVALID_ARGS`
- **Step 2**: policy denial → `POLICY_DENIED`, `REQUIRES_CONFIRMATION`, `SCOPE_MISSING`, `RISK_TOOL_MISMATCH`
- **Step 3**: ambiguity → `AMBIGUOUS_VARIANT`; quarantined → `VARIANT_QUARANTINED`
- **Step 4**: ETag 304 returns cached; cache miss continues
- **Step 5**: missing credentials → `AUTH_REQUIRED`
- **Step 6**: rate exhausted after retry → `RATE_LIMITED`
- **Step 7**: adapter panic → recovered into `INTERNAL_ERROR` with sanitized stack
- **Step 8**: profile error → returns the raw upstream payload with a warning envelope (lossy projection only)

## Where to add things

| Adding... | Goes in |
|---|---|
| A new Google API op | `cmd/gen-catalog/overrides.toml` then `go run ./cmd/gen-catalog` |
| A new adapter (e.g. gRPC for a service) | `internal/adapters/<adapter-name>/` + register in `defaultAdapters()` |
| A new policy gate | `internal/dispatch/lifecycle.go` between existing steps |
| A new output stage | `internal/output/profile/` with a §9 DSL stage entry |
| A new convenience tool | `internal/embedded/data/tier-a-roster.v1.json` + `internal/mcp/schemas.go` |
| A new release-gate test | `internal/securityscan/` + `docs/test-matrix.md` row |
| A new auth strategy | `internal/auth/` + register in `NewDefaultCompositeResolver` |

## Cross-cutting invariants

- **Single binary, no CGo** — `internal/securityscan/TestReleaseBinaryNoCGo`
  + `build-matrix` workflow enforce this on every PR.
- **No silent stdout before initialized** — MCP stdio framing test
  `TestStdioFramingClean` (spec §13.1).
- **Single-profile per process** — confirmation token HMAC key is per-process;
  cross-profile replay impossible (spec §6.1.2 Profile binding).
- **Stable error code set** — 28 codes enumerated in spec §1421; new codes
  require a spec amendment.

## Further reading

- [`docs/test-matrix.md`](test-matrix.md) — every release-gate test and its purpose
- [`docs/catalog-abi.md`](catalog-abi.md) — catalog binary ABI for plugins
- [`docs/expression-profile-dsl.md`](expression-profile-dsl.md) — §9 output DSL
- [`docs/plugin-contract.md`](plugin-contract.md) — §8 plugin host contract
- [`CONTRIBUTING.md`](https://github.com/ehmo/gum/blob/main/CONTRIBUTING.md) — conventional commits + release process

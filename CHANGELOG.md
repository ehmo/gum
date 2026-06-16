# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [1.0.0] - 2026-06-14

v1 release candidate: 222 catalog operations across 32 services, BYO OAuth as
the public Google auth path, Google Ads support, expression-profile output
shaping, and hardened CLI/MCP setup checks.

### Added

- **Google Ads Keyword Planner** — first non-Workspace native service. Three
  read ops (`googleads.keywordPlanIdeas.generateKeywordIdeas` /
  `.generateKeywordHistoricalMetrics` / `.generateKeywordForecastMetrics`) via a
  new `google-ads-sdk` adapter (`internal/adapters/googleads/`) on the Google
  Ads API v24. `byo_oauth` (adwords scope) plus a secret `developer-token`
  header sourced server-side (keychain / `GUM_GOOGLE_ADS_DEVELOPER_TOKEN`, never
  an invocation arg). New `gum auth use-ads-developer-token` command. The
  adapter retries 429/5xx with backoff (honouring `Retry-After`) and fails
  closed on bad input. Verified live against a real account.
- **Expression-profile output pipeline now applies.** The dispatch kernel
  resolves a variant's `output_profile` via an injected catalog-embedded
  lookup (spec §9.2 third layer) and applies it at step 8, for both CLI and MCP.
  Built-in profiles ship in `internal/output/profile/builtin/`; the two Google
  Ads read ops carry compact profiles (~89% output reduction on keyword ideas).
  Backward compatible — ops whose profile name has no definition are unchanged.
- **Catalog breadth (Tier 1 + Tier 2).** People/Contacts, official YouTube Data
  API v3, Forms, Chat, Classroom, Photos, Cloud Identity, Apps Script, Vault,
  Meet, Groups Settings, Indexing, and Admin Reports (audit + usage), plus Admin
  Directory writes.
- **Catalog depth.** Drive, Sheets, Docs, Slides, and Tasks deepened toward
  full core CRUD.
- **API-key services.** Custom Search and Maps (Geocoding / TimeZone /
  DistanceMatrix); Places (New) and Routes with header-routed request fields
  (`X-Goog-FieldMask`).
- `{+param}` reserved path-template expansion for resource-name path params.
- `gum login --service <names>` / `--all` scope selection.

### Changed

- `gum login` defaults to the core Workspace scope set instead of a fragile
  ~60-scope union that broke consent across APIs the OAuth client hadn't enabled.
- `gum login` now requires an operator-registered Google Desktop OAuth client
  for OAuth-backed operations.
- `gum doctor` treats BYO OAuth, API key, service-account, and ADC setups as
  valid credential sources based on local readiness.
- Hardened gum CLI trust boundaries.
- Raised the `internal/auth` coverage ratchet to retain the current green
  baseline.

### Fixed

- `drive.about.get` requires `fields=` — baked `fields=*` into the binding.
- Login scope poisoning: dropped `gmail.metadata` (blocked `format=FULL`) and
  switched Photos to the valid 2025 `photoslibrary.readonly` scope.
- Expression-profile DSL parser handles the full documented field set.
- Dispatch: accept string-form integer/bool query params; persist the
  confirmation signing key so cross-process confirm works; populate
  `StructuredContent` on the semantic-cache hit path; treat an empty (204) body
  as success rather than a parse error.
- Auth: actionable remediation when a BYO Desktop client omits its secret; wire
  the `plugin_managed` strategy so unofficial ops aren't dead.
- Release/CI: goreleaser snapshot builds without OAuth client secrets;
  actionlint-clean workflows.

### Security

- Bump `golang.org/x/crypto` 0.51.0 → 0.52.0 and `google.golang.org/grpc`
  1.66.2 → 1.79.3 (with `google.golang.org/protobuf` 1.34.2 → 1.36.10) to clear
  all govulncheck advisories; `go` directive 1.26.3 → 1.26.4. `govulncheck ./...`
  reports no vulnerabilities.

### Known limitations

- Confirmation-token signing keys are persisted with 0600 permissions and read
  with `O_NOFOLLOW` on Unix to close symlink-substitution attacks. A future
  keychain-backed MAC would add platform keychain dependencies to the dispatch
  kernel, so v1 keeps the filesystem key path.
- Shape 1 plugin `network` and `fs_write_dir` manifest capabilities are now
  enforced on macOS via `sandbox-exec` and on Linux via Landlock plus a network
  namespace; unsupported platforms fail closed at spawn until their OS sandbox
  backends are implemented.
- Public install URLs and release assets must be checked without GitHub auth
  after the repository visibility flips to public.

## [0.1.0] - 2026-05-22

First public preview. v0.1.0 proved the CLI and MCP server on a small
Workspace and Flights slice before the v1 catalog expansion.

### Added

- Single Go binary (`gum`) exposing CLI + MCP stdio server.
- Build-time Google capability catalog (`cmd/gen-catalog`) generating the first
  27-tool roster: 9 meta tools and 18 convenience tools.
- Dispatch lifecycle (`internal/dispatch/lifecycle.go`) with policy gate,
  in-memory cache, auth, token bucket, executor, shape, return.
- Closed-enum auth strategies (`internal/auth/strategy.go`): `byo_oauth` and
  `adc` implemented; remaining 6 return `AUTH_STRATEGY_NOT_IMPLEMENTED` per
  spec §15 deferred lanes.
- Persistent token bucket (`internal/auth/persistent_bucket.go`) backed by
  bbolt with nanosecond-resolution Retry-After freeze.
- HMAC-SHA256 confirmation tokens (`internal/dispatch/confirmation.go`) with
  key auto-generation at `<keyDir>/confirmation.key` mode 0600, rotation via
  `.N`-suffix sibling files, closed-enum purpose check pre-verify.
- BM25-only-v1 retrieval index (`internal/embed/bm25.go`) backing
  `gum.search_apis` (`internal/help/searchapis.go`); no external model API.
- 7-rule build-time description sanitizer (`internal/sanitize/sanitizer.go`):
  marketing language, model hints, second-person, token budgets (≤220 conv /
  ≤360 meta cl100k), risk disclosure, PII patterns.
- bbolt persistent cache (`internal/cache/bbolt.go`) with 256MB cap and 256-entry
  LRU hot tier.
- TOON wire format (`internal/output/toon`) — ~200 LOC, in-tree.
- Expression-profile DSL subset (`internal/output/profile`) for output shaping.
- Gain ledger (`internal/output/gain`) computing cl100k token savings per op.
- Shape 1 mcp-plugin host (`internal/plugins/host.go`) with manifest validation,
  install/remove/list, FS-write checks, macOS `sandbox-exec` enforcement, and
  Linux Landlock/network-namespace enforcement for subprocess
  network/fs_write_dir capabilities.
- Canary scheduler (`internal/auth/canary.go`) that atomically mutates
  `live_canary_state` + `last_checked` in the embedded managed-scope manifest.
- Risor v2 sandbox (`internal/sandbox/risor/sandbox.go`) with caller-injectable
  `gum_call`/`gum_search`/`gum_confirm_destructive`,
  sandbox-owned `gum_http_get` (30s default timeout, HTTPS-only, 1MB body cap),
  and `gum_allow_write`/`gum_allow_destructive` bool globals.
- `gum gain [--by-op] [--fixture-replay [--format=json|toon]]` subcommand.
- `gum plugin install|list|remove|run` subcommands.
- `gum mcp --stdio` server with `tools/list`, `tools/call`, `initialize`.
- `gum --version` reports build-time injected version.
- Goreleaser config (`.goreleaser.yaml`) targeting darwin/linux × amd64/arm64
  with `CGO_ENABLED=0`, `-trimpath`, `-ldflags='-s -w'`.
- Release workflow (`.github/workflows/release.yml`) with SLSA L1 provenance
  via `slsa-github-generator` v2.

[1.0.0]: https://github.com/ehmo/gum/releases/tag/v1.0.0
[0.1.0]: https://github.com/ehmo/gum/releases/tag/v0.1.0

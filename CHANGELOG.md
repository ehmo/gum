# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [2.0.0] - 2026-09-20

This release closes the gap between what the specification promised and what
the binary did. Several fixes reject input that earlier versions accepted, so
the major version changes even though no specification promise was withdrawn.

### Breaking

- The expression-profile key is `format`, not `default_format`, and its enum is
  `toon|csv|json|markdown`. The parser read `default_format` with a
  `toon|json|raw` enum, so every profile printed in the specification and in
  `docs/expression-profile-dsl.md` failed on line 1. A profile still using
  `default_format` now fails with a message naming the new spelling. The
  config key `output.default_format` is unchanged.
- `field_mask_mode="dual_fetch"` is rejected with `INVALID_ARGS` before any
  upstream request. The mode promised a second unmasked fetch to feed the
  recovery artifact; the kernel only ever issued one request, so it billed one
  request, wrote a masked artifact under a promise of pre-mask recovery, and
  stamped the audit log with a fetch that never happened. The enum value still
  parses, so profiles keep validating.
- `gum cache migrate` exits non-zero on `RSYNC_AMBIGUITY`. The envelope said
  `ok:false` while the exit code said success, so `migrate || handle` never
  fired. stdout is unchanged.
- `gum auth use-api-key` and `gum auth use-ads-developer-token` exit non-zero
  when the keychain write fails. They returned 0, so a script that piped in a
  secret was told the secret was stored. A platform with no keychain backend at
  all keeps the environment-variable fallback and still exits 0.
- The nine MCP meta-tools, the two skill helpers, and the 18 convenience tools
  validate arguments against the `inputSchema` they advertise. A closed enum, a
  required property, a declared type, and `additionalProperties:false` were all
  advisory before, and a bad value reached the handler as a zero value. A
  violation now returns a §7 `INVALID_ARGS` envelope.
- The 18 convenience tool schemas move to the §4.1 argument names of their
  backing ops. `gmail_search` takes `q`, not `query`; `flights_search` takes
  `departure_date`, not `departureDate`; the four Gmail rows declare the
  required `userId`; `calendar_upcoming` declares `calendarId` as required.
  `drive.get_file` drops `mimeType` and `drive.share` drops `emailMessage`,
  because neither op declares them. `gmail_get_message` no longer takes a
  `format` key, since `gmail.users.messages.get` declares its own.
- `gum.read`, `gum.write`, and `gum.destructive` ignore `allow_write` and
  `allow_destructive` in their arguments. The per-tier switch is the only writer
  of those flags.
- `gum.code`'s `destructive_scope` items are `{op_id, resource_key}` objects,
  not strings. The executor's scope extractor dropped strings, so a caller
  following the old schema ran destructive code with no scope.
- `gum.poll` registers `RawJsonResult` and returns `isError=true` on
  `LRO_TIMEOUT`. It previously reported a timed-out poll as a success.
- `gum.describe_op`, `gum.gain`, and `gum.cache_stats` register the result
  schemas §2256 to §2258 name, not `SingleObjectResult`. `skills_get` drops its
  output schema.
- TOON keeps the keys of a map whose values are all empty.
  `{"error":"","status":null}` encoded as `{}` with both key names gone and no
  lossy flag. The `{}` sentinel now covers a map with no fields only.
- `truncate_strings` puts the ellipsis inside the limit. A profile asking for
  180 characters received 181.
- `gum_print` encodes a non-string value as JSON. A map printed as
  `map[a:1 b:x]`, which is not parseable in the §13 `data` member that carries
  it. `gum_print(nil)` aborted the script and now prints `null`.
- Response numbers decode through `json.Number`. An integer above 2^53 came back
  with different digits, so a Google Ads customer id lost its low bits and two
  distinct ids could collide into one dedupe key.
- The per-op semantic TTL table is keyed on catalog op ids. Seven of eleven keys
  matched no op, so the whole 24h tier was dead and `gmail.users.getProfile`
  expired every 60 seconds.
- `gum profile test` gains `--name` to select one definition from a
  multi-profile file. It is not spelled `--profile`, which the root command owns.

### Added

- `gum plugin info <name>`, specified at §12 line 2495 and missing from the
  binary. `--format=json` emits the same object the `gum://plugin/{name}` MCP
  resource carries, through the same JCS canonicaliser.
- `--max-items` on `gum read|write|destructive|call` and `max_items` on the
  three MCP risk tools. It replaces the profile's `collapse_arrays` cap for one
  invocation; `all` skips the stage. The override stays off the arguments, so it
  does not change the cache key or the args hash.
- Real `csv` and `markdown` encoders at stage 8. Both names were in the closed
  format enum; every name but `json` fell through to the TOON arm while the
  reported format kept the requested name, so a client that asked for `csv` got
  TOON bytes and a client that asked for `markdown` got a JSON tree in a field
  the specification says holds a string.
- The `_expression` envelope on every §13 result shape. No production path
  emitted one, so an MCP client could not tell that a profile had dropped fields
  or collapsed rows. On the CLI the same information goes to stderr as a
  one-line shaping notice, leaving stdout the payload root.
- `unsupported_capabilities` on `catalog.Variant`, required by §925 on every
  non-`full` `execution_support` branch. `gum.describe_op` reused the variant's
  whole `capabilities[]` list, which §918's own worked example contradicts.
- `"partial"` in both `DescribeOpResult` `execution_support` enums and its
  `oneOf`. A variant legal under §918 produced structured content that failed
  the schema §3175 binds it to.
- `[output_profiles."<name>"]`, `[override_bindings]`, and top-level
  `[[tests]]` in a profile file. The parser accepted bare top-level keys only,
  so every documented example failed to validate and `[override_bindings]` had
  no parser case, no runtime consumer, and no producer for
  `OVERRIDE_BINDING_INVALID`. Bindings now reach dispatch: a `variant_id`
  binding beats an `op_id` binding, both beat the variant's own
  `output_profile`, and a caller-supplied profile beats all three.
- `occurrence_count` on a deduped row, required by §9.1 stage 7. It is written
  only for a group above one row, and never over an upstream field of that name.
- `<field>_truncated` siblings from `truncate_strings`, required by §2.8 and
  §9.1 stage 6.
- `GUM_KEYRING_TIMEOUT` to override the keychain call bound.
- `--destructive-budget` and repeatable `--destructive-scope` on `gum code`.
  `--allow-destructive` could never execute: the adapter rejects a budget
  outside 1..20 and the CLI had no flag to set one, so every run failed
  `INVALID_ARGS` before the first line of script.
- `internal/lint/imports_test.go`, the §14 import-graph gate the specification
  names and the tree did not contain.
- Five profile DSL keys documented that parsed only in Go: `projection`,
  `flatten_singletons`, `sort_by`, `limit`, `omit_zero_counts`. Each parsed and
  then failed schema validation against `additionalProperties:false`.

### Changed

- Stage 1 injects the variant's `default_fields` as the upstream field mask when
  a profile states none, which is what the §9.1 field table has always said. No
  shipped variant declares `default_fields`, so nothing new goes upstream today.
- `results` joins `items`, `data`, and `messages` as a recognised record-array
  key, and the row stages resolve that array through the same helper the counts
  use. Two top-level arrays made the heuristic give up, so a 243-keyword call
  reported `result_count` 0 with 100 rows in the body.
- The shaping notice states the row counts that `dedupe` and `limit` removed,
  not only the dropped field names. Stage 7 recorded nothing, so a caller read a
  shortened result as a complete one.
- `_expression.omitted_count` counts every row shaping drops, not only the ones
  `collapse_arrays` writes a sibling for. `_expression.lossy` reads the cap in
  force, so a caller `max_items` that truncates a lossless profile no longer
  reports `lossy:false` beside `omitted_count:95`.
- CSV encodes top-level scalars as repeated columns and secondary arrays as one
  count column each. It wrote the primary record table alone, so `nextPageToken`
  vanished with nothing recorded in `lossy`, `dropped_paths`, or the notice.
  Markdown no longer truncates cells at 60 columns; that limit is an
  80-column terminal layout and the ASCII table keeps it.
- Every kernel error carries a §7 envelope. An adapter error travelled out
  unwrapped and the MCP seam returned a bare sentence with no `error_code` and
  no `retryable` flag. Anything unstructured becomes `SERVICE_DOWN` with the
  cause kept in the chain.
- A non-`gum_oauth` auth failure carries `auth_strategy`, `missing_components`,
  and `setup_command`, and keeps its own code. `AUTH_KEYCHAIN_UNAVAILABLE`,
  `BYO_OAUTH_CLIENT_NOT_CONFIGURED`, `AUTH_STRATEGY_NOT_IMPLEMENTED`, and
  `GUM_OAUTH_MANAGED_CLIENT_NOT_READY` all arrived as `AUTH_REQUIRED`, and the
  CLI answered every one with a hint naming `gcloud`, which gum does not depend
  on.
- `gum plugin remove` and reinstall run as registry transactions. A torn publish
  rolls back, and a reinstall merges the existing row instead of replacing it.
- The response renderer moves from `cmd/gum` to `internal/output/render`, so the
  CLI and the MCP seam produce the same bytes.
- Catalog variants sort by `variant_id` before JCS hashing, as §8.7 requires.
  Two profiles that installed the same plugins in a different order wrote
  byte-different catalogs with identical content.
- `gen-catalog` validates every variant's `output_profile` against the built-in
  profile set and runs the bound-profile checks. `flights.v1.plugin.search`
  shipped naming a profile body that has never existed.
- The release savings gate is two floors, a registration floor and a response
  floor, instead of one blended 0.80 that sat at 0.80004 because it charged gum
  every output-schema byte against a baseline that declares none.
- `staticcheck` pins to v0.8.1. v0.7.0 cannot read Go 1.26 export data.

### Fixed

- `gum` hung forever at startup on a Linux host whose Secret Service collection
  is locked, which covers headless, SSH, container, and CI. go-keyring parks on
  a D-Bus unlock prompt nobody can answer, and every invocation reached it
  through `GrantedScopes`. Every keychain call is now bounded: 20s interactive,
  2s background.
- `tee_mode="failures"` wrote nothing. The write lived at lifecycle step 7c and
  the step-7 error path returns before it, so the one mode that exists for
  failures was dead. Adapter errors now carry the verbatim non-2xx body into the
  artifact.
- A cache hit skipped the recovery artifact, so a warm response named dropped
  paths with no artifact to recover them from and a `recovery=resource_link`
  profile answered with no `gum://results` link.
- The gain ledger was never written. Nothing set `dispatch.Config.Ledger`
  outside tests, so `gum gain` reported an empty ledger however much traffic a
  profile had served. Four divergences went with it: the default path ignored
  the profile segment and `XDG_DATA_HOME`, the file was created 0644 instead of
  0600, `gain.enabled=false` was not honoured, and `NewLedger` parsed the whole
  file on open.
- TOON `Decode` split on every newline without tracking quoting, so a string
  holding a newline, which is ordinary Gmail and Docs data, was cut in half.
  `{"a": "x\ny=z"}` decoded to `{"a": "\"x", "y": "z\""}`: truncated, with a
  dangling quote and a fabricated field, and a nil error. Verified with 30s of
  `FuzzToonDecode`, 11.2M executions, no crash.
- `dedupe`, `sort_by`, and `limit` required a top-level array, which no real
  Google list response is and which stage 5 rewrites away. `dedupe` also keyed a
  row on all-absent fields, collapsing a result set to one row.
- `k=-1` on `gum.search_apis` panicked the stdio server on a slice bound.
  `gum.read` forwarded a fractional `page_size` upstream for an opaque 400.
- A confirmation token's replay marker was keyed on the caller's hex signature
  string, and `hex.DecodeString` accepts upper-case, so re-spelling the field
  missed both the marker file and the in-memory map and one approval authorized
  unbounded executions. The binding hash also omitted `caller` and `risk_class`,
  so a token approved on one surface replayed on the other.
- `canonicalizeArgs` serializes through JCS and drops null-valued keys at every
  depth, per §10.0 rule 1, so an argument spelled as null and an omitted
  argument share one cache key. `semanticFields` keeps projection and keep-field
  lists in separate labelled groups; concatenating them let two profiles collide
  and a warm call was served the other profile's narrower body.
- An adapter returning `(nil, nil)` dereferenced nil in the shaping step. A
  panic inside a response annotator ended the whole `gum mcp --stdio` session.
- `gum cache clear --expired` printed every matched key as removed even when the
  delete transaction failed and the rows were still on disk. An over-size store
  that could no longer evict grew past `max_size_bytes` silently. `Get` read a
  hot-tier timestamp outside the lock, reproducible under `-race`.
- The HTTP cache migration ran one autocommit per row, rewrote every key as
  `<bucket>/<key>` so migrated `gum-cache` responses became permanently
  unreachable, and wrote every row with ttl 0, resurrecting expired entries and
  stripping the deadline from live ones. `--force`, the documented recovery for
  `ErrSQLiteCorrupt`, returned the very error it exists to clear.
- A quarantined plugin whose subprocess died is respawned; a canary spawn of a
  quarantined plugin is refused; a passing setup canary clears quarantine; and
  an install writes the initial state §8 specifies.
- Stored plugin credentials reach the subprocess. `gum plugin setup` wrote each
  one to the keychain and nothing read it back, so every plugin needing a
  credential failed its canary and was quarantined with no retry time.
- `gum plugin setup` failed with "no input provided" on the second credential of
  a two-credential manifest, because it built one scanner per descriptor and the
  first buffered the whole stream.
- `gum auth use-oauth-client` erased the stored client secret when re-run
  without a secret flag, and the next token exchange failed `invalid_client`.
- `gum auth logout` returned early when no BYO client was registered, so the
  `gum_oauth` vault purge never ran and legacy refresh tokens stayed in the
  keychain under a "nothing to clear" message.
- `--raw` and `--no-field-mask` were parsed and discarded.
- The MCP `OP_NOT_FOUND` envelope carried five suggestions where §4.1 caps it at
  three.
- A typed scalar nested in `body:='{"mode":5}'` skipped the enum check the flat
  form gets, so a bad value reached the API as a 400 instead of a local
  `CLI_ARG_INVALID`.
- `applyCollapseArrays` panicked on a negative `max_items`.
- The audit log's §11 omit-when-false rule was broken: `marshalEntry` dropped
  `risk_override_reason`, `shaping_bypassed`, and `sanitizer_bypassed` from the
  canonical block without recording the skip, so the extras pass re-added them
  in alphabetical position. Latent; no shipped caller passes those keys.
- The fsync-fallback warning goes to the audit log instead of stderr.
- The fixture harness measured the source body, so `expect_result_count`
  asserted the upstream row count and `expect_omitted_count` could only match 0.

### Security

- A symlinked confirmation signing key is refused on both read paths. The
  lost-race adopt path used `os.ReadFile`, which follows symlinks, so anyone able
  to write the data directory could plant a link at
  `confirmation-signing.key` and have gum sign destructive-confirmation tokens
  with a key they control.
- `gum.read` with `variant_id` pinned to a destructive variant executed it. The
  risk-tier pre-check compared the tier against the op's default variant while
  the kernel honoured the pin, and the same handler copied `allow_destructive`
  out of the arguments. Reachable only through a plugin-contributed catalog,
  since no shipped op has variants of differing risk class.
- `gum plugin setup` echoed every typed secret to the terminal and into
  scrollback. The prompt now clears ECHO around each read.
- A quarantined plugin with no retry time was spawned anyway, because the window
  check read a zero time as elapsed.
- Deleting one file disabled the plugin executable trust check: the digest
  sidecar read returned `("", nil)` when missing and `Start` skipped
  verification on an empty wanted digest. `plugins.lock` is now authoritative
  and the sidecar must agree with it.
- A plugin manifest may not claim a host-owned environment variable in
  `needs_user_creds` or `env_allow`. Without the gate a manifest could list
  `GUM_OAUTH_CLIENT_SECRET` and plugin setup would prompt the operator for it by
  display name alone.
- A plugin-catalog `schema_hashes` value became a path segment with no
  validation, so a row carrying `json/../../secret` read any file the process
  could reach. The value must be lowercase sha256 hex.
- On GCE, Cloud Run, and GKE every workload derived the same auth subject
  fingerprint, because `FindDefaultCredentials` fills JSON only when it loaded a
  credential file. That fingerprint keys tee artifact HMACs, `gum://results`
  handles, cache entries, and gain-ledger rows, so two service accounts sharing
  a profile directory could read and overwrite each other's artifacts. Resolve
  reads the default service-account email from the metadata server and fails
  with `ADC_SUBJECT_UNKNOWN` when it cannot.
- The `byo_oauth` principal fingerprint is keyed on the account, not the refresh
  token. Google rotates that token on every re-login, so one account walked to a
  new fingerprint and left its cache rows, result handles, and ledger rows
  unreachable.
- Confirmation tokens no longer bind an empty principal.
  `ConfirmationParams.AuthFingerprint` read a field no path assigns, hashing the
  empty string on every call while reading as a wrong-account guarantee.
- `TestNoManagedOAuthInjectionInBuildSurfaces` scans GoReleaser ldflags,
  workflow environment blocks, HASP targets, and build scripts for an injected
  OAuth client secret. The previous gate scanned committed `.go` files only,
  which all four surfaces bypass.

## [1.5.0] - 2026-09-18

### Added

- Keyword Planner location and language defaults.
  `GUM_GOOGLE_ADS_GEO_TARGET_CONSTANTS`, `GUM_GOOGLE_ADS_LANGUAGE`, and the
  profile keys `googleads.geo_target_constants` and `googleads.language` fill
  `geoTargetConstants` and `language` when a `googleads.keywordPlanIdeas` call
  omits them. A call argument wins over the environment, and the environment
  wins over the profile config. Passing `"geoTargetConstants":[]` and
  `"language":""` asks one call for worldwide figures.
- `matchedInputs` and `unmatchedInputs` on
  `googleads.keywordPlanIdeas.generateKeywordHistoricalMetrics`. Google merges
  close variants of the submitted keywords into one result, so a batch of 245
  keywords can return 243 results. `matchedInputs` names the submitted keywords
  a merged result covers, and `unmatchedInputs` lists the keywords that reached
  no result. `--format raw` still returns the upstream body.

### Changed

- An adapter can add fields to an upstream response before the expression
  profile runs. Specification §9.1 states the rule: an adapter may add fields,
  may not drop or rewrite upstream fields, and never runs on `--format raw` or
  on the recovery artifact.

### Fixed

- `gum config set googleads.geo_target_constants 2840` reported
  `accepts 1 arg(s), received 2` and named no accepted form. The error now
  names the `key=value` form and rewrites the caller's own tokens into a
  runnable example, including `--profile` when the caller passed it.

## [1.4.0] - 2026-09-18

### Added

- A default Google Ads account. `GUM_GOOGLE_ADS_CUSTOMER_ID`,
  `GUM_GOOGLE_ADS_LOGIN_CUSTOMER_ID`, and the profile keys
  `googleads.customer_id` and `googleads.login_customer_id` fill
  `customerId` and `loginCustomerId` when a call omits them. A call argument
  wins over the environment, and the environment wins over the profile config.
- A Keyword Planner guide for `geoTargetConstants` and `language`. Calls that
  omit them return worldwide data for all languages.

### Changed

- The `gum call` wizard no longer prompts for an account id that a default
  supplies. A missing `customerId` error names both ways to set a default.

### Fixed

- The specification said request-field defaults were not injected. It now
  describes the shipped behavior: catalog defaults and configured account
  defaults are added before canonicalization, cache keys, and audit hashes.

### Security

- `google.golang.org/grpc` v1.83.2 fixes GO-2026-6348, which `govulncheck`
  found on a call path, plus GO-2026-6441 and GO-2026-6443.
- `golang.org/x/crypto` v0.56.0 fixes GO-2026-6303, GO-2026-6354, and
  GO-2026-6355.

## [1.3.0] - 2026-09-07

### Added

- Data Manager API event ingestion through `datamanager.events.ingest` and
  diagnostics through `datamanager.requestStatus.retrieve`.
- A setup guide for the Data Manager OAuth scope, request body, validation,
  and asynchronous processing checks.

### Fixed

- Conflicting boolean fields in `gum call` no longer silently discard a
  validation flag in favor of an explicit body.
- REST POST and PATCH requests are not retried after transport or response-read
  failures, including a failed attempt after an HTTP 429 rejection.
- Google validation errors retain request IDs and bounded field diagnostics.

## [1.2.0] - 2026-09-06

### Added

- Google Ads GAQL reporting through `googleads.googleAds.search`.
- Batch resource mutations through `googleads.googleAds.mutate`, with
  destructive confirmation and validation-only support.
- Offline conversion uploads through
  `googleads.conversionUploads.uploadClickConversions` for eligible developer
  tokens, with up to 2,000 conversions per request.

### Changed

- Google Ads errors expose upstream error codes, field paths, triggers, and
  request IDs when available.
- Google Ads writes are sent once to avoid replaying mutations after an
  ambiguous server error. Read operations retain bounded retries.
- New Google Ads writes reject raw bodies and malformed boolean options
  before sending a request. Conversion uploads always use partial failure;
  callers must inspect `partialFailureError` even after HTTP 200.

## [1.1.0] - 2026-08-07

### Added

- Added the read-only `gmail.users.messages.attachments.get` catalog operation.
  It retrieves a message attachment through the Gmail v1 API with
  `gmail.readonly` scope and requires `userId`, `messageId`, and attachment
  `id` path arguments.

### Changed

- The public docs site now deploys from the public repository's `main` branch.
  A stable-release tag cannot publish binaries until the live release notes and
  changelog match the tagged source.

## [1.0.3] - 2026-08-06

### Changed

- The MCP server speaks protocol revision 2026-07-28, built on
  `modelcontextprotocol/go-sdk` v1.7.0. Clients on the older revision are
  served by the same code path.
- `resources/read` on an unknown URI answers with JSON-RPC `-32602` instead of
  `-32002`, and an unknown method over stdio answers `-32601`.
- `tools/list` always carries `readOnlyHint` and `idempotentHint`, including
  when the value is `false`. `destructiveHint` and `openWorldHint` keep their
  present-or-absent behaviour.
- Project-local profile lookup obtains the client's roots through an input
  request carried on the tool result, because revision 2026-07-28 removed the
  server-initiated `roots/list` call it used before. That costs one extra round
  trip on the first tool call of a session. The resolution rules, the
  `_meta.gumRoot` selection in multi-root sessions, and the
  `PROJECT_ROOT_REQUIRED` envelope are unchanged.
- The capability catalog was regenerated from the upstream discovery documents.
  222 operations before and after: none added, none removed, seven changed. Six
  gained an optional upstream request field (`groupIdFilter`,
  `includeSensitiveData`, `showOwnOrganizationOnly`, `eventLabelVersion`, and
  `markupSyntax` on two chat operations), along with the enum values
  `workspace_studio` and `writerWithoutPrivateAccess`. One Drive operation
  description was reworded. No risk class, scope, auth strategy, or required
  argument changed.

## [1.0.2] - 2026-08-06

### Changed

- gum is now MIT licensed. It was FSL-1.1-ALv2 through v1.0.1.
- Published docs site at the `docs/` tree, including a command reference
  generated from `gum schema --json` and per-service pages.
- New `gum schema --json` command. The docs generators consume it; it also
  gives agents a machine-readable view of the CLI surface.
- `gum plugin list` reads `plugin-state.json` and prints status, retry count,
  next retry, and last error for every install dir, quarantined ones included.
  Before this it listed only plugins whose manifest loaded, so a quarantined
  plugin was invisible.
- CLI help and error text no longer cite internal `spec.md` sections. The
  public docs are the reference surface.

### Fixed

- An expression profile that dropped fields left no trace in the output, so a
  caller could not tell an absent field from one the API never returned.
  `profile.Apply` now records the dropped paths; the CLI prints them on stderr
  and MCP returns them in a text block, naming the recovery artifact when tee
  wrote one.
- A cache hit returned the stored body verbatim and skipped output shaping, so
  a warm call could answer in a format the caller never asked for. Cached
  bodies now go through the same shaping as cold ones, a bad `--format` fails
  identically warm and cold, and `raw`-format responses are no longer cached.
- A quarantined plugin returned `SERVICE_DOWN`, which read as an upstream
  outage. It now returns `VARIANT_QUARANTINED`.
- An op declaring a request-level field default failed with a required-arg
  error. Catalog defaults are applied before validation.
- An `integer` argument accepted any number, so `destructive_budget=2.5`
  passed local validation and became an opaque upstream 400. Fractional
  values, NaN, and infinities are rejected.
- `gum.code`'s `destructive_budget` was declared `int` while the type checker
  only knew `integer`, so the parameter had no type checking at all.
- `gum schema` panicked on a command whose usage line had no arguments.
- A long-running-operation poll ignored a malformed `done` field, reporting a
  finished operation as still running and waiting for a completion that had
  already happened.
- `gum config`, the update-notification cache, the canary registry, and
  `gum init` settings each wrote through a fixed `<path>.tmp` with no fsync.
  Two gum processes writing at once could rename each other's partial bytes
  into place, and a crash just after the rename could leave a zero-length
  file. All four now use one atomic-write helper with a unique temp name and
  an fsync.

### Security

- Dependency refresh: `govulncheck` reports 0 known vulnerabilities against
  the updated module graph.

## [1.0.1] - 2026-06-18

### Changed

- `gum auth probe` defaults to `--strategy auto`. It checks the BYO OAuth client
  and grant created by `gum auth login` first, then falls back to ADC when no
  BYO client is configured.
- `gum auth probe --strategy adc` keeps the old ADC-only check for operators who
  want to test gcloud or Application Default Credentials directly.

### Fixed

- `gum auth probe --scopes https://www.googleapis.com/auth/adwords` no longer
  ignores a fresh BYO OAuth login and fails against stale ADC credentials with
  `invalid_rapt`.
- Probe scopes are normalized before token resolution, so short names such as
  `adwords` work with BYO OAuth grants.

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

[1.0.3]: https://github.com/ehmo/gum/compare/v1.0.2...v1.0.3
[1.0.2]: https://github.com/ehmo/gum/compare/v1.0.1...v1.0.2
[1.0.1]: https://github.com/ehmo/gum/compare/v1.0.0...v1.0.1
[1.0.0]: https://github.com/ehmo/gum/releases/tag/v1.0.0
[0.1.0]: https://github.com/ehmo/gum/releases/tag/v0.1.0

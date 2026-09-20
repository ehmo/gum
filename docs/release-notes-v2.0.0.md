---
title: gum v2.0.0 release notes
date: 2026-09-20
status: release
---

# gum v2.0.0

A whole-repository review closed the gap between what the specification
promised and what the binary did. The major version changes because several
fixes now reject input that earlier versions accepted.

## Highlights

- `gum` no longer hangs at startup on a Linux host whose Secret Service
  collection is locked. Every OS keychain call is bounded.
- `csv` and `markdown` are real encoders. Both were in the advertised format
  enum and both returned TOON or JSON under the requested label.
- Every result carries the `_expression` envelope, so a caller can see that a
  profile dropped fields or collapsed rows. No production path emitted one.
- MCP tools enforce the `inputSchema` they advertise. A closed enum, a required
  property and `additionalProperties:false` were advisory.
- `tee_mode="failures"` writes an artifact, the gain ledger records traffic, and
  a cache hit gets the same recovery artifact as a cold call. All three were
  dead paths.
- Five security fixes, including a symlinked confirmation signing key, a
  risk-tier bypass that let `gum.read` execute a destructive variant, and a
  shared auth-subject fingerprint across every workload on GCE.

## Install

```sh
brew install ehmo/tap/gum
# If already installed:
brew update
brew upgrade ehmo/tap/gum
gum --version
```

The standalone installer remains available:

```sh
curl -fsSL https://raw.githubusercontent.com/ehmo/gum/main/install.sh | GUM_VERSION=v2.0.0 bash
```

## Upgrade notes

Most installations need no change. Four cases need action.

**1. An expression profile using `default_format`.** Rename the key to
`format`. The specification and `docs/expression-profile-dsl.md` have always
named it `format`; the parser read `default_format`, so every documented
example failed to validate. The enum widens to `toon|csv|json|markdown`.
`raw` is rejected, because it names a caller choice that skips shaping rather
than an encoder a profile selects; pass `--format raw` instead. The config key
`output.default_format` is unchanged.

```diff
-default_format = "json"
+format = "json"
```

Validate before upgrading in place:

```sh
gum profile validate <path>
```

**2. A profile or call using `field_mask_mode="dual_fetch"`.** The mode is
rejected with `INVALID_ARGS`. It promised a second unmasked upstream fetch to
feed the recovery artifact and the kernel only ever issued one request. Use
`field_mask_mode="none"` to send no mask, or drop the key.

**3. A script calling MCP tools with arguments the schema forbade.** Arguments
are now validated. In particular `gmail_search` takes `q`, not `query`;
`flights_search` takes `departure_date`, not `departureDate`; the four Gmail
convenience tools require `userId`; and `gum.code`'s `destructive_scope` items
are `{op_id, resource_key}` objects, not strings.

**4. A script reading the exit code of `gum cache migrate`, `gum auth
use-api-key`, or `gum auth use-ads-developer-token`.** All three returned 0 on
failures that their own output reported. They now exit non-zero. A script with
`migrate || handle` will start taking the `handle` branch on
`RSYNC_AMBIGUITY`.

## Breaking

- The expression-profile key is `format`, not `default_format`, with the enum
  `toon|csv|json|markdown`.
- `field_mask_mode="dual_fetch"` is rejected before any upstream request.
- `gum cache migrate` exits non-zero on `RSYNC_AMBIGUITY`.
- `gum auth use-api-key` and `gum auth use-ads-developer-token` exit non-zero
  when the keychain write fails. A platform with no keychain backend keeps the
  environment-variable fallback and still exits 0.
- The nine MCP meta-tools, the two skill helpers and the 18 convenience tools
  validate arguments against their advertised `inputSchema`.
- The 18 convenience tool schemas use the §4.1 argument names of their backing
  ops. `drive.get_file` drops `mimeType`, `drive.share` drops `emailMessage`,
  and `gmail_get_message` drops `format`, because no op declares them.
- `gum.read`, `gum.write` and `gum.destructive` ignore `allow_write` and
  `allow_destructive` in their arguments.
- `gum.code`'s `destructive_scope` items are `{op_id, resource_key}` objects.
- `gum.poll` registers `RawJsonResult` and reports `LRO_TIMEOUT` as an error.
- `gum.describe_op`, `gum.gain` and `gum.cache_stats` register the result
  schemas §2256 to §2258 name. `skills_get` drops its output schema.
- TOON keeps the keys of a map whose values are all empty.
- `truncate_strings` puts the ellipsis inside the limit, not past it.
- `gum_print` encodes a non-string value as JSON.
- Response numbers decode through `json.Number`, so an integer above 2^53 keeps
  its digits.
- The per-op semantic TTL table is keyed on catalog op ids.
- `gum profile test` takes `--name`, not `--profile`, to pick one definition
  from a multi-profile file.

## Added

- `gum plugin info <name>`. `--format=json` emits the same object the
  `gum://plugin/{name}` MCP resource carries.
- `--max-items` on `gum read|write|destructive|call` and `max_items` on the
  three MCP risk tools, replacing the profile's `collapse_arrays` cap for one
  invocation. `all` skips the stage. The override does not change the cache key
  or the args hash.
- `csv` and `markdown` encoders at stage 8.
- The `_expression` envelope on every §13 result shape, and the same
  information on stderr as a one-line CLI shaping notice.
- `unsupported_capabilities` in the catalog ABI, and `"partial"` in the
  `DescribeOpResult` `execution_support` enums.
- The documented profile file envelope: `[output_profiles."<name>"]`,
  `[override_bindings]` and top-level `[[tests]]`. Override bindings now reach
  dispatch.
- `occurrence_count` on a deduped row and `<field>_truncated` siblings from
  `truncate_strings`.
- `GUM_KEYRING_TIMEOUT` to override the keychain call bound.
- `--destructive-budget` and repeatable `--destructive-scope` on `gum code`,
  without which `--allow-destructive` could never execute.
- Five profile DSL keys documented that parsed only in Go: `projection`,
  `flatten_singletons`, `sort_by`, `limit`, `omit_zero_counts`.

## Changed

- Stage 1 injects the variant's `default_fields` as the upstream field mask when
  a profile states none. No shipped variant declares `default_fields`, so
  nothing new goes upstream today.
- `results` is a recognised record-array key alongside `items`, `data` and
  `messages`.
- The shaping notice states the row counts `dedupe` and `limit` removed.
- `_expression.omitted_count` counts every row shaping drops, and
  `_expression.lossy` reads the cap in force.
- CSV carries top-level scalars as repeated columns and secondary arrays as one
  count column each. Markdown no longer truncates cells.
- Every kernel error carries a §7 envelope. Anything unstructured becomes
  `SERVICE_DOWN` with the cause kept in the chain.
- A non-`gum_oauth` auth failure carries `auth_strategy`,
  `missing_components` and `setup_command`, and keeps its own error code.
- `gum plugin remove` and reinstall run as registry transactions.
- Catalog variants sort by `variant_id` before JCS hashing.
- `gen-catalog` validates every variant's `output_profile` name at build.
- `staticcheck` pins to v0.8.1, which can read Go 1.26 export data.

## Fixed

- `gum` hung forever at startup on a locked Secret Service collection, which
  covers headless, SSH, container and CI hosts. go-keyring parks on a D-Bus
  unlock prompt nobody can answer, and every invocation reached it through
  `GrantedScopes`. Keychain calls are bounded at 20s interactive, 2s
  background.
- `tee_mode="failures"` wrote nothing, because the write sat past the point the
  step-7 error path returns from.
- The gain ledger was never written. Nothing set `dispatch.Config.Ledger`
  outside tests, so `gum gain` reported an empty ledger however much traffic a
  profile had served. The file was also created 0644 instead of 0600, ignored
  `XDG_DATA_HOME` and the profile segment, and did not honour
  `gain.enabled=false`.
- A cache hit skipped the recovery artifact, so a warm response named dropped
  paths with no artifact to recover them from.
- TOON `Decode` cut a string holding a newline in half and returned a nil
  error. `{"a": "x\ny=z"}` decoded to `{"a": "\"x", "y": "z\""}`: truncated,
  with a dangling quote and a fabricated field. Verified with 30s of
  `FuzzToonDecode`, 11.2M executions, no crash.
- `dedupe`, `sort_by` and `limit` required a top-level array, which no real
  Google list response is. `dedupe` also collapsed a result set to one row when
  its key fields were all absent.
- `k=-1` on `gum.search_apis` panicked the stdio server.
- A confirmation token's replay marker was keyed on the caller's hex signature
  string, so re-spelling that field in upper case bypassed it and one approval
  authorized unbounded executions.
- `canonicalizeArgs` drops null-valued keys at every depth, so an argument
  spelled as null and an omitted argument share one cache key. Two profiles can
  no longer collide on the semantic cache key and be served each other's body.
- `gum cache clear --expired` counted matched keys, not committed deletions.
- The HTTP cache migration autocommitted per row, rewrote every `gum-cache` key
  into an unreachable form, and wrote every row with ttl 0.
- Stored plugin credentials reach the subprocess. `gum plugin setup` wrote them
  to the keychain and nothing read them back, so every plugin needing a
  credential failed its canary.
- `gum plugin setup` failed on the second credential of a two-credential
  manifest.
- `gum auth use-oauth-client` erased the stored client secret when re-run
  without a secret flag.
- `gum auth logout` skipped the `gum_oauth` vault purge when no BYO client was
  registered.
- `--raw` and `--no-field-mask` were parsed and discarded.
- A typed scalar nested in `body:='{"mode":5}'` skipped the enum check the flat
  form gets.
- The audit log's §11 omit-when-false rule re-added the keys it had just
  dropped, in the wrong position. No shipped caller passes those keys.

## Security

- A symlinked confirmation signing key is refused on both read paths. The
  lost-race adopt path followed symlinks, so anyone able to write the data
  directory could have gum sign destructive-confirmation tokens with a key they
  control.
- `gum.read` with `variant_id` pinned to a destructive variant executed it,
  because the risk-tier pre-check compared against the op's default variant
  while the kernel honoured the pin. The same handler read `allow_destructive`
  out of the caller's arguments. Reachable only through a plugin-contributed
  catalog; no shipped op has variants of differing risk class.
- `gum plugin setup` echoed every typed secret to the terminal.
- Deleting one file disabled the plugin executable trust check. `plugins.lock`
  is now authoritative and the digest sidecar must agree with it.
- A quarantined plugin with no retry time was spawned anyway.
- A plugin manifest may not claim a host-owned environment variable in
  `needs_user_creds` or `env_allow`, which a manifest could use to have setup
  prompt the operator for `GUM_OAUTH_CLIENT_SECRET` by display name.
- A plugin-catalog `schema_hashes` value became an unvalidated path segment, so
  a row carrying `json/../../secret` read any file the process could reach.
- On GCE, Cloud Run and GKE every workload derived the same auth-subject
  fingerprint. That fingerprint keys tee artifact HMACs, `gum://results`
  handles, cache entries and gain-ledger rows, so two service accounts sharing a
  profile directory could read and overwrite each other's artifacts. Resolve
  now reads the service-account email from the metadata server and fails with
  `ADC_SUBJECT_UNKNOWN` when it cannot.
- The `byo_oauth` principal fingerprint keys on the account, not the refresh
  token, which Google rotates on every re-login.
- Confirmation tokens no longer bind an empty principal.
- `govulncheck` reports 0 vulnerabilities that gum's code calls and 0 in
  packages gum imports. One advisory remains in the module graph: GO-2026-5932
  marks `golang.org/x/crypto/openpgp` unmaintained. gum imports no symbol from
  that package, and the advisory has no fixed version.

## Known limitations

- No catalog variant declares `default_fields`, so the §770 requirement that
  every variant carry them is still unmet and the stage-1 field mask has nothing
  to inject. `--fields` completion is therefore empty.
- Nothing converts a plugin registry variant row into a dispatchable catalog
  variant, so `gum plugin install` runs no catalog validation. The plugin
  contract doc now says so.
- No catalog variant declares a non-`full` `execution_support`, so
  `unsupported_capabilities` and `"partial"` are declared and tested but not
  exercised by shipped data.
- `field_mask_mode="dual_fetch"` is refused rather than implemented.
- The §10.2 HTTP and ETag cache backend has no production reader or writer.
  `http.db` and `http-wal.db` are created and never used.
- macOS binaries are not notarized. The Homebrew formula clears quarantine
  during installation. For standalone installs, inspect with
  `spctl --assess --type execute --verbose gum` and use
  `xattr -d com.apple.quarantine gum` if Gatekeeper rejects the binary.

## Token savings

Measured with the release fixtures using a local build stamped 2.0.0. Run from
the `apps/gum` directory of the matching source checkout:

```sh
gum gain --fixture-replay --format=toon
gum gain --fixture-replay --format=json
```

| Default format | Total calls | Total tokens in | Total tokens saved | Aggregate savings |
| --- | ---: | ---: | ---: | ---: |
| `toon` | 10 | 3,922 | 0 | 0 % |
| `json` | 10 | 3,922 | -12 | 0.31 % overhead |

## Verification

All seven jobs in the [v2.0.0 release workflow](https://github.com/ehmo/gum/actions/runs/35527377730)
passed: tag validation, live docs match, tests, `govulncheck`, the GoReleaser
build, the independent four-platform rebuild, and the provenance comparison.

The four downloaded archives matched `checksums.txt`, and each one matched its
subject digest in `gum-v2.0.0.intoto.jsonl`. The extracted darwin/arm64 binary
matched `release-binaries.sha256`.

A local rebuild reproduced all four published binaries. The command in
Reproducibility below, run from a clean clone at tag `v2.0.0` on one darwin/arm64
host with `GOTOOLCHAIN=go1.26.7`, produced these hashes:

| Target | sha256 |
| --- | --- |
| darwin/amd64 | `e7cfd445523b3ea6bbddd4a24cf728f4c4b75d003f0f1f1a365187962d7304a5` |
| darwin/arm64 | `332081264900db51e6602826ffc4fa94b2bbe156885185752a27bc0e138c51d0` |
| linux/amd64 | `4b258174657c55210caef26a0fbe37ff8201d1d3373caf368638859202423af5` |
| linux/arm64 | `1cc2dba8270dda1b4274732487552070f84f8fc0d49a5f5769b3a182df4fc156` |

Each hash matches the matching line in `release-binaries.sha256`.

The Homebrew installation reports 2.0.0 and `gum doctor` passed every check.
`brew audit --strict --online --os=all --arch=all ehmo/tap/gum` and
`brew test ehmo/tap/gum` both passed.

## Reproducibility

```sh
git clone https://github.com/ehmo/gum.git
cd gum && git checkout v2.0.0
cd apps/gum
GOTOOLCHAIN=go1.26.7 CGO_ENABLED=0 GOOS=<os> GOARCH=<arch> go build -trimpath \
  -ldflags='-s -w -X main.version=2.0.0' ./cmd/gum
sha256sum gum
```

All four published binaries were built with `go1.26.7`. The hashes above were
produced by cross-compiling every target from one host, so a single machine can
check the whole set.

Build from a full clone, not from a linked `git worktree`. Go embeds the commit
revision in the binary, and it silently skips that stamp in a linked worktree,
which changes the hash.

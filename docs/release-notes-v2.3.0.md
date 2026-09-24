---
title: gum v2.3.0 release notes
date: 2026-09-23
status: release
---

# gum v2.3.0

This release builds the prompt-injection defenses the specification had only
described, corrects two RFC 8785 bugs that made gum's argument hashes disagree
with every other implementation, logs failed dispatches for the first time, and
replaces around 240 documentation claims that the shipped binary contradicted.

## Highlights

- Prompt-injection defense is built, in two layers. Layer 1 wraps the first
  text content block of a dispatched op response in
  `<external_data trusted="false">` markers at the MCP presentation boundary.
  Layer 2 scrubs the error envelope, where a Google 400 echoes the argument
  that failed validation and carried third-party calendar titles and file
  names into `message`. Catalog and plugin descriptions now pass the same
  sanitizer at build time and at manifest load.
- `args_canonical` follows RFC 8785. Number formatting and control-character
  escaping were both wrong, so a hash computed by any other implementation
  never matched gum's for the affected values. The semantic cache key, the
  ETag cache key, the tee hash, the confirmation token and the scope-upgrade
  request hash all derive from that string.
- Every failed dispatch is appended to `audit.jsonl` with its `error_code`.
  Only successes were logged, so a rejected destructive call, an
  `AUTH_REQUIRED` and an upstream 403 all left the log silent.
- The documentation describes the binary that ships. Around 240 comments,
  error strings, embedded descriptions and doc sentences stopped using
  `v0.1.0` as a synonym for the current build, and new gates fail the build
  when a citation names a repository path, a `gum` subcommand, an MCP tool, a
  specification label or a test-matrix row that does not exist.
- `gum.describe_op` obeys its own limits. The `max_variants` and `max_chars`
  knobs were documented with defaults and ranges while the handler ignored
  both, so an op with a 900-character summary returned the whole summary.

## Install

```bash
curl -fsSL https://raw.githubusercontent.com/ehmo/gum/main/install.sh | GUM_VERSION=v2.3.0 bash
gum --version
gum doctor
```

## Upgrade notes

No configuration, credential, or stored state changes. Four behavior changes
are visible to a caller.

`args_canonical` now formats a narrow class of values differently, so anything
derived from it changes for calls carrying those values: a non-integer at or
above 1e7, an exponent that used to be zero-padded, `1e-06`, or a control
character in U+0008, U+0009, U+000A, U+000C or U+000D. Cached entries keyed on
such a call miss once and refetch. A confirmation token minted by an older
binary for such a call no longer validates; re-run the call to mint a new one.
Nothing else needs clearing, and `gum cache clear` is optional.

`_profile_resolution_warning` is gone from the response envelope, from
`ExpressionMeta`, from its `Fields` projection and from the registered MCP
outputSchema. Nothing ever set it. A client that reads the field gets nothing;
`ExpressionMeta` stays open, so removing an optional property loosens no
validator.

A roots-less MCP client no longer falls back to a project root. Project-local
profile lookup is off without roots, and resolution falls back to user-global
and then to the catalog-embedded profile with no opt-in. The
`--allow-implicit-project-root` flag the documentation offered never parsed and
the documentation no longer names it.

`--no-warn-recovery` is deprecated in favor of `--no-warn-lossy`. It still
parses and prints a deprecation notice. It is kept for one release cycle.

## Added

- Every failed dispatch is appended to `audit.jsonl` with its `error_code`.
  Only successes were logged, and the error path reached the audit sink from
  one place that fired only under `--unsanitized`, so a rejected destructive
  call, an `AUTH_REQUIRED` and an upstream 403 all left the log silent.
  `error_code` is optional and absent on success, so the entry shape stays at
  `v: 1`.
- `OVERRIDE_DISABLES_LOSSY_STAGE`. An override that weakens the profile it
  displaces on any of the six loss-driving fields now warns.
  `gum profile validate` and the CLI runtime loader write to stderr; the MCP
  dispatch path logs, because the stdio transport owns stdout. An omitted
  `recovery` counts as a removal, since resolution swaps the profile whole
  rather than merging field by field. `field_mask_mode` is the exception:
  empty means upstream, so omitting it keeps the mask. `--no-warn-lossy`
  suppresses the warning, and a long-lived `gum mcp --stdio` session prints
  each distinct warning once.
- `gum call --unsanitized` returns the raw error body. It warns on stderr,
  requires `--yes-unsanitized` when stdin is not a TTY, and logs
  `sanitizer_bypassed: true`. `internal/mcp` never sets it.
- `shaping_bypassed: true` on `--raw`, which the specification promised and
  nothing wrote.
- The RFC 8785 reference test corpus is vendored at
  `internal/output/jcs/testdata/upstream` from
  `github.com/cyberphone/json-canonicalization` under Apache-2.0. Its six
  input/output pairs run byte for byte, and the test fails if the directory
  holds anything other than six pairs, so a corpus that lost files cannot pass
  by testing nothing. A second test regenerates the first 10000 lines of that
  project's 100-million-value number corpus, checks the stream against the
  SHA-256 its README publishes, and compares every value against gum's
  formatter.
- `docs/mcp.md` documents the five meta-tool tuning keys with their defaults,
  ranges and clamping behavior. `docs/agent-setup.md` carries a provenance
  table mapping each of the five JSON transcripts under
  `docs/agent-contracts/` to the command that produces it.
- Nine new gates hold documentation to the tree: the §13 prompt-body size cap
  is measured against the 6 KiB limit, MCP tool registration is contained to
  `internal/mcp`, no prose cites an unregistered `gum.*` tool, no document
  cites an unknown `gum` subcommand, the five agent-contract transcripts are
  compared byte for byte against live output, no citation names a
  `docs/test-matrix.md` row by ordinal, no requirement cell in that table
  dates a gate to a v0.x release, every quoted specification label resolves,
  and every backticked repository path resolves to a file or a directory.

## Changed

- A roots-less MCP client no longer falls back to a project root. See Upgrade
  notes.
- Catalog generation rejects any variant that declares `gum_oauth` and fails
  the whole run, rather than degrading the variant to `byo_oauth` with a
  warning. `GUM_OAUTH_SCOPE_NOT_MANAGED` never existed in any Go file. The
  shipped catalog declares zero `gum_oauth` variants.
- `gum.describe_op` truncates its own string fields. The §9.4
  `truncate_strings` stage runs over the result at the documented
  400-character default. The stage copies the result and leaves the embedded
  catalog untouched.

## Fixed

- The specification describes the build that ships. It read as a v0.x roadmap,
  claiming absent defenses as shipped and naming target releases that came and
  went. Around 240 comments, error strings, embedded JSON descriptions and doc
  sentences stopped using `v0.1.0` as a synonym for the current build, and
  comments now cite specification sections instead of line numbers, which
  drift on every edit. Two gates in `internal/lint` keep both rules enforced.
- Numbers in `args_canonical` follow RFC 8785 §3.2.2.3. `canonicalNumber`
  formatted every non-integer with strconv's `'g'` verb, which is not the
  ECMAScript `Number::toString` algorithm the RFC adopts. It disagreed on
  three classes of value: it moved to an exponent at 1e7, where ES6 still
  writes `10000000`; it padded the exponent to two digits, writing `1e-07`
  where ES6 writes `1e-7`; and it wrote `1e-06` where ES6 writes `0.000001`.
  `es6Number` now walks the five rules off
  `strconv.FormatFloat(f, 'e', -1, 64)`. The exact-digit `int64`/`uint64` fast
  path is kept and documented as a deliberate departure above 2^53.
- Control characters in `args_canonical` follow RFC 8785 §3.2.2.2. Every code
  point below U+0020 was written `\uXXXX`, and the function's own doc comment
  asserted the RFC demanded it. It requires `\b`, `\t`, `\n`, `\f` and `\r`
  for U+0008, U+0009, U+000A, U+000C and U+000D, and hex for the rest,
  U+000B included, since JSON defines no `\v`. Three of the six reference
  vectors caught it.
- The two §9.4 `meta_tools.describe_op.*` knobs are read. `max_variants` and
  `max_chars` now load from the active expression profile and clamp to the
  documented ranges, and an unparseable value falls back to the default.
- The `LRO_UNSUPPORTED_IN_CODE` refusal names a tool that exists. It told the
  caller to use a `gum.call` tool the server has never registered; the
  risk-tiered `gum.read`, `gum.write` and `gum.destructive` trio replaced it.
  The message now points at the CLI or the matching risk-class tool and at
  `gum.poll`.
- `TEE_SECRET_CORRUPT` names a command that exists. It told the user to run
  `gum cache repair`; `gum cache` carries `clear`, `migrate` and `stats`, so
  the one repair path is removing the tee directory, which the message now
  says. Seven further citations were corrected, among them
  `gum config <key>=<value>` without the `set` verb.
- `gum profile validate` takes one path and `--variant`. The documented
  `--mcp-roots` fixture mode never existed, and `project_root_uri` was
  declared and never emitted.
- ADC is described as it resolves. The specification named a per-profile
  `gum auth use-adc` mode that no command tree carries. Resolution reads
  `GOOGLE_APPLICATION_CREDENTIALS`, then the gcloud cache, then the GCE
  metadata server. `gum auth status` reports which sources are present
  without a network call, and `gum auth probe --strategy adc` exercises the
  chain.
- `AUTH_KEYCHAIN_UNAVAILABLE` carries the envelope it emits. The
  specification promised `backend`, `detail` and `setup_command` members; the
  error is the ordinary §7 shape without them.
- The `hasp run` example in `README.md` and `docs/hasp.md` can run.
  `hasp run --target gum-work` resolves a manifest target, not the app profile
  `hasp app connect` saves, so the documented sequence failed. Both pages now
  use `--project-root .` with session grants, matching the fix the bundled
  skill already had.
- Two published transcripts matched no command output.
  `docs/agent-contracts/cli-skills-hasp.json` and `cli-skills-list.json`
  carried the pre-fix hasp skill body with a stale `sha256` and byte count.
  Both are regenerated from the live commands.
- Appendix A holds `go.mod` to the specification. It promised "CI fails the
  build on drift from these floors" with nothing reading it.
  `spf13/viper`, `refraction-networking/utls` and
  `cyberphone/json-canonicalization` were pinned at exact versions while none
  appeared in `go.mod` or in any import, and `lukechampine.com/blake3`, which
  signs every confirmation token, had no row at all.
- Documentation cites paths and gates that exist. The specification named
  `internal/init/GUM.md.tmpl`, which does not exist; the template lives at
  `internal/initpkg/GUM.md.tmpl`. The stub-expiry procedure read the release
  version from a nonexistent `internal/version` package and is now marked
  unimplemented. The auth-strategy extension checklist pointed at
  `internal/cli/auth`, no such package, instead of the `gum auth` tree in
  `cmd/gum/auth.go`. And `CONTRIBUTING.md` sent contributors to a
  `cmd/gen-catalog/reserved_namespaces.go` registry that never existed.
- Specification §13 describes the gates that exist. It prescribed a
  five-directory AST scan, a `// gum:registration-helper` marker convention, a
  repository-wide marker sweep and two error codes. None of that existed: the
  real scan reads one package and matches `*sdkmcp.Tool` literals.
- Citations name what they cite. 71 comments and messages across 56 files
  pointed at a `docs/test-matrix.md` row by ordinal, and a sweep found 65 of
  them resolving to an unrelated requirement, one naming a row past the end of
  the table. Eleven citations quoted a section label no normative document
  contained.
- Six error codes that appear nowhere in the tree say so:
  `PLUGIN_RISK_CLASS_MISSING`, `PLUGIN_RISK_CLASS_MISMATCH`,
  `CATALOG_SCHEMA_REF_INVALID`, `SERVICE_ROOT_TEMPLATE_INVALID`,
  `PLUGIN_SANDBOX_UNSUPPORTED` and the §10.0 Rule 4
  `TIMEZONE_SENSITIVE_CONFLICT` warning.
- Remote usage-telemetry export is marked not built. §12.3 carried normative
  requirements for an `https://`-only endpoint check at startup and a
  2-second push timeout. No code reads `GUM_USAGE_SOCKET`,
  `GUM_USAGE_ENDPOINT` or `GUM_USAGE_AUTH_HEADER`. The wire format and
  transport rules stay normative for the export that lands.
- The specification header no longer carries a hand-maintained date. It read
  `2026-05-22` while 97 later commits had changed the file.

## Security

- Prompt-injection layer 1 is built. The first text content block of a
  dispatched op response is wrapped in `<external_data trusted="false">`
  markers at the MCP presentation boundary, past the expression pipeline, the
  output profiles, the field masks, the TOON encoder, the outputSchema check
  and the gain ledger, all six of which measure the bytes of the shaped body.
  An `external_data` tag inside the payload is replaced with `[redacted]`
  first, in any case and with any attributes, so a calendar event titled
  `</external_data>` cannot end the fence. The shaping notice, the recovery
  `resource_link` and `structuredContent` stay outside the fence. CLI output
  is a byte stream for a shell pipeline and stays unfenced.
- Prompt-injection layer 2 is built, over the error envelope. Upstream error
  text is attacker-influenced and is not the answer the caller asked for: a
  Google 400 echoes the argument that failed validation, so third-party
  calendar titles and file names reached the model inside `message`.
  `sanitize.Scrub` replaces role markers, instruction-override phrasings and
  role-reassignment phrasings with `[redacted]`, and `ScrubJSON` walks nested
  detail values. Success bodies are not scrubbed: deleting a phrase from the
  mail or document the caller asked to read would corrupt the answer rather
  than defend it.
- Catalog and plugin descriptions pass the sanitizer. The build-time gate runs
  the rules over every op in the generated catalog from
  `validateGeneratedCatalog`, the one choke point all fourteen generator paths
  reach before writing a snapshot. `plugins.LoadManifest` runs them over every
  `advertised_tools[].description`, so a manifest edited in place after
  install is rejected at the next spawn. Before this, every service family
  except Gmail and Calendar reached `catalog.json` unchecked.
- Six further hardening rules are enforced: NFKD normalization,
  pseudo-instruction tags, injection directives, a credential-beside-a-path
  check, base64 payload detection and a codepoint cap. A joined-field re-scan
  covers rules 2, 6, 9 and 10, because a title ending "designed for" and a
  summary opening "AI agents" join into a hint that neither field carries
  alone.
- `risk_override_reason` is validated. A reason carrying a control code, a
  zero-width character, a bidirectional control character, or `<` or `>` fails
  with `RISK_OVERRIDE_REASON_INVALID`, and the sanitizer then runs on the
  validated value. Neither check existed, and the field reached
  `catalog.json` and the audit log unvalidated.
- A plugin cannot claim a Google first-party prefix. A test pins that plugin
  op ids and variant ids stay under `plug.`, that a dotted `plugin_id` is
  rejected, and that the embedded catalog ships no op under `plug.`. The
  specification and three guides had pointed at a reserved-namespace file
  that never existed.

`govulncheck` is a blocking release gate.

## Known limitations

- 217 of 228 catalog variants declare no `default_fields`. Specification §5.6
  curates the table by hand and it covers 11 high-traffic read operations, so
  every other variant sends no upstream field mask unless the call passes
  `--fields`, and its response comes back full size.
- Layer 1 fences the first text content block of an op response. It is a
  marker for the model, not a parser boundary, and a model may still follow
  instructions inside the fence.
- The manifest `canary` string, its relative date specifiers and its
  build-time gates are unimplemented, as `docs/plugin-contract.md` records.
  The shipped canary is a spawn probe. Verify with `rg CANARY_DATE_TOO_SOON`,
  which matches no Go file.
- `example_args` is curated on three operations. Every other operation's
  example comes from required fields alone, so an optional argument that
  changes what an answer means can still be missing from it.
- The ETag store has no TTL and no eviction. It grows until `gum cache clear`
  runs. Check its size with `gum cache stats`.
- A second gum process on the same profile cannot open the ETag store while
  the first holds it, for example a `gum call` during a long-lived MCP
  session. The second process waits 250 ms, then dispatches without
  revalidation and fetches the full body. `gum cache stats` reports
  `entries: 0` for the locked store in that case.
- macOS binaries carry the Go linker's ad-hoc signature, not a Developer ID
  signature, and are not notarized. Measured on the published v2.2.0
  `darwin/arm64` artifact under Darwin 25.6.0 and not re-measured since:
  `codesign -dvvv` reports `flags=0x20002(adhoc,linker-signed)` and
  `codesign -v` exits 0, `curl` sets no `com.apple.quarantine` attribute, and
  a copy with quarantine forced on still runs from a shell.
  `spctl --assess --type execute` reports `rejected`, and no install path gum
  publishes consults that verdict. Older macOS versions were not tested.
- The `v2.0.0` tag tree still fails `make fmt-check`. A published tag is not
  moved, because its binaries, checksums and provenance all name that commit.

## Token savings

Measured with the release fixtures using a local build, run from the
`apps/gum` directory of the matching source checkout:

```sh
gum gain --fixture-replay --format=toon
gum gain --fixture-replay --format=json
```

| Default format | Total calls | Total tokens in | Total tokens saved | Aggregate savings |
| --- | ---: | ---: | ---: | ---: |
| `toon` | 10 | 3,922 | 210 | 5.35 % |
| `json` | 10 | 3,922 | -12 | 0.31 % overhead |

Both rows are unchanged from v2.2.1. The fixture replay measures the shaping
pipeline, and layer 1 wraps the shaped body after every stage that counts its
bytes.

## Reproducibility

```sh
git clone https://github.com/ehmo/gum.git
cd gum && git checkout v2.3.0
cd apps/gum
GOTOOLCHAIN=go1.26.7 CGO_ENABLED=0 GOOS=<os> GOARCH=<arch> go build -trimpath \
  -ldflags='-s -w -X main.version=2.3.0' ./cmd/gum
sha256sum gum
```

Build from a full clone, not from a linked `git worktree`. Go embeds the commit
revision in the binary, and it silently skips that stamp in a linked worktree,
which changes the hash.

Write the rebuilt binary outside the clone. Go stamps `vcs.modified=true` when
the working tree holds any untracked file, so a `-o` path inside the checkout
changes the hash of every build after the first.

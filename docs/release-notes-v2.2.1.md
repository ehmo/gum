---
title: gum v2.2.1 release notes
date: 2026-09-22
status: release
---

# gum v2.2.1

This patch fixes the Keyword Planner guidance that let a caller ask for United
States figures and get worldwide ones, clears the thirteen lint findings that
kept the `test` workflow red on every run since v2.1.0, regenerates the catalog
from upstream, and drops macOS notarization from the release pipeline.

## Highlights

- Keyword Planner operations say what happens when you omit geo or language
  targeting. The old wording, "Omit for all locations.", reads like a default;
  the figures it produces cover every location and nothing in the response
  marks them. The three `keywordPlanIdeas` operations now carry a worked
  targeting example as well.
- `gum describe` can show an optional argument in its example. A catalog
  operation may curate `example_args`, which overlays the synthesizer's
  required-field output.
- `gum describe googleads.keywordPlanIdeas.generateKeywordIdeas` returns a
  runnable example. It previously had no seed at all, because the operation
  takes `keywords` and/or `url` and neither is a required field.
- The `test` workflow is green again. It failed on every run since v2.1.0, so
  v2.1.0 and v2.2.0 both shipped with that gate red.
- The release pipeline no longer signs or notarizes macOS binaries, and the
  v2.2.0 note telling readers to clear quarantine by hand is corrected.

## Install

```bash
curl -fsSL https://raw.githubusercontent.com/ehmo/gum/main/install.sh | GUM_VERSION=v2.2.1 bash
gum --version
gum doctor
```

## Upgrade notes

None. No configuration, credential, or stored state changes.

If you ran a Keyword Planner operation without `geoTargetConstants` or
`language`, the figures you received cover every location and every language.
Re-run with targeting to get country and language figures; they cannot be
rescaled afterwards.

```shell
gum read googleads.keywordPlanIdeas.generateKeywordHistoricalMetrics \
  --args '{"keywords":["horse breeds"],"geoTargetConstants":["2840"],"language":"1000"}'
```

A per-profile default removes the need to pass them on every call:

```shell
gum config set googleads.geo_target_constants=2840
gum config set googleads.language=1000
```

## Added

- `example_args` on a catalog operation. `gum describe` synthesizes its example
  from required fields, so an optional field whose omission changes what the
  answer means could never appear there. A curated `example_args` map is
  applied as the last overlay, which lets it both add optional arguments and
  correct a synthesized placeholder. The field is additive and optional under
  catalog ABI rule 4: operations without it produce the same example as before,
  and older binaries ignore it.

## Changed

- Catalog regenerated from the upstream Google discovery documents. 228
  operations before and after: none added, none removed. Google reworded the
  `chat.spaces.messages.create` `requestId` description from "A unique request
  ID for this message" to "A unique ID for this request. A random UUID is
  recommended.", and the derived request schema carries the same wording. No
  scope, risk class, auth strategy, or required argument changed, so no
  existing call behaves differently.
- The release pipeline no longer signs or notarizes macOS binaries. The
  `notarize` block is gone from `apps/gum/.goreleaser.yaml` and the five
  `MACOS_*` secrets are gone from the `goreleaser` job. gum ships plain CLI
  binaries, not app bundles, and no install path it publishes needs a
  Developer ID signature. See Known limitations for what was measured.

## Fixed

- Keyword Planner geo and language targeting. `geoTargetConstants` and
  `language` are optional, and a call that omits either returns figures
  covering every location or every language with nothing in the response
  marking them. The only warning was in `docs/auth-guides/google-ads.md`, which
  an MCP caller never reads, and `gum describe` made it worse: it synthesizes
  the example from required fields, so the canonical example for all three
  `keywordPlanIdeas` operations showed `customerId` and a seed and left the
  targeting out. Both field descriptions now name the consequence and the
  per-profile config key, and all three examples carry
  `geoTargetConstants: ["2840"]` and `language: "1000"`, the United States and
  English ids used in the worked example in `docs/auth-guides/google-ads.md`.
- `gum describe googleads.keywordPlanIdeas.generateKeywordIdeas` returns an
  example that runs. The operation takes `keywords` and/or `url`, so neither is
  marked required and the synthesizer emitted neither; a caller who pasted the
  example got a request with no seed.
- Thirteen lint findings, three from staticcheck and ten from golangci-lint.
  staticcheck runs first and short circuits the job, so CI never printed the
  golangci-lint half. bbolt v1.5.0 deprecated the top-level `ErrTimeout` alias;
  the cache-lock comparison moved to `go.etcd.io/bbolt/errors`, which holds the
  same value `bolt_unix.go` returns, so a second opener still gets
  `ErrCacheLocked` rather than `ErrCacheCorrupt`. That branch had no test and
  now has one. The rest are unchecked `fmt.Fprint` returns in
  `gain_render.go`, a double negative in the MCP elicitation capability check,
  and findings in two test files. No runtime behaviour changed.

## Security

No security fixes in this release. `govulncheck` is a blocking release gate.
It reports 0 vulnerabilities in gum's own code and 0 in the packages gum
imports. One vulnerability sits in a module `go.mod` requires and no gum code
path calls.

## Known limitations

- 217 of 228 catalog variants declare no `default_fields`. Spec §5.6 curates the
  table by hand and it covers 11 high-traffic read operations, so every other
  variant sends no upstream field mask unless the call passes `--fields`, and
  its response comes back full size.
- macOS binaries carry the Go linker's ad-hoc signature, not a Developer ID
  signature, and are not notarized. Measured on the published v2.2.0
  `darwin/arm64` artifact under Darwin 25.6.0: `codesign -dvvv` reports
  `flags=0x20002(adhoc,linker-signed)` and `codesign -v` exits 0, `curl` sets
  no `com.apple.quarantine` attribute, and a copy with quarantine forced on
  still runs from a shell. `spctl --assess --type execute` reports `rejected`,
  and no install path gum publishes consults that verdict. Notarization matters
  for a cask, a `.pkg`, or an app bundle, none of which gum ships. Older macOS
  versions were not tested.
- The ETag store has no TTL and no eviction. It grows until `gum cache clear`
  runs. Check its size with `gum cache stats`.
- A second gum process on the same profile cannot open the ETag store while the
  first holds it, for example a `gum call` during a long-lived MCP session. The
  second process waits 250 ms, then dispatches without revalidation and fetches
  the full body. `gum cache stats` reports `entries: 0` for the locked store in
  that case.
- `example_args` is curated on three operations. Every other operation's
  example still comes from required fields alone, so an optional argument that
  changes what an answer means can still be missing from it.
- The `v2.0.0` tag tree still fails `make fmt-check`. A published tag is not
  moved, because its binaries, checksums and provenance all name that commit.

## Token savings

Measured with the release fixtures using a local build stamped 2.2.1, run from
the `apps/gum` directory of the matching source checkout:

```sh
gum gain --fixture-replay --format=toon
gum gain --fixture-replay --format=json
```

| Default format | Total calls | Total tokens in | Total tokens saved | Aggregate savings |
| --- | ---: | ---: | ---: | ---: |
| `toon` | 10 | 3,922 | 210 | 5.35 % |
| `json` | 10 | 3,922 | -12 | 0.31 % overhead |

Both rows are unchanged from v2.2.0. This release changes no shaping code, and
the fixture replay measures the shaping pipeline.

## Reproducibility

```sh
git clone https://github.com/ehmo/gum.git
cd gum && git checkout v2.2.1
cd apps/gum
GOTOOLCHAIN=go1.26.7 CGO_ENABLED=0 GOOS=<os> GOARCH=<arch> go build -trimpath \
  -ldflags='-s -w -X main.version=2.2.1' ./cmd/gum
sha256sum gum
```

Build from a full clone, not from a linked `git worktree`. Go embeds the commit
revision in the binary, and it silently skips that stamp in a linked worktree,
which changes the hash.

Write the rebuilt binary outside the clone. Go stamps `vcs.modified=true` when
the working tree holds any untracked file, so a `-o` path inside the checkout
changes the hash of every build after the first.

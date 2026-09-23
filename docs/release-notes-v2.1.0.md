---
title: gum v2.1.0 release notes
date: 2026-09-21
status: release
---

# gum v2.1.0

This release fixes a wire bug that made every gum tool unavailable in strict
MCP clients, implements the spec §8.7 remote plugin package sources, puts the
spec §9.0 TOON document on the MCP response path, and makes installed plugin
operations reachable from every surface.

## Highlights

- `tools/list` no longer emits `"outputSchema": null`. Strict MCP clients
  reject `null` there, so in those clients v2.0.x exposed zero working tools.
  The field is now omitted when a tool has no output schema.
- Plugin manifests may declare `pypi`, `github_release`, and `git` package
  sources. Every remote source is pinned: a SHA-256 checksum selects the
  artifact, git sources pin a 40-hex commit, and unpinned or local installs
  are recorded as `dev-untrusted` in `plugins.lock`.
- MCP responses carry the spec §9.0 TOON wire document, and `gum gain`
  fixture replay measures it: 210 tokens saved (5.35%) across the release
  fixtures, up from 0 in v2.0.x.
- Installed plugin variants merge into the session catalog at process start,
  with per-row validation, so search, describe, and dispatch reach plugin
  operations. In v2.0.x nothing read the plugin catalog back.
- A profile now binds to one Google account. A login that returns a different
  account, or a stored credential whose subject changes, is refused with
  `AUTH_SUBJECT_MISMATCH`. `gum auth login --switch-account` rebinds.
- `gum.code` enforces one cumulative output budget, `gum_parallel` enforces
  the §9.0.1 aggregate ceilings before dispatch, and capability-bearing
  scripts require CLI confirmation with capability-bound tokens.

## Install

```bash
curl -fsSL https://raw.githubusercontent.com/ehmo/gum/main/install.sh | GUM_VERSION=v2.1.0 bash
gum --version
gum doctor
```

## Upgrade notes

- Users of strict MCP clients regain all 29 tools with no action beyond
  upgrading.
- Each auth profile now records the Google account it was created with, and
  a login through that profile which comes back as a different account is
  refused before anything is stored. To rebind a profile on purpose, run
  `gum auth login --switch-account`. Dispatch refuses a resolved credential
  whose subject fingerprint differs from the recorded one with the same
  `AUTH_SUBJECT_MISMATCH` error and writes a `credential_subject_changed`
  audit event.
- The dead `gum code --timeout-sec` flag is gone. The §6 wall-clock budget
  was already the only timeout; a script that passed the flag must drop it.
- `--format raw` prints a notice naming the fields raw output drops. Pipelines
  that parse stderr should ignore it or switch to `--format json`.

## Added

- Remote plugin package sources (spec §8.7). A manifest `[package]` block
  declares `source = "pypi" | "github_release" | "git" | "local" | "bundled"`.
  PyPI installs select the index artifact matching the declared `sha256:`
  checksum and build a host-managed virtualenv with
  `pip install --no-index --no-deps`; `uvx` and `pipx` are never spawned.
  GitHub release artifacts verify SHA-256 before a mode-pinned unpack that
  refuses symlinks, traversal, and non-regular entries. Git sources check out
  a pinned 40-hex commit and re-verify `HEAD` against the pin. `plugins.lock`
  rows record `source`, `ref`, and `checksum`; `local` and unpinned-git rows
  carry `risk="dev-untrusted"`, which `gum plugin list` surfaces.
- Spec §9.0 TOON documents on the MCP response path, and `gum gain
  --fixture-replay` measures the real wire document.
- `gum gain` text and CSV renderers, gain modes, savings computed from the
  ledger, and gain entries for failed dispatches.
- Active plugin variants merge into the session catalog through one seam both
  the CLI and the MCP server use. A row missing its op, variant, owner,
  binding, adapter key, or tool name is refused, as is a binding that names
  another plugin's executable. On an op collision the generated catalog wins.
- `gum plugin list --format=json`.
- Curated `default_fields` for 11 high-traffic read operations, so the
  stage-1 field mask now has real data to inject.
- `field_mask_mode="dual_fetch"` issues the second, unmasked request and
  reports both sides instead of being refused.
- Compound plugins receive a short-lived host Google access token under the
  spec §7 forwarding rule.
- A build-time first-party request schema store.
- The filesystem tee is wired into the shipped binary.
- `gum code` requires CLI confirmation for capability-bearing scripts, and a
  confirmation token binds the script's capability flags.
- `gum auth login --switch-account`.

## Changed

- Each profile binds to one Google account (see Upgrade notes).
- Long-running operations are gated out of code mode.
- `gum.code` enforces one cumulative output budget across prints and the
  return value; `gum_parallel` enforces the §9.0.1 aggregate ceilings before
  the first dispatch.
- JSON resource bodies are canonicalized per RFC 8785.
- `plugin-catalog.json` is stamped with the install generation, so a torn
  registry publish is detected instead of served.
- `gen-catalog` refuses overrides without a manifest entry and enforces
  `DEFAULT_VARIANT_INVALID` at generation time; dispatch falls through a
  quarantined default variant.
- grpc-sdk operations carry the `routing_headers` invariant.

## Fixed

- `tools/list` emitted `"outputSchema": null` for every tool (see
  Highlights). A wire-level regression test now decodes the raw frame and
  fails if any tool puts a non-object `outputSchema` on the wire.
- Gain-ledger rotation no longer loses entries.
- An unparseable `meta_tools.search_apis.collapse_arrays.max_items` value is
  ignored instead of failing the request.
- `gum://status/canaries` reports the real canary roster.
- Compound-auth failures report the real missing components, and Google
  policy refusals map onto `missing_components`.
- `gum://status/health` bounds every `detail` string at 80 characters.

## Removed

- The dead `gum code --timeout-sec` flag.
- Two auth strategies the wire enum omits; they were undispatchable.
- The dead output-profile column from the convenience-tool table.

## Security

- Remote plugin installs are checksum-pinned end to end; see Added. There is
  no development escape hatch around the checksum gate.
- One dependency was added: `github.com/pelletier/go-toml/v2 v2.4.3`. Only
  the build-time `gen-catalog` tool imports it; the shipped `gum` binary does
  not link it.
- `govulncheck` reports the same result as v2.0.1: 0 vulnerabilities in code
  this project calls, and one finding, GO-2026-5932 in module
  `golang.org/x/crypto`, which has no fixed version and no called symbol.

## Known limitations

- 217 of 228 catalog variants declare no `default_fields`. Spec §5.6 curates the
  table by hand and it covers 11 high-traffic read operations, so every other
  variant sends no upstream field mask unless the call passes `--fields`, and
  its response comes back full size.
- No catalog variant declares a non-`full` `execution_support`, so
  `unsupported_capabilities` and `"partial"` are declared and tested but not
  exercised by shipped data.
- The §10.2 HTTP and ETag cache backend has no production reader or writer.
  `http.db` and `http-wal.db` are created and never used.
- macOS binaries carry the Go linker's ad-hoc signature, not a Developer ID
  signature, and are not notarized. Checked on this release's `darwin/arm64`
  artifact: `codesign -v` exits 0, which meets the Apple Silicon requirement
  that every executable carry a signature. `curl` sets no
  `com.apple.quarantine` attribute, so neither `install.sh` nor a Homebrew
  download reaches Gatekeeper, and a quarantined copy still runs from a shell
  (checked under Darwin 25.6.0). `spctl --assess --type execute` reports
  `rejected`, and no install path gum publishes consults that verdict. An
  earlier version of this note told readers to clear quarantine by hand. That
  advice was wrong.
- The `v2.0.0` tag tree still fails `make fmt-check`. A published tag is not
  moved, because its binaries, checksums and provenance all name that commit.

## Token savings

Measured with the release fixtures using a local build stamped 2.1.0, run from
the `apps/gum` directory of the matching source checkout:

```sh
gum gain --fixture-replay --format=toon
gum gain --fixture-replay --format=json
```

| Default format | Total calls | Total tokens in | Total tokens saved | Aggregate savings |
| --- | ---: | ---: | ---: | ---: |
| `toon` | 10 | 3,922 | 210 | 5.35 % |
| `json` | 10 | 3,922 | -12 | 0.31 % overhead |

The `toon` row rose from 0 in v2.0.x because the replay now measures the §9.0
wire document instead of a shaping stage that preceded it.

## Reproducibility

```sh
git clone https://github.com/ehmo/gum.git
cd gum && git checkout v2.1.0
cd apps/gum
GOTOOLCHAIN=go1.26.7 CGO_ENABLED=0 GOOS=<os> GOARCH=<arch> go build -trimpath \
  -ldflags='-s -w -X main.version=2.1.0' ./cmd/gum
sha256sum gum
```

Build from a full clone, not from a linked `git worktree`. Go embeds the commit
revision in the binary, and it silently skips that stamp in a linked worktree,
which changes the hash.

## Verification

Release run
[35688916890](https://github.com/ehmo/gum/actions/runs/35688916890), tag
`v2.1.0` at public commit `48bdc8bb086cde31a1de548dc3255a475295db73`. All seven
jobs passed: validate semver tag, docs deployed from tag commit, pre-release
tests, govulncheck, goreleaser, reproducible-build canary, verify release
provenance against release artifacts.

Checked independently of the pipeline, against the published artifacts:

- `shasum -a 256 -c checksums.txt` reported OK for all four archives.
- The provenance attestation matched: identity and archive hashes for `v2.1.0`
  at commit `48bdc8bb086cde31a1de548dc3255a475295db73`.
- A clean `git clone --depth 1 --branch v2.1.0` rebuilt all four binaries to the
  hashes below, which equal `release-binaries.sha256`. The binaries extracted
  from the four published archives hash identically.

| Platform | Binary sha256 |
| --- | --- |
| `darwin/amd64` | `d68d6e23c79d9d12e4152a263fb50fa8430bcd9689160fc1a070db5480d3ff01` |
| `darwin/arm64` | `b74483d01d4666055f2235aeb877850d19b73a2dad3d25e9b3ae02a7898b9cb0` |
| `linux/amd64` | `09d88ce41278ef92af6148a4c469e80efb8b32c3044a665c8b22872d40833ac8` |
| `linux/arm64` | `190d7612d32d86eec47234ff8ddf61f9ca39435450ce3e4d6e2e71298fab6560` |

| Archive | sha256 |
| --- | --- |
| `gum_2.1.0_darwin_amd64.tar.gz` | `ed77b3f45dde7bdfbb84addf82d45b83521966b8180ea119197cb66748f1c870` |
| `gum_2.1.0_darwin_arm64.tar.gz` | `887e3ea381ad3aa1469ca491c60a397ef95d2a22bfcf95a35c95abcbfef4c7df` |
| `gum_2.1.0_linux_amd64.tar.gz` | `0aa33acd5c12812a599a141dfe420e524f1f0d1edd4b3777e96761825b8028cc` |
| `gum_2.1.0_linux_arm64.tar.gz` | `fb29f0256ebd01b085724ae8c4dda426abc74cfdf50489804549adf26d5dcada` |

The Homebrew tap carries the same four digests at `ehmo/homebrew-tap` commit
`28f9ad0`. `brew audit --strict --online --os=all --arch=all ehmo/tap/gum` and
`brew test ehmo/tap/gum` both passed, the tap-drift check passed, and the
installed binary reports `2.1.0` with `gum doctor: all checks passed`.

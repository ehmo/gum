---
title: gum v2.0.1 release notes
date: 2026-09-20
status: release
---

# gum v2.0.1

This tag carries no code change. It exists because the v2.0.0 tree failed the
project's own `gofmt` gate, and a formatting gate is only proven once a tag
ships a tree that passes it.

## Highlights

- The binary behaves exactly like v2.0.0. The only Go difference is column
  alignment inside one map literal in `cmd/gum/root.go`.
- `make fmt-check` resolves `gofmt` from the Go minor that CI pins instead of
  whatever is on `PATH`, so a maintainer on a newer Go cannot write files the
  gate rejects.
- `make fmt` is a new target and is the supported way to format this module.
- The release workflow runs `make fmt-check` before it publishes, so no later
  tag can ship a tree the `test` workflow rejects.
- The v2.0.0 release notes gained a Verification section and a corrected
  Reproducibility recipe.

## Install

```bash
curl -fsSL https://raw.githubusercontent.com/ehmo/gum/main/install.sh | GUM_VERSION=v2.0.1 bash
gum --version
gum doctor
```

## Upgrade notes

None. No config, catalog, wire-format, or behavior change. Upgrading from
v2.0.0 changes only the reported version string.

## Added

- `make fmt` formats `./cmd` and `./internal` with the pinned `gofmt`.
- The `pre-release-tests` job in `.github/workflows/release.yml` runs
  `make fmt-check`.

## Changed

- `make fmt-check` and `make fmt` resolve `gofmt` from `GOFMT_MINOR`, default
  `go1.26`, and fall back to fetching `GOFMT_TOOLCHAIN`, default `go1.26.7`,
  when the host runs a different minor. A host already on the pinned minor uses
  its own `gofmt` and downloads nothing.

## Fixed

- The v2.0.0 tree failed `make fmt-check` under the Go minor CI pins.
  `gofmt` changed its alignment rules between Go 1.26 and Go 1.27: 1.27 splits
  the adapter map literal in `cmd/gum/root.go` into two alignment groups where
  1.26 keeps one. The gate ran the host `gofmt`, so a 1.26 CI runner rejected
  what a 1.27 host had just written. The `test` workflow had failed on that one
  file since commit `e73ee74`, including on the commit that v2.0.0 tags.

## Security

No vulnerability fixes and no dependency change. `govulncheck` reports the same
result as v2.0.0: 0 vulnerabilities in code this project calls, and one finding,
GO-2026-5932 in module `golang.org/x/crypto`, which has no fixed version and no
called symbol.

## Known limitations

Unchanged from v2.0.0.

- No catalog variant declares `default_fields`. Spec §5.6 curates the table by
  hand and it was still empty at this tag, so the stage-1 field mask had nothing
  to inject and `--fields` completion was empty.
- Nothing converts a plugin registry variant row into a dispatchable catalog
  variant, so `gum plugin install` runs no catalog validation.
- No catalog variant declares a non-`full` `execution_support`, so
  `unsupported_capabilities` and `"partial"` are declared and tested but not
  exercised by shipped data.
- `field_mask_mode="dual_fetch"` is refused rather than implemented.
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

Unchanged from v2.0.0. This release does not touch output shaping. Measured
with the release fixtures using a local build stamped 2.0.1, run from the
`apps/gum` directory of the matching source checkout:

```sh
gum gain --fixture-replay --format=toon
gum gain --fixture-replay --format=json
```

| Default format | Total calls | Total tokens in | Total tokens saved | Aggregate savings |
| --- | ---: | ---: | ---: | ---: |
| `toon` | 10 | 3,922 | 0 | 0 % |
| `json` | 10 | 3,922 | -12 | 0.31 % overhead |

## Verification

All seven jobs in the [v2.0.1 release workflow](https://github.com/ehmo/gum/actions/runs/35530321049)
passed: tag validation, live docs match, tests, `govulncheck`, the GoReleaser
build, the independent four-platform rebuild, and the provenance comparison.
The `pre-release-tests` job ran the new `gofmt` step and it passed, which is the
check the v2.0.0 tree failed.

The four downloaded archives matched `checksums.txt`, and each one matched its
subject digest in `gum-v2.0.1.intoto.jsonl` at commit
`2557cfe4ac6b5bf98b83b83813f80991223adaca`. The binary extracted from each of
the four archives matched its entry in `release-binaries.sha256`.

A local rebuild reproduced all four published binaries. The command in
Reproducibility below, run from a clean clone at tag `v2.0.1` on one darwin/arm64
host with `GOTOOLCHAIN=go1.26.7`, produced these hashes:

| Target | sha256 |
| --- | --- |
| darwin/amd64 | `791a418ba73f850b3685fd146fa34e2bf04cd22a993337328aa8390be7a15a47` |
| darwin/arm64 | `bf563a8d1041f92bdf8255acf856a377011cd53e55cb653cb985f4590c5cabc8` |
| linux/amd64 | `f0336ce18ff1b475d3c341a3804a1dd59f08f5b7301ec332a9a37bd7dc870f7c` |
| linux/arm64 | `c8a794e183b47d66f6fdd79bc3f9c53e9bc01414ee7e093c1312a644aa7b1907` |

Each hash matches the matching line in `release-binaries.sha256`.

The Homebrew installation reports 2.0.1 and `gum doctor` passed every check.
`brew audit --strict --online --os=all --arch=all ehmo/tap/gum` and
`brew test ehmo/tap/gum` both passed, and the `tap-drift` workflow confirms both
formulae point at this release.

## Reproducibility

```sh
git clone https://github.com/ehmo/gum.git
cd gum && git checkout v2.0.1
cd apps/gum
GOTOOLCHAIN=go1.26.7 CGO_ENABLED=0 GOOS=<os> GOARCH=<arch> go build -trimpath \
  -ldflags='-s -w -X main.version=2.0.1' ./cmd/gum
sha256sum gum
```

Build from a full clone, not from a linked `git worktree`. Go embeds the commit
revision in the binary, and it silently skips that stamp in a linked worktree,
which changes the hash.

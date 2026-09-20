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

- No catalog variant declares `default_fields`, so the §770 requirement that
  every variant carry them is still unmet and the stage-1 field mask has nothing
  to inject. `--fields` completion is therefore empty.
- Nothing converts a plugin registry variant row into a dispatchable catalog
  variant, so `gum plugin install` runs no catalog validation.
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

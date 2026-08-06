---
title: gum v1.0.2 release notes
date: 2026-08-06
status: release
---

# gum v1.0.2

gum is now MIT licensed, ships a public docs site, and fixes a set of defects
that let bad output and bad arguments pass silently.

## Highlights

- MIT license, replacing FSL-1.1-ALv2.
- Published docs site with a command reference generated from
  `gum schema --json`.
- Dropped fields from an expression profile are now reported instead of
  vanishing.
- A cache hit is shaped like a cold call, so a warm response cannot come back
  in a format you did not ask for.
- `gum plugin list` shows quarantined plugins, with retry count and last error.

## Install

```bash
curl -fsSL https://raw.githubusercontent.com/ehmo/gum/main/install.sh | GUM_VERSION=v1.0.2 bash
gum --version
gum doctor
```

## Upgrade notes

None. No config, catalog, or wire-format change.

## Added

- `gum schema --json` prints the CLI surface as machine-readable JSON. The docs
  generators consume it; agents can use it to discover commands and arguments.
- A docs site rendered from the `docs/` tree, including per-service pages.

## Changed

- gum is MIT licensed. Versions through v1.0.1 were FSL-1.1-ALv2.
- `gum plugin list` reads `plugin-state.json` and prints status, retry count,
  next retry, and last error for every install directory. It previously listed
  only plugins whose manifest loaded, so a quarantined plugin was invisible.
  The footer names `gum plugin reload` and `gum plugin unquarantine`.
- A quarantined plugin returns `VARIANT_QUARANTINED`. It used to return
  `SERVICE_DOWN`, which read as an upstream outage.
- CLI help and error text no longer cite internal `spec.md` sections; the
  public docs are the reference surface.

## Fixed

- An expression profile that dropped fields left no marker in the output, so a
  caller could not tell an absent field from one the upstream API never
  returned. The dropped paths are now named on stderr (CLI) or in their own
  text block (MCP), along with the recovery artifact when tee wrote one.
- A cache hit returned the stored body verbatim and skipped output shaping, so
  a warm call could answer in a format the caller never requested. Cached
  bodies now go through the same shaping as cold ones, a bad `--format` fails
  identically warm and cold, a non-JSON body falls back to verbatim with a
  `WARN`, and `raw`-format responses are no longer cached.
- An op declaring a request-level field default failed with a required-argument
  error. Catalog defaults are applied before validation.
- An `integer` argument accepted any number, so `destructive_budget=2.5` passed
  local validation and became an opaque upstream 400. Fractional values, NaN,
  and infinities are rejected; whole-valued JSON numbers still pass.
- `gum.code`'s `destructive_budget` was declared `int` while the type checker
  recognized only `integer`, so that argument had no type checking at all.
- `gum schema` panicked on a command whose usage line declared no arguments.
- A long-running-operation poll ignored a malformed `done` field. A finished
  operation was reported as still running, and the poller waited for a
  completion that had already happened.
- `gum config`, the update-notification cache, the canary registry, and
  `gum init` settings each wrote through a fixed `<path>.tmp` with no fsync.
  Two gum processes writing at once could rename each other's partial bytes
  into place, and a crash just after the rename could leave a zero-length file
  where a valid one used to be. All four now use one atomic-write helper with a
  unique temp name and an fsync.

## Security

No vulnerability fixes. The dependency graph was refreshed and `govulncheck`
reports 0 known vulnerabilities against it.

## Known limitations

- macOS release binaries are not notarized: the signing secrets are not
  provisioned, so goreleaser skips the notarize step. Check with
  `spctl --assess --type execute --verbose gum`. If it rejects the binary,
  clear the quarantine attribute: `xattr -d com.apple.quarantine gum`.
- There is no Homebrew cask. Install with `install.sh` or from the release
  archives.

## Token savings

Measured on the in-tree release fixtures before tagging. This release does not
change output shaping, so both rows match v1.0.1.

```bash
cd apps/gum
gum gain --fixture-replay --format=toon
gum gain --fixture-replay --format=json
```

| Default format | Total calls | Total tokens in | Total tokens saved | Aggregate savings |
| --- | ---: | ---: | ---: | ---: |
| `toon` | 10 | 3,922 | 0 | 0 % |
| `json` | 10 | 3,922 | -12 | 0.31 % overhead |

## Reproducibility

```bash
git checkout v1.0.2
cd apps/gum
CGO_ENABLED=0 go build -trimpath -ldflags='-s -w -X main.version=v1.0.2' ./cmd/gum
sha256sum gum
```

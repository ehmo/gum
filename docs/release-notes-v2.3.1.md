---
title: gum v2.3.1 release notes
date: 2026-09-23
status: release
---

# gum v2.3.1

This tag carries no change to the binary. It exists for two things the v2.3.0
tag does not hold: the expression profile DSL author reference, which the
release manifest did not export, and a vulnerability gate that fails on a class
of advisory the old gate could not fail on.

## Highlights

- `docs/profile-dsl-reference.md` is in a release tree. The page was written
  before v2.3.0, but `scripts/public-release-manifest.json` did not list it, so
  the v2.3.0 tag carries no copy and a checkout of that tag produces none. The
  docs site serves `main` and has published the page since it landed there.
- `scripts/check-govulncheck.py` replaces `govulncheck ./...` as the gate in
  the pull-request workflow and in the release workflow. `govulncheck` exits 0
  when an advisory covers only import paths the build never compiles, so such
  an advisory used to reach `main` without failing any gate.
- `scripts/govulncheck-allowlist.json` is the only way to accept one, and each
  entry names the advisory id, the module, the reason the requirement stays and
  the date. A finding the build can reach has no allowlist path at all, and an
  entry for its advisory does not create one.
- `make vulncheck` runs the same gate from a checkout.

## Install

```bash
curl -fsSL https://raw.githubusercontent.com/ehmo/gum/main/install.sh | GUM_VERSION=v2.3.1 bash
gum --version
gum doctor
```

## Upgrade notes

None. No configuration, credential, catalog, wire-format or behavior change.
No non-test Go file differs from v2.3.0. Upgrading changes the reported version
string and nothing else.

## Added

- `scripts/check-govulncheck.py`, run by `.github/workflows/govulncheck.yml` on
  every pull request and by the `govulncheck` job in
  `.github/workflows/release.yml` on every tag. It reads the scanner's JSON and
  classifies each finding by its most precise trace frame.
- `scripts/govulncheck-allowlist.json`, which records module-level advisories.
  Each entry requires `id`, `module`, `reason` and `recorded`.
- `make vulncheck`.
- `TestGovulncheckAllowlistShape` in `internal/testmatrix`, which holds every
  allowlist entry to a well-formed advisory id, a module, a reason long enough
  for a reviewer to check, and an ISO date, and rejects duplicate ids.

## Changed

- `docs/profile-dsl-reference.md` is exported. It is listed in the release
  manifest and in the Reference section of the docs-site navigation.
- Both `govulncheck` jobs invoke the gate script instead of the scanner
  directly. The scanner stays pinned at `golang.org/x/vuln/cmd/govulncheck@v1.3.0`
  and the scan still covers `./...` at symbol granularity.
- `SECURITY.md` and `docs/security.md` give `make vulncheck` as the local
  recipe and state the three ways the gate fails.

## Fixed

- A module-level advisory can no longer land unnoticed. `govulncheck` prints
  those under "vulnerabilities in modules you require" and exits 0, so the
  previous gate passed whatever appeared there. GO-2026-5932 arrived that way.
- A recorded advisory the scan no longer reports fails the gate, so the
  allowlist cannot accumulate entries for advisories that are gone.

## Security

No vulnerability fix and no dependency change. The gate reports 0 findings the
build can reach and one module-level finding, GO-2026-5932 in
`golang.org/x/crypto`, which the allowlist records.

That advisory cannot be cleared by a version bump. It says
`golang.org/x/crypto/openpgp` and its six subpackages are unmaintained, and its
affected range opens at `0` and carries no `fixed` event, so every release of
the module is in range and no later release leaves it. gum imports none of
those packages. The module stays in the graph for
`golang.org/x/crypto/cryptobyte`, which `github.com/google/s2a-go` reaches under
`cloud.google.com/go/auth`, so the requirement cannot be dropped either. The
allowlist entry records that reasoning next to the id.

## Known limitations

Unchanged from the [v2.3.0 release notes](release-notes-v2.3.0.md), with one
addition.

- The allowlist holds one permanent entry. GO-2026-5932 has no fixed version,
  so the entry cannot expire and a reviewer reading the gate's output sees a
  recorded advisory on every clean run.
- The `v2.3.0` tag tree carries no `docs/profile-dsl-reference.md`, and the
  `v2.0.0` tag tree still fails `make fmt-check`. A published tag is not moved,
  because its binaries, checksums and provenance all name that commit.

## Token savings

Unchanged from v2.3.0. This release does not touch output shaping. Measured
with the release fixtures using a local build, run from the `apps/gum`
directory of the matching source checkout:

```sh
gum gain --fixture-replay --format=toon
gum gain --fixture-replay --format=json
```

| Default format | Total calls | Total tokens in | Total tokens saved | Aggregate savings |
| --- | ---: | ---: | ---: | ---: |
| `toon` | 10 | 3,922 | 210 | 5.35 % |
| `json` | 10 | 3,922 | -12 | 0.31 % overhead |

## Reproducibility

```sh
git clone https://github.com/ehmo/gum.git
cd gum && git checkout v2.3.1
cd apps/gum
GOTOOLCHAIN=go1.26.7 CGO_ENABLED=0 GOOS=<os> GOARCH=<arch> go build -trimpath \
  -ldflags='-s -w -X main.version=2.3.1' ./cmd/gum
sha256sum gum
```

Build from a full clone, not from a linked `git worktree`. Go embeds the commit
revision in the binary, and it silently skips that stamp in a linked worktree,
which changes the hash.

Write the rebuilt binary outside the clone. Go stamps `vcs.modified=true` when
the working tree holds any untracked file, so a `-o` path inside the checkout
changes the hash of every build after the first.

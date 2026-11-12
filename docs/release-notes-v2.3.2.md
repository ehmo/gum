---
title: gum v2.3.2 release notes
date: 2026-11-12
status: not-published
---

# gum v2.3.2

This tag was not published as a GitHub release. CI selected cached Go 1.26.10
and the vulnerability gate rejected GO-2026-6859. The release attempt was
cancelled before any binaries were published. Install [v2.3.3](release-notes-v2.3.3.md)
for the changes below and the corrected toolchain selection.

Keychain timeouts keep their retryable error, and Keyword Planner and Search
Console now have direct guides and searchable documentation.

## Highlights

- A timed-out granted-scope read returns retryable `AUTH_KEYCHAIN_UNAVAILABLE`.
  Earlier releases discarded the read error and returned non-retryable
  `SCOPE_MISSING`, even when the account had the required grant.
- The next scoped invocation reads the grant again. A running MCP server can
  recover after a temporary Keychain timeout without a restart. Operations
  that declare no scopes skip this lookup.
- [Keyword Planner](keyword-planner.md) and [Search Console](search-console.md)
  have dedicated guides, linked from the homepage, sidebar, service directory,
  and related reference pages.
- Docs search covers page text, headings, and operation IDs. Results link to
  matching sections and show excerpts. The previous search filtered only
  sidebar page titles.

## Install

```bash
curl -fsSL https://raw.githubusercontent.com/ehmo/gum/main/install.sh | GUM_VERSION=v2.3.2 bash
gum --version
gum doctor
```

## Upgrade notes

No credential or configuration migration. If a scope read times out, unlock
the keychain or retry when the host is less busy. `GUM_KEYRING_TIMEOUT` still
cannot raise the two-second best-effort cap. A readable grant that lacks a
required scope still returns `SCOPE_MISSING`.

## Added

- Task guides with setup steps and examples for keyword ideas, search volume,
  forecasts, organic search performance, URL inspection, and sitemaps.
- Content search with section links, excerpts, result counts, and messages for
  no results or a failed index download.

## Changed

- Scope-read failures reach dispatch, plugin token forwarding, diagnostics,
  and post-consent verification instead of being treated as empty grants.
- The docs homepage and repository README link directly to common tasks.
- Public test, build-matrix, fuzz, and standalone vulnerability workflows use
  GitHub-hosted runners. Release jobs retain their existing builder.

## Fixed

- Keychain timeouts reported as `SCOPE_MISSING` (gum-tz5n).
- A stdio framing test could read a partial frame under load. Its wait now
  requires the complete newline-terminated frame; this changes tests only.

## Security

Scope-read failures still block scoped operations. No scope bypass, credential
storage change or new OAuth client is included.

Release builds use Go 1.26.11. `golang.org/x/net` moves to v0.62.0 and
`golang.org/x/crypto` to v0.59.0, with their required Go submodule updates.
These updates clear the release vulnerability gate. The old module-only
GO-2026-5932 allowlist entry is removed because the advisory no longer applies.
See the [Go security release history](https://go.dev/doc/devel/release) and
[GO-2026-5932](https://pkg.go.dev/vuln/GO-2026-5932).

## Known limitations

Google product access requirements still apply after OAuth. Check the
[Google Ads setup](auth-guides/google-ads.md) for developer-token and account
requirements, or [Search Console setup](auth-guides/search-console.md) for
property access.

## Token savings

Measured with this release's local build using fixture replay:

```bash
gum gain --fixture-replay --format=toon
gum gain --fixture-replay --format=json
```

| Default format | Total calls | Total tokens in | Total tokens saved | Aggregate savings |
| --- | ---: | ---: | ---: | ---: |
| `toon` | 10 | 3,922 | 210 | 5.35% |
| `json` | 10 | 3,922 | -12 | 0.31% overhead |

These are deterministic fixture measurements, not live API traffic.

## Reproducibility

Use the release workflow's Go toolchain version and a clean checkout:

```bash
git checkout v2.3.2
cd apps/gum
CGO_ENABLED=0 go build -trimpath -ldflags='-s -w -X main.version=2.3.2' -o gum ./cmd/gum
shasum -a 256 gum
```

Compare the executable hash with the matching platform entry in
`release-binaries.sha256`. The GitHub release body records the completed
publication and artifact checks.

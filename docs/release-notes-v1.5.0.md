---
title: gum v1.5.0 release notes
date: 2026-09-18
status: release
---

# gum v1.5.0

Keyword Planner calls can carry a default location and language, and a batch
response now names which submitted keywords each merged result covers.

## Highlights

- Set `geoTargetConstants` and `language` once per profile with
  `gum config set`, or per shell with `GUM_GOOGLE_ADS_GEO_TARGET_CONSTANTS`
  and `GUM_GOOGLE_ADS_LANGUAGE`.
- `generateKeywordHistoricalMetrics` adds `matchedInputs` and
  `unmatchedInputs`, so a caller can join results back to the keywords it
  submitted.
- `gum config set <key> <value>` fails with the accepted `key=value` form and
  a corrected example instead of an argument count.

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
curl -fsSL https://raw.githubusercontent.com/ehmo/gum/main/install.sh | GUM_VERSION=v1.5.0 bash
```

## Upgrade notes

None. Calls that pass `geoTargetConstants` and `language` behave as before, and
calls that pass neither still return worldwide figures for all languages. To
stop passing them, set the defaults in the profile that makes the calls:

```sh
gum config set googleads.geo_target_constants=2840
gum config set googleads.language=1000
```

See the
[Google Ads guide](auth-guides/google-ads.md#default-location-and-language).

## Added

- Defaults for `geoTargetConstants` and `language` on the three
  `googleads.keywordPlanIdeas` operations, which are the operations that
  declare the fields. A call argument wins over the environment, and the
  environment wins over the profile config. `"geoTargetConstants":[]` and
  `"language":""` ask one call for worldwide figures.
- The geo default takes a comma-separated list. Each id must be digits, alone
  or behind its own resource prefix, so `geoTargetConstants/2840` and `2840`
  both work and `languageConstants/1000` is rejected for the geo key. A
  malformed default fails the call with `INVALID_ARGS` and names the variable
  or config key that holds it.
- `matchedInputs` on a `generateKeywordHistoricalMetrics` result and
  `unmatchedInputs` at the top level. Google merges close variants of the
  submitted keywords into one result keyed by its own text. The report behind
  this change sent 245 keywords and got 243 results: three submitted keywords
  had no result with their text, and a caller joining on `text` read their
  33,100, 18,100, and 590 average monthly searches as zero.
  `matchedInputs` appears only when a result
  answers for more than one submitted keyword or for a keyword that differs
  from its `text`, so an ordinary batch pays nothing.

## Changed

- An adapter can add fields to an upstream response before the expression
  profile runs. Specification §9.1 sets the rule: an adapter may add fields,
  may not drop or rewrite an upstream field, and never runs for `--format raw`
  or for the recovery artifact.
- The resolved location and language are part of the canonical arguments. Two
  markets never share a cache entry or audit args hash.

## Fixed

- `gum config set googleads.geo_target_constants 2840` reported
  `accepts 1 arg(s), received 2`, which names neither the accepted form nor a
  way out of it. The error now reads
  `config set takes one key=value argument, got 2; try: gum config set googleads.geo_target_constants=2840`.
  It rewrites the caller's own tokens, quotes a value containing spaces, and
  repeats a non-default `--profile`.

## Security

- No dependency or authorization changes in this release. `govulncheck` reports
  0 vulnerabilities that gum's code calls and 0 in packages gum imports. One
  advisory remains in the module graph: GO-2026-5932 marks
  `golang.org/x/crypto/openpgp` unmaintained. gum imports no symbol from that
  package, and the advisory has no fixed version.

## Known limitations

- The targeting defaults cover the `keywordPlanIdeas` operations only.
  `googleads.googleAds.search` queries carry their own location and language
  criteria in GAQL.
- Keyword Planner still does not mark a worldwide result. With no default and
  no argument, the figures are worldwide and nothing in the response says so.
- `matchedInputs` matches on case, whitespace, and punctuation, plus Google's
  own `closeVariants` list. A submitted keyword that Google merged without
  reporting a close variant, and whose text differs by more than punctuation,
  is listed in `unmatchedInputs`.
- macOS binaries are not notarized. The Homebrew formula clears quarantine
  during installation. For standalone installs, inspect with
  `spctl --assess --type execute --verbose gum` and use
  `xattr -d com.apple.quarantine gum` if Gatekeeper rejects the binary.

## Token savings

Measured with the release fixtures using a local build stamped 1.5.0. Run from
the `apps/gum` directory of the matching source checkout:

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
git checkout v1.5.0
cd apps/gum
CGO_ENABLED=0 go build -trimpath -ldflags='-s -w -X main.version=1.5.0' ./cmd/gum
sha256sum gum
```

Match the release's Go toolchain. `go version <downloaded-binary>` prints the
version the published artifacts were built with; pass it as `GOTOOLCHAIN` to
the command above.

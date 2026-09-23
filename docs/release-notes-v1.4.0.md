---
title: gum v1.4.0 release notes
date: 2026-09-18
status: release
---

# gum v1.4.0

Google Ads calls can omit `customerId` and `loginCustomerId` when a profile or
environment sets a default account.

## Highlights

- Set a default Google Ads account once per profile with `gum config set`.
- Override it per shell with `GUM_GOOGLE_ADS_CUSTOMER_ID` and
  `GUM_GOOGLE_ADS_LOGIN_CUSTOMER_ID`.
- The `gum call` wizard skips account ids that a default supplies.
- A new Keyword Planner section explains why calls without
  `geoTargetConstants` and `language` return worldwide figures.
- gRPC v1.83.2 fixes an HTTP/2 memory exhaustion advisory on a call path in
  gum.

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
curl -fsSL https://raw.githubusercontent.com/ehmo/gum/main/install.sh | GUM_VERSION=v1.4.0 bash
```

## Upgrade notes

None. Calls that pass `customerId` behave as before. To stop passing it, set a
default in the profile that makes the calls:

```sh
gum config set googleads.customer_id=<customer-id>
gum config set googleads.login_customer_id=<manager-customer-id>
```

Set `loginCustomerId` only when you reach the account through a manager
account. To send one call without the manager default, pass
`"loginCustomerId":""`. See the
[Google Ads guide](auth-guides/google-ads.md#default-account).

## Added

- Default account values for every Google Ads operation that declares
  `customerId` or `loginCustomerId`. A call argument wins over the environment,
  and the environment wins over the profile config.
- Defaults accept 10 digits with optional dashes or spaces. A malformed value
  fails the call with `INVALID_ARGS` and names the variable or config key
  that holds it.
- The MCP server reads the profile config on every call, so a new default
  applies without a restart.
- Keyword Planner guidance with United States and English examples
  (`geoTargetConstants:["2840"]`, `language:"1000"`).

## Changed

- A call that omits `customerId` with no default now fails with a hint that
  names `gum config set googleads.customer_id` and
  `GUM_GOOGLE_ADS_CUSTOMER_ID`.
- The resolved account is part of the canonical arguments. Two accounts never
  share a cache entry, audit args hash, or confirmation binding.

## Fixed

- Specification rule 7 said request-field defaults were not injected. gum has
  injected them since v1.0.2. The rule now describes the shipped behavior.

## Security

- `google.golang.org/grpc` moves from v1.83.0 to v1.83.2. It fixes
  GO-2026-6348, an HTTP/2 memory exhaustion issue that `govulncheck` found on
  a call path in gum, and GO-2026-6441 and GO-2026-6443, which gum does not
  call.
- `golang.org/x/crypto` moves from v0.54.0 to v0.56.0. It fixes GO-2026-6303,
  GO-2026-6354, and GO-2026-6355, which gum does not call.
- `govulncheck` now reports 0 vulnerabilities that gum's code calls. One
  advisory remains in the module graph: GO-2026-5932 marks
  `golang.org/x/crypto/openpgp` unmaintained. gum imports no symbol from that
  package, and the advisory has no fixed version.

## Known limitations

- Defaults cover Google Ads `customerId` and `loginCustomerId` only. Data
  Manager calls still need `operatingAccount`, and `loginAccount` when access
  goes through a manager, in each request body.
- Keyword Planner does not mark a worldwide result. Check that the call passed
  `geoTargetConstants` and `language` before comparing figures across
  countries.
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

## Token savings

Measured with the release fixtures using a local build stamped 1.4.0. Run from
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

All seven jobs in the [v1.4.0 release workflow](https://github.com/ehmo/gum/actions/runs/35369340831)
passed, including tests, vulnerability checks, the independent four-platform
rebuild, and provenance checks. Downloaded archive checksums and extracted
binary hashes matched the published manifests. A local macOS ARM rebuild with
the command below matched the published binary hash.

The Homebrew installation reports 1.4.0 and reproduces the token savings
figures above. It completed a live Keyword Planner historical metrics call
that set the account only through `GUM_GOOGLE_ADS_CUSTOMER_ID` and
`GUM_GOOGLE_ADS_LOGIN_CUSTOMER_ID`, with no `customerId` argument. Homebrew's
formula audit and package tests passed.

## Reproducibility

```sh
git checkout v1.4.0
cd apps/gum
GOTOOLCHAIN=go1.26.7 CGO_ENABLED=0 go build -trimpath -ldflags='-s -w -X main.version=1.4.0' ./cmd/gum
sha256sum gum
```

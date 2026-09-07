---
title: gum v1.3.0 release notes
date: 2026-09-07
status: release
---

# gum v1.3.0

gum adds Data Manager API operations for offline conversions and request diagnostics.

## Highlights

- Send event batches through `datamanager.events.ingest` with validation-only support.
- Read per-destination processing results through `datamanager.requestStatus.retrieve`.
- Conflicting boolean body arguments fail before sending a request.
- REST POST and PATCH calls are not replayed after transport or response-read failures.
- Validation errors retain Google's request ID, field path, and reason.

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
curl -fsSL https://raw.githubusercontent.com/ehmo/gum/main/install.sh | GUM_VERSION=v1.3.0 bash
```

## Upgrade notes

Enable the Data Manager API in the Cloud project that owns your OAuth client,
then authorize its separate scope:

```sh
gum login --service datamanager
gum auth probe --scopes https://www.googleapis.com/auth/datamanager
```

These operations use BYO OAuth and do not require a Google Ads developer token.
See the [Google Ads and Data Manager guide](auth-guides/google-ads.md#data-manager)
for request fields and validation commands.

For `gum write` and MCP, put ingestion fields inside `body`. Set
`body.validateOnly:true` to check a batch without recording it. The `gum call`
convenience form can assemble flat body fields, but conflicting boolean values
between those fields and an explicit body now fail locally. Existing non-boolean
body precedence is unchanged.

## Added

- Data Manager API v1 event ingestion, with up to 2,000 events per request.
- Request-status retrieval by `requestId`, including per-destination errors
  and warnings.

## Fixed

- Explicit validation intent could be discarded when a flat boolean field
  conflicted with an explicit JSON body.
- A network failure or truncated success response could replay a POST or
  PATCH. These methods now return the failure after that attempt, including
  when it follows a rate-limit retry.
- Generic Google validation messages could omit the field and reason that
  identify a rejected input. Errors now include request IDs and up to five
  field violations, plus a count of additional violations.

## Known limitations

- Google does not provide request-status diagnostics for validation-only
  requests. For live uploads, wait 30 minutes before the first status check;
  processing can take up to 24 hours. Inspect every destination's result.
- An explicit HTTP 429 rejection permits one retry. Check request state before
  resubmitting an upload after an uncertain transport or server failure.
- Live asynchronous processing was not exercised; status routing and response
  preservation were tested with HTTP fixtures.
- macOS binaries are not notarized. The Homebrew formula clears quarantine
  during installation. For standalone installs, inspect with
  `spctl --assess --type execute --verbose gum` and use
  `xattr -d com.apple.quarantine gum` if Gatekeeper rejects the binary.

## Token savings

Measured with the release fixtures using the Homebrew-installed v1.3.0 binary.
Run from the `apps/gum` directory of the matching source checkout:

```sh
gum gain --fixture-replay --format=toon
gum gain --fixture-replay --format=json
```

| Default format | Total calls | Total tokens in | Total tokens saved | Aggregate savings |
| --- | ---: | ---: | ---: | ---: |
| `toon` | 10 | 3,922 | 0 | 0 % |
| `json` | 10 | 3,922 | -12 | 0.31 % overhead |

## Verification

All seven jobs in the [v1.3.0 release workflow](https://github.com/ehmo/gum/actions/runs/34077162315)
passed, including tests, vulnerability checks, the independent four-platform
rebuild, and provenance checks. Downloaded archive checksums and extracted
binary hashes matched the published manifests.

Both the downloaded macOS ARM release and the Homebrew installation completed
a live ingestion with `validateOnly:true` and returned a `requestId`. No
conversion was recorded. Homebrew's formula audit and package tests passed.

If an upgraded Homebrew executable times out during OAuth refresh, check its
[firewall approval](support.md#oauth-connection-timeouts) before replacing
credentials.

## Reproducibility

```sh
git checkout v1.3.0
cd apps/gum
GOTOOLCHAIN=go1.26.7 CGO_ENABLED=0 go build -trimpath -ldflags='-s -w -X main.version=1.3.0' ./cmd/gum
sha256sum gum
```

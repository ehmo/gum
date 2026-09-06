---
title: gum v1.2.0 release notes
date: 2026-09-06
status: release candidate
---

# gum v1.2.0

gum adds Google Ads reporting, batch mutations, and offline conversion uploads.

## Highlights

- Run GAQL queries with `googleads.googleAds.search`.
- Create, update, or remove resources with `googleads.googleAds.mutate`.
  Every batch requires destructive confirmation, including validation runs.
- Upload click conversions with
  `googleads.conversionUploads.uploadClickConversions` when the developer
  token is eligible for Google's offline upload service.
- Google Ads errors retain upstream details, request IDs, and field locations.

## Install

```bash
curl -fsSL https://raw.githubusercontent.com/ehmo/gum/main/install.sh | GUM_VERSION=v1.2.0 bash
gum --version
gum doctor
```

Or through the tap:

```bash
brew install ehmo/tap/gum
```

## Upgrade notes

Existing Google Ads credentials work with the new operations: BYO OAuth with
the `adwords` scope, a developer token, and access to the customer account.
See the [Google Ads setup guide](auth-guides/google-ads.md).

Use `gum destructive` for mutations and `gum write --allow-write` for
conversion uploads. Supply the declared arguments at the top level; the new
write operations reject `body`. Set `validateOnly:true` to validate a request
without applying it. Mutations still require confirmation in that mode.

## Added

- `googleads.googleAds.search` supports GAQL queries, page tokens, and
  summary-row settings through Google Ads API v24.
- `googleads.googleAds.mutate` accepts ordered `mutateOperations`, with
  `validateOnly`, `partialFailure`, and response-content options.
- `googleads.conversionUploads.uploadClickConversions` accepts up to 2,000
  conversions per request and always sends `partialFailure:true`.

## Changed

- Google Ads reads retain bounded retries for rate limits and server errors.
  Writes are sent once. Check account state before resubmitting a write whose
  outcome is uncertain.
- Error messages include Google Ads error codes, field paths, and triggers
  when the upstream response provides them.

## Security

Mutations use the destructive risk class because they can permanently remove
resources. Malformed boolean options and raw write bodies fail before an
upstream request. The new operations use the existing `adwords` OAuth scope
and server-side developer-token credential.

## Known limitations

- Google restricts offline uploads for developer tokens without upload
  requests between December 17, 2025 and June 15, 2026. An affected token
  receives `CUSTOMER_NOT_ALLOWLISTED_FOR_THIS_FEATURE`; Google directs new
  integrations to the Data Manager API. See the
  [Google Ads setup guide](auth-guides/google-ads.md#offline-conversions).
- Inspect `partialFailureError` even when a mutation or conversion upload
  returns HTTP 200. Successful rows can coexist with rejected rows.
- Google Ads fixes the search page size. Follow `nextPageToken`, and narrow
  the query or selected fields if a response exceeds gum's size limit.
- The new operations were verified with local HTTP fixtures and dispatcher
  tests. Live Google Ads calls were not exercised for this release.
- macOS release binaries are not notarized. Check with
  `spctl --assess --type execute --verbose gum`. If Gatekeeper rejects the
  binary, clear its quarantine attribute with
  `xattr -d com.apple.quarantine gum`. The tap formula does this on install.
- The tap formula is updated after release assets publish, so it can trail
  the tag briefly.

## Token savings

Measured with the release fixtures using the candidate binary:

```bash
gum gain --fixture-replay --format=toon
gum gain --fixture-replay --format=json
```

| Default format | Total calls | Total tokens in | Total tokens saved | Aggregate savings |
| --- | ---: | ---: | ---: | ---: |
| `toon` | 10 | 3,922 | 0 | 0 % |
| `json` | 10 | 3,922 | -12 | 0.31 % overhead |

## Reproducibility

```bash
git checkout v1.2.0
cd apps/gum
CGO_ENABLED=0 go build -trimpath -ldflags='-s -w -X main.version=v1.2.0' ./cmd/gum
sha256sum gum
```

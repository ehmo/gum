# gum v1.0.0 Release Notes

gum v1.0.0 is the public release candidate for the CLI and MCP server.

## Highlights

- BYO OAuth is the public Google auth path. Users create and register their own
  Google Desktop OAuth client.
- The catalog covers 222 operations across 32 Google services.
- `gum doctor` checks BYO OAuth, API keys, service-account keys, and ADC.
- MCP setup now has a dedicated client guide and a stdio-only health check.

## Install

```shell
curl -fsSL https://raw.githubusercontent.com/ehmo/gum/main/install.sh | GUM_VERSION=v1.0.0 bash
gum --version
gum doctor
```

## Upgrade Notes

Register your own OAuth client before calling BYO OAuth operations:

```shell
printf '%s' "$GOOGLE_OAUTH_CLIENT_SECRET" \
  | gum auth use-oauth-client --client-id "$GOOGLE_OAUTH_CLIENT_ID" --secret-stdin
gum login --service gmail,calendar
```

## Added

- Auth guides for each supported Google API family.
- MCP client setup guide with absolute-path config snippets.

## Changed

- `gum login` now requires the operator to register a Google Desktop OAuth
  client before using OAuth-backed operations.
- Release builds no longer receive OAuth client secrets through GoReleaser or
  GitHub Actions.
- `cmd/test-matrix` runs without a default exception file.

## Fixed

- Public docs no longer advertise broken `go install` or remote plugin install
  paths.
- Plugin confinement docs now match the enforced runtime behavior.

## Security

- Removed bundled OAuth client credentials from the public auth path.
- Removed build-time OAuth secret injection from release configuration.

## Known Limitations

- Public install URLs must be checked without GitHub authentication after the
  repository is made public.
- Google APIs can still reject valid OAuth grants for product-specific setup,
  tenant policy, property verification, developer-token approval, or quota.

## Token savings

Measured on the in-tree release fixtures before tagging. The TOON row uses the
release-profile replay gate over 200 fixtures. The JSON row is the raw fixture
replay baseline for callers that bypass shaping.

| Default format | Total calls | Total tokens in | Total tokens saved | Aggregate savings |
| --- | ---: | ---: | ---: | ---: |
| `toon` | 200 | 546,703 | 456,374 | 83.48 % |
| `json` | 10 | 3,922 | -12 | 0.31 % overhead |

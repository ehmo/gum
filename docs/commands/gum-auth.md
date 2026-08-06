# `gum auth`

> Generated from `gum schema --json`. Do not edit this page by hand; run `make docs-commands`.

Manage Google OAuth credentials

## Usage

```bash
gum auth
```

## Parent

- [gum](gum.md)

## Subcommands

- [gum auth login](gum-auth-login.md) - Authorize gum via your OAuth client (loopback + PKCE; no gcloud)
- [gum auth probe](gum-auth-probe.md) - Acquire a token for --scopes and print non-secret metadata
- [gum auth setup](gum-auth-setup.md) - Walk the credential prerequisites for an operation
- [gum auth status](gum-auth-status.md) - Print resolved auth provider and scope coverage
- [gum auth use-ads-developer-token](gum-auth-use-ads-developer-token.md) - Store the Google Ads API developer token in the OS keychain so the
- [gum auth use-api-key](gum-auth-use-api-key.md) - Configure the api_key auth strategy
- [gum auth use-oauth-client](gum-auth-use-oauth-client.md) - Register a Desktop-app OAuth client you created in the Google Cloud console.
- [gum auth use-service-account](gum-auth-use-service-account.md) - Configure the service_account_key auth strategy

## Flags

| Flag | Type | Default | Help |
| --- | --- | --- | --- |
| `--log-format` | `string` | json | Log format: json\|text |
| `--log-level` | `string` | info | Log level: debug\|info\|warn\|error (overrides GUM_LOG_LEVEL) |
| `--profile` | `string` | default | Profile name to read/write config under |

## See also

- [gum](gum.md)
- [Command index](README.md)

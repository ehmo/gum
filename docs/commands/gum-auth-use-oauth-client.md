# `gum auth use-oauth-client`

> Generated from `gum schema --json`. Do not edit this page by hand; run `make docs-commands`.

Register a Desktop-app OAuth client you created in the Google Cloud console.
gum then runs the browser login itself (loopback + PKCE) and refreshes tokens
automatically; no gcloud dependency. Create one at:
  https://console.cloud.google.com/apis/credentials > Create credentials > OAuth client ID > Desktop app
Google issues a client_secret for Desktop-app clients and its token endpoint REQUIRES it
even with PKCE, so pipe the secret via --secret-stdin (it never enters shell history).
Only a true public client, rare for Google, may omit the secret.

## Usage

```bash
gum auth use-oauth-client --client-id <id> [--secret-stdin | --secret-file <path>] [flags]
```

## Parent

- [gum auth](gum-auth.md)

## Arguments

| Name | Help |
| --- | --- |
| `id` |  |
| `path` |  |

## Flags

| Flag | Type | Default | Help |
| --- | --- | --- | --- |
| `--client-id` | `string` |  | OAuth client ID from the Google Cloud console (required) |
| `--secret-file` | `string` |  | Read the client secret from this file |
| `--secret-stdin` | `bool` | false | Read the client secret from stdin (kept out of shell history) |
| `--log-format` | `string` | json | Log format: json\|text |
| `--log-level` | `string` | info | Log level: debug\|info\|warn\|error (overrides GUM_LOG_LEVEL) |
| `--profile` | `string` | default | Profile name to read/write config under |

## See also

- [gum auth](gum-auth.md)
- [Command index](README.md)

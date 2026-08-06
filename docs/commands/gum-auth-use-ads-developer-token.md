# `gum auth use-ads-developer-token`

> Generated from `gum schema --json`. Do not edit this page by hand; run `make docs-commands`.

Store the Google Ads API developer token in the OS keychain so the
googleads.* Keyword Planner ops can send it as the developer-token header.
The token is a secret: pipe it via --stdin (default) or --from-file; it is
never accepted as a positional argument and never sent as an invocation arg.
The OAuth Bearer is separate. Run `gum auth use-oauth-client` then
`gum login --service googleads` to authorize the adwords scope.

## Usage

```bash
gum auth use-ads-developer-token [flags]
```

## Parent

- [gum auth](gum-auth.md)

## Flags

| Flag | Type | Default | Help |
| --- | --- | --- | --- |
| `--from-file` | `string` |  | Read the developer token from this file (alternative to --stdin) |
| `--stdin` | `bool` | false | Read the developer token from stdin (default when no --from-file is given) |
| `--log-format` | `string` | json | Log format: json\|text |
| `--log-level` | `string` | info | Log level: debug\|info\|warn\|error (overrides GUM_LOG_LEVEL) |
| `--profile` | `string` | default | Profile name to read/write config under |

## See also

- [gum auth](gum-auth.md)
- [Command index](README.md)

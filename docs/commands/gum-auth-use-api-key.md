# `gum auth use-api-key`

> Generated from `gum schema --json`. Do not edit this page by hand; run `make docs-commands`.

Configure the api_key auth strategy

## Usage

```bash
gum auth use-api-key [flags]
```

## Parent

- [gum auth](gum-auth.md)

## Flags

| Flag | Type | Default | Help |
| --- | --- | --- | --- |
| `--from-file` | `string` |  | Read the API key from this file (alternative to --stdin) |
| `--stdin` | `bool` | false | Read the API key from stdin (default when no --from-file is given) |
| `--log-format` | `string` | json | Log format: json\|text |
| `--log-level` | `string` | info | Log level: debug\|info\|warn\|error (overrides GUM_LOG_LEVEL) |
| `--profile` | `string` | default | Profile name to read/write config under |

## See also

- [gum auth](gum-auth.md)
- [Command index](README.md)

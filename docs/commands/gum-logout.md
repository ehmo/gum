# `gum logout`

> Generated from `gum schema --json`. Do not edit this page by hand; run `make docs-commands`.

Clear gum's stored OAuth credentials (switch or sign out of a Google account)

## Usage

```bash
gum logout [flags]
```

## Parent

- [gum](gum.md)

## Flags

| Flag | Type | Default | Help |
| --- | --- | --- | --- |
| `--forget-client` | `bool` | false | Also remove the registered BYO OAuth client, not just the login grant |
| `--log-format` | `string` | json | Log format: json\|text |
| `--log-level` | `string` | info | Log level: debug\|info\|warn\|error (overrides GUM_LOG_LEVEL) |
| `--profile` | `string` | default | Profile name to read/write config under |

## See also

- [gum](gum.md)
- [Command index](README.md)

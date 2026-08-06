# `gum plugin run`

> Generated from `gum schema --json`. Do not edit this page by hand; run `make docs-commands`.

Call a tool on a running plugin

## Usage

```bash
gum plugin run <id> <tool> [args-json]
```

## Parent

- [gum plugin](gum-plugin.md)

## Arguments

| Name | Help |
| --- | --- |
| `id` |  |
| `tool` |  |
| `args-json` |  |

## Flags

| Flag | Type | Default | Help |
| --- | --- | --- | --- |
| `--log-format` | `string` | json | Log format: json\|text |
| `--log-level` | `string` | info | Log level: debug\|info\|warn\|error (overrides GUM_LOG_LEVEL) |
| `--profile` | `string` | default | Profile name to read/write config under |

## See also

- [gum plugin](gum-plugin.md)
- [Command index](README.md)

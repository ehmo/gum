# `gum search`

> Generated from `gum schema --json`. Do not edit this page by hand; run `make docs-commands`.

BM25 search the embedded catalog (TTY table, pipe JSON)

## Usage

```bash
gum search <query> [flags]
```

## Parent

- [gum](gum.md)

## Arguments

| Name | Help |
| --- | --- |
| `query` |  |

## Flags

| Flag | Type | Default | Help |
| --- | --- | --- | --- |
| `--format` | `string` |  | Output format: json\|table (default: TTY=table, pipe=json) |
| `--top-k` | `int` | 10 | Maximum number of results |
| `--log-format` | `string` | json | Log format: json\|text |
| `--log-level` | `string` | info | Log level: debug\|info\|warn\|error (overrides GUM_LOG_LEVEL) |
| `--profile` | `string` | default | Profile name to read/write config under |

## See also

- [gum](gum.md)
- [Command index](README.md)

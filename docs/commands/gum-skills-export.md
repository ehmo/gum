# `gum skills export`

> Generated from `gum schema --json`. Do not edit this page by hand; run `make docs-commands`.

Export installable gum skills

## Usage

```bash
gum skills export --out <dir> [flags]
```

## Parent

- [gum skills](gum-skills.md)

## Arguments

| Name | Help |
| --- | --- |
| `dir` |  |

## Flags

| Flag | Type | Default | Help |
| --- | --- | --- | --- |
| `--force` | `bool` | false | Overwrite existing files |
| `--format` | `string` | text | Output format: text\|json |
| `--out` | `string` |  | Output directory |
| `--log-format` | `string` | json | Log format: json\|text |
| `--log-level` | `string` | info | Log level: debug\|info\|warn\|error (overrides GUM_LOG_LEVEL) |
| `--profile` | `string` | default | Profile name to read/write config under |

## See also

- [gum skills](gum-skills.md)
- [Command index](README.md)

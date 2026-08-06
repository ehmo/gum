# `gum skills install`

> Generated from `gum schema --json`. Do not edit this page by hand; run `make docs-commands`.

Install gum skills for Codex-compatible agents

## Usage

```bash
gum skills install [flags]
```

## Parent

- [gum skills](gum-skills.md)

## Flags

| Flag | Type | Default | Help |
| --- | --- | --- | --- |
| `--dir` | `string` |  | Install directory (default: $CODEX_HOME/skills or ~/.codex/skills) |
| `--dry-run` | `bool` | false | Print files without changing disk |
| `--force` | `bool` | false | Overwrite existing files |
| `--format` | `string` | text | Output format: text\|json |
| `--target` | `string` | codex | Skills target: codex |
| `--log-format` | `string` | json | Log format: json\|text |
| `--log-level` | `string` | info | Log level: debug\|info\|warn\|error (overrides GUM_LOG_LEVEL) |
| `--profile` | `string` | default | Profile name to read/write config under |

## See also

- [gum skills](gum-skills.md)
- [Command index](README.md)

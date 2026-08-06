# `gum agents install`

> Generated from `gum schema --json`. Do not edit this page by hand; run `make docs-commands`.

Install gum agent files

## Usage

```bash
gum agents install [flags]
```

## Parent

- [gum agents](gum-agents.md)

## Flags

| Flag | Type | Default | Help |
| --- | --- | --- | --- |
| `--dry-run` | `bool` | false | Print the write plan without changing files |
| `--features` | `string` | skills,mcp | Comma-separated features: skills,mcp |
| `--force` | `bool` | false | Overwrite existing skill files and replace managed MCP blocks |
| `--format` | `string` | text | Output format: text\|json |
| `--mcp-arg` | `stringArray` | [] | Extra argument appended to the gum mcp command |
| `--scope` | `string` | user | Install scope: user\|project |
| `--target` | `string` | all | Agent target: codex\|claude\|cursor\|gemini\|all |
| `--toolset` | `string` | core | MCP toolset (core) |
| `--log-format` | `string` | json | Log format: json\|text |
| `--log-level` | `string` | info | Log level: debug\|info\|warn\|error (overrides GUM_LOG_LEVEL) |
| `--profile` | `string` | default | Profile name to read/write config under |

## See also

- [gum agents](gum-agents.md)
- [Command index](README.md)

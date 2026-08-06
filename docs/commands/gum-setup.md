# `gum setup`

> Generated from `gum schema --json`. Do not edit this page by hand; run `make docs-commands`.

Guided setup for a local gum install. It writes agent skills and MCP config for Codex, Claude, Cursor, or Gemini, then prints the Google OAuth and doctor commands needed for first success.

## Usage

```bash
gum setup [flags]
```

## Parent

- [gum](gum.md)

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
| `--yes` | `bool` | false | Apply without an interactive confirmation |
| `--log-format` | `string` | json | Log format: json\|text |
| `--log-level` | `string` | info | Log level: debug\|info\|warn\|error (overrides GUM_LOG_LEVEL) |
| `--profile` | `string` | default | Profile name to read/write config under |

## See also

- [gum](gum.md)
- [Command index](README.md)

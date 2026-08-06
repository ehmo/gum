# `gum init`

> Generated from `gum schema --json`. Do not edit this page by hand; run `make docs-commands`.

gum init bootstraps GUM for a new user or project. Default behavior is diff-and-prompt: gum init never silently patches a security-sensitive file. Use --target to pick the host (claude-code | claude-desktop | cursor). Use --refresh to regenerate GUM.md only after a gum upgrade.

## Usage

```bash
gum init [flags]
```

## Parent

- [gum](gum.md)

## Flags

| Flag | Type | Default | Help |
| --- | --- | --- | --- |
| `--global` | `bool` | false | Patch ~/.claude/settings.json and write ~/GUM.md (instead of project-local; claude-code target only) |
| `--refresh` | `bool` | false | Rewrite GUM.md only; do not touch the host config or credentials |
| `--target` | `string` | claude-code | MCP host to patch: claude-code \| claude-desktop \| cursor |
| `--yes` | `bool` | false | Skip interactive confirmation and apply the patch |
| `--log-format` | `string` | json | Log format: json\|text |
| `--log-level` | `string` | info | Log level: debug\|info\|warn\|error (overrides GUM_LOG_LEVEL) |
| `--profile` | `string` | default | Profile name to read/write config under |

## See also

- [gum](gum.md)
- [Command index](README.md)

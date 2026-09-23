# `gum agents`

> Generated from `gum schema --json`. Do not edit this page by hand; run `make docs-commands`.

Install gum skills and MCP config for coding agents

## Usage

```bash
gum agents
```

## Parent

- [gum](gum.md)

## Subcommands

- [gum agents install](gum-agents-install.md) - Install gum agent files

## Flags

| Flag | Type | Default | Help |
| --- | --- | --- | --- |
| `--log-format` | `string` | json | Log format: json\|text |
| `--log-level` | `string` | info | Log level: debug\|info\|warn\|error (overrides GUM_LOG_LEVEL) |
| `--no-warn-lossy` | `bool` | false | Suppress the OVERRIDE_DISABLES_LOSSY_STAGE warning when a profile override drops a lossy-compression stage |
| `--profile` | `string` | default | Profile name to read/write config under |

## See also

- [gum](gum.md)
- [Command index](README.md)

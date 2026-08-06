# `gum plugin reload`

> Generated from `gum schema --json`. Do not edit this page by hand; run `make docs-commands`.

Clears any quarantine state for the named plugin, then spawns the subprocess once via the supervisor to act as a passive canary. A spawn failure re-quarantines the plugin.

## Usage

```bash
gum plugin reload <id>
```

## Parent

- [gum plugin](gum-plugin.md)

## Arguments

| Name | Help |
| --- | --- |
| `id` |  |

## Flags

| Flag | Type | Default | Help |
| --- | --- | --- | --- |
| `--log-format` | `string` | json | Log format: json\|text |
| `--log-level` | `string` | info | Log level: debug\|info\|warn\|error (overrides GUM_LOG_LEVEL) |
| `--profile` | `string` | default | Profile name to read/write config under |

## See also

- [gum plugin](gum-plugin.md)
- [Command index](README.md)

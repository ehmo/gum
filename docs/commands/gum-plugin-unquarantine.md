# `gum plugin unquarantine`

> Generated from `gum schema --json`. Do not edit this page by hand; run `make docs-commands`.

Resets quarantined, retry_count, backoff_step, and next_retry_at in plugin-state.json so the plugin can be invoked on the next call. Use when the operator has independently verified the plugin is healthy and wants to bypass the exponential-backoff window.

## Usage

```bash
gum plugin unquarantine <id>
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

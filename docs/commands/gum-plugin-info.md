# `gum plugin info`

> Generated from `gum schema --json`. Do not edit this page by hand; run `make docs-commands`.

Prints the plugin record assembled from plugins.lock, plugin-state.json and
plugin-catalog.json for the active profile.

--format=json emits the same object the gum://plugin/<name> MCP resource
serves, byte for byte, so scripts can diff the two.

## Usage

```bash
gum plugin info <name> [flags]
```

## Parent

- [gum plugin](gum-plugin.md)

## Arguments

| Name | Help |
| --- | --- |
| `name` |  |

## Flags

| Flag | Type | Default | Help |
| --- | --- | --- | --- |
| `--format` | `string` | text | Output format: text\|json |
| `--log-format` | `string` | json | Log format: json\|text |
| `--log-level` | `string` | info | Log level: debug\|info\|warn\|error (overrides GUM_LOG_LEVEL) |
| `--profile` | `string` | default | Profile name to read/write config under |

## See also

- [gum plugin](gum-plugin.md)
- [Command index](README.md)

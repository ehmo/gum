# `gum mcp`

> Generated from `gum schema --json`. Do not edit this page by hand; run `make docs-commands`.

Run the gum MCP server. The public release supports --stdio transport.

## Usage

```bash
gum mcp [flags]
```

## Parent

- [gum](gum.md)

## Flags

| Flag | Type | Default | Help |
| --- | --- | --- | --- |
| `--stdio` | `bool` | false | Run on stdio transport (required) |
| `--log-format` | `string` | json | Log format: json\|text |
| `--log-level` | `string` | info | Log level: debug\|info\|warn\|error (overrides GUM_LOG_LEVEL) |
| `--profile` | `string` | default | Profile name to read/write config under |

## See also

- [gum](gum.md)
- [Command index](README.md)

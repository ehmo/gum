# `gum schema`

> Generated from `gum schema --json`. Do not edit this page by hand; run `make docs-commands`.

Print a JSON description of the active gum command tree, including command paths, aliases, arguments, and flags.

## Usage

```bash
gum schema [flags]
```

## Parent

- [gum](gum.md)

## Flags

| Flag | Type | Default | Help |
| --- | --- | --- | --- |
| `-h`<br>`--help` | `bool` | false | help for schema |
| `--json` | `bool` | false | Emit JSON output |
| `--log-format` | `string` | json | Log format: json\|text |
| `--log-level` | `string` | info | Log level: debug\|info\|warn\|error (overrides GUM_LOG_LEVEL) |
| `--no-warn-lossy` | `bool` | false | Suppress the OVERRIDE_DISABLES_LOSSY_STAGE warning when a profile override drops a lossy-compression stage |
| `--profile` | `string` | default | Profile name to read/write config under |

## See also

- [gum](gum.md)
- [Command index](README.md)

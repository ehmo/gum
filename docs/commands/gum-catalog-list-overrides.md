# `gum catalog list-overrides`

> Generated from `gum schema --json`. Do not edit this page by hand; run `make docs-commands`.

List all variants with risk_override=true from the resolved catalog

## Usage

```bash
gum catalog list-overrides
```

## Parent

- [gum catalog](gum-catalog.md)

## Flags

| Flag | Type | Default | Help |
| --- | --- | --- | --- |
| `--log-format` | `string` | json | Log format: json\|text |
| `--log-level` | `string` | info | Log level: debug\|info\|warn\|error (overrides GUM_LOG_LEVEL) |
| `--no-warn-lossy` | `bool` | false | Suppress the OVERRIDE_DISABLES_LOSSY_STAGE warning when a profile override drops a lossy-compression stage |
| `--profile` | `string` | default | Profile name to read/write config under |

## See also

- [gum catalog](gum-catalog.md)
- [Command index](README.md)

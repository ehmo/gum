# `gum auth setup`

> Generated from `gum schema --json`. Do not edit this page by hand; run `make docs-commands`.

Walk the credential prerequisites for an operation

## Usage

```bash
gum auth setup <op_id>
```

## Parent

- [gum auth](gum-auth.md)

## Arguments

| Name | Help |
| --- | --- |
| `op_id` |  |

## Flags

| Flag | Type | Default | Help |
| --- | --- | --- | --- |
| `--log-format` | `string` | json | Log format: json\|text |
| `--log-level` | `string` | info | Log level: debug\|info\|warn\|error (overrides GUM_LOG_LEVEL) |
| `--no-warn-lossy` | `bool` | false | Suppress the OVERRIDE_DISABLES_LOSSY_STAGE warning when a profile override drops a lossy-compression stage |
| `--profile` | `string` | default | Profile name to read/write config under |

## See also

- [gum auth](gum-auth.md)
- [Command index](README.md)

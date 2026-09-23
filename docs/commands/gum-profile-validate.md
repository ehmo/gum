# `gum profile validate`

> Generated from `gum schema --json`. Do not edit this page by hand; run `make docs-commands`.

Parse an expression-profile DSL file and report any errors. Use this in CI to catch malformed catalog profiles before release.

## Usage

```bash
gum profile validate <path> [flags]
```

## Parent

- [gum profile](gum-profile.md)

## Arguments

| Name | Help |
| --- | --- |
| `path` |  |

## Flags

| Flag | Type | Default | Help |
| --- | --- | --- | --- |
| `--variant` | `string` |  | Catalog variant to bind for the strip_nulls safety check |
| `--log-format` | `string` | json | Log format: json\|text |
| `--log-level` | `string` | info | Log level: debug\|info\|warn\|error (overrides GUM_LOG_LEVEL) |
| `--no-warn-lossy` | `bool` | false | Suppress the OVERRIDE_DISABLES_LOSSY_STAGE warning when a profile override drops a lossy-compression stage |
| `--profile` | `string` | default | Profile name to read/write config under |

## See also

- [gum profile](gum-profile.md)
- [Command index](README.md)

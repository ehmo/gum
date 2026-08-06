# `gum auth probe`

> Generated from `gum schema --json`. Do not edit this page by hand; run `make docs-commands`.

Acquire a token for --scopes and print non-secret metadata

## Usage

```bash
gum auth probe [flags]
```

## Parent

- [gum auth](gum-auth.md)

## Flags

| Flag | Type | Default | Help |
| --- | --- | --- | --- |
| `--scopes` | `stringSlice` | [gmail.readonly] | Scopes to acquire |
| `--strategy` | `string` | auto | Auth strategy to probe: auto, byo_oauth, or adc |
| `--log-format` | `string` | json | Log format: json\|text |
| `--log-level` | `string` | info | Log level: debug\|info\|warn\|error (overrides GUM_LOG_LEVEL) |
| `--profile` | `string` | default | Profile name to read/write config under |

## See also

- [gum auth](gum-auth.md)
- [Command index](README.md)

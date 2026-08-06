# `gum auth status`

> Generated from `gum schema --json`. Do not edit this page by hand; run `make docs-commands`.

Print resolved auth provider and scope coverage

## Usage

```bash
gum auth status [flags]
```

## Parent

- [gum auth](gum-auth.md)

## Flags

| Flag | Type | Default | Help |
| --- | --- | --- | --- |
| `--scopes` | `stringSlice` | [gmail.readonly] | Catalog scopes to probe |
| `--log-format` | `string` | json | Log format: json\|text |
| `--log-level` | `string` | info | Log level: debug\|info\|warn\|error (overrides GUM_LOG_LEVEL) |
| `--profile` | `string` | default | Profile name to read/write config under |

## See also

- [gum auth](gum-auth.md)
- [Command index](README.md)

# `gum config`

> Generated from `gum schema --json`. Do not edit this page by hand; run `make docs-commands`.

Read or write values in the active profile's config.toml. The active profile is selected via --profile (default: 'default').

## Usage

```bash
gum config
```

## Parent

- [gum](gum.md)

## Subcommands

- [gum config get](gum-config-get.md) - Print the value of a config key from the active profile
- [gum config list](gum-config-list.md) - List all config keys in the active profile
- [gum config set](gum-config-set.md) - Persist a config key=value pair to the active profile
- [gum config unset](gum-config-unset.md) - Remove a config key from the active profile

## Flags

| Flag | Type | Default | Help |
| --- | --- | --- | --- |
| `--log-format` | `string` | json | Log format: json\|text |
| `--log-level` | `string` | info | Log level: debug\|info\|warn\|error (overrides GUM_LOG_LEVEL) |
| `--profile` | `string` | default | Profile name to read/write config under |

## See also

- [gum](gum.md)
- [Command index](README.md)

# `gum canary`

> Generated from `gum schema --json`. Do not edit this page by hand; run `make docs-commands`.

gum canary --plugin=<id> [--live] resolves the named plugin under the active install root, spawns it once via the plugin host, and reports the outcome as a stable JSON envelope on stdout. A failed canary surfaces SERVICE_DOWN.

## Usage

```bash
gum canary [flags]
```

## Parent

- [gum](gum.md)

## Flags

| Flag | Type | Default | Help |
| --- | --- | --- | --- |
| `--live` | `bool` | false | Issue a live subprocess ping after Start |
| `--plugin` | `string` |  | Plugin id to canary (required) |
| `--log-format` | `string` | json | Log format: json\|text |
| `--log-level` | `string` | info | Log level: debug\|info\|warn\|error (overrides GUM_LOG_LEVEL) |
| `--profile` | `string` | default | Profile name to read/write config under |

## See also

- [gum](gum.md)
- [Command index](README.md)

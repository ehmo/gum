# `gum cache clear`

> Generated from `gum schema --json`. Do not edit this page by hand; run `make docs-commands`.

Clear the dispatcher response cache

## Usage

```bash
gum cache clear [flags]
```

## Parent

- [gum cache](gum-cache.md)

## Flags

| Flag | Type | Default | Help |
| --- | --- | --- | --- |
| `--bak` | `bool` | false | Remove http.db.bak backup file |
| `--expired` | `bool` | false | Evict TTL-expired cache entries |
| `--log-format` | `string` | json | Log format: json\|text |
| `--log-level` | `string` | info | Log level: debug\|info\|warn\|error (overrides GUM_LOG_LEVEL) |
| `--profile` | `string` | default | Profile name to read/write config under |

## See also

- [gum cache](gum-cache.md)
- [Command index](README.md)

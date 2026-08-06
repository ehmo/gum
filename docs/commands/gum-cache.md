# `gum cache`

> Generated from `gum schema --json`. Do not edit this page by hand; run `make docs-commands`.

Inspect or clear the dispatcher response cache

## Usage

```bash
gum cache
```

## Parent

- [gum](gum.md)

## Subcommands

- [gum cache clear](gum-cache-clear.md) - Clear the dispatcher response cache
- [gum cache migrate](gum-cache-migrate.md) - Migrate BoltDB cache (http.db) to WAL-SQLite (http-wal.db)
- [gum cache stats](gum-cache-stats.md) - Print dispatcher cache stats

## Flags

| Flag | Type | Default | Help |
| --- | --- | --- | --- |
| `--log-format` | `string` | json | Log format: json\|text |
| `--log-level` | `string` | info | Log level: debug\|info\|warn\|error (overrides GUM_LOG_LEVEL) |
| `--profile` | `string` | default | Profile name to read/write config under |

## See also

- [gum](gum.md)
- [Command index](README.md)

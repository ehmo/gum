# `gum cache migrate`

> Generated from `gum schema --json`. Do not edit this page by hand; run `make docs-commands`.

Migrate BoltDB cache (http.db) to WAL-SQLite (http-wal.db)

## Usage

```bash
gum cache migrate [flags]
```

## Parent

- [gum cache](gum-cache.md)

## Flags

| Flag | Type | Default | Help |
| --- | --- | --- | --- |
| `--force` | `bool` | false | Discard existing http-wal.db without sentinel and re-migrate from http.db |
| `--log-format` | `string` | json | Log format: json\|text |
| `--log-level` | `string` | info | Log level: debug\|info\|warn\|error (overrides GUM_LOG_LEVEL) |
| `--profile` | `string` | default | Profile name to read/write config under |

## See also

- [gum cache](gum-cache.md)
- [Command index](README.md)

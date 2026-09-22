# `gum cache clear`

> Generated from `gum schema --json`. Do not edit this page by hand; run `make docs-commands`.

Clear the dispatcher response cache.

With no flags this removes stored HTTP/ETag validators. Those entries have
no TTL and nothing evicts them, so this is the only way to reclaim the
store or to force a read to be re-shaped under a changed output profile.

pattern is a glob matched against op_id: `gum cache clear "gmail.*"` drops
one API and `gum cache clear gmail.users.messages.list` drops one op. With
no pattern the whole store is cleared.

## Usage

```bash
gum cache clear [pattern] [flags]
```

## Parent

- [gum cache](gum-cache.md)

## Arguments

| Name | Help |
| --- | --- |
| `pattern` |  |

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

# `gum gain`

> Generated from `gum schema --json`. Do not edit this page by hand; run `make docs-commands`.

Print cumulative gain (token-savings) stats from the local ledger, or replay a fixture set with --fixture-replay.

## Usage

```bash
gum gain [flags]
```

## Parent

- [gum](gum.md)

## Flags

| Flag | Type | Default | Help |
| --- | --- | --- | --- |
| `--by-op` | `bool` | false | Aggregate gain by op_id |
| `--fixture-replay` | `bool` | false | Replay fixtures from testdata/fixtures/gain-replay |
| `--format` | `string` | toon | Output format for --fixture-replay only (json\|toon); ignored otherwise |
| `--since` | `string` |  | Filter ledger entries with ts >= since (RFC3339 UTC) |
| `--until` | `string` |  | Filter ledger entries with ts <= until (RFC3339 UTC) |
| `--log-format` | `string` | json | Log format: json\|text |
| `--log-level` | `string` | info | Log level: debug\|info\|warn\|error (overrides GUM_LOG_LEVEL) |
| `--profile` | `string` | default | Profile name to read/write config under |

## See also

- [gum](gum.md)
- [Command index](README.md)

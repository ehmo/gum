# Token-savings ledger and reporting

GUM's "gain" ledger records, for every dispatch, how many tokens the host
client would have spent calling the raw upstream API directly versus how
many it actually spent through GUM. The delta is the **gain** — the reason
the tool exists.

## What gets recorded

Every dispatch appends one JSON line to the profile's gain ledger at
`~/.local/share/gum/<profile>/gain-ledger.jsonl`, honouring `XDG_DATA_HOME`.
The first line is a header record that pins the schema version and the
tokenizer, so old rows stay comparable if either changes.

The fields that carry the savings claim:

| Field                      | Meaning                                                    |
|----------------------------|------------------------------------------------------------|
| `op_id` / `op_family`      | The op invoked, and the same id with the method stripped.  |
| `variant_id`               | The resolved variant. Null for a `gum_parallel` outer row. |
| `output_profile`           | The expression profile that shaped the response.           |
| `raw_tokens`               | The upstream body before any shaping.                      |
| `shaped_tokens`            | The body after shaping. The gain is the difference.        |
| `request_tokens`           | The outgoing request.                                      |
| `response_tokens`          | What the host client received.                             |
| `cache_status`             | `miss`, `hit`, `semantic`, `etag_304`, `not_applicable`.   |
| `field_mask_status`        | `applied`, `skipped`, or `not_applicable`.                 |
| `is_retry`                 | Same session, op family, and args hash within 5 minutes.   |
| `error_code`               | Set on a failed dispatch.                                  |
| `timestamp`                | RFC3339 UTC at append time.                                |

The ledger lives in one profile's data directory, so a second profile keeps
its own file. Each row also carries `args_hash` and
`auth_subject_fingerprint`, which is why the file is written mode 600.

## Token counting

GUM counts tokens with `cl100k_base` through `tiktoken-go/tokenizer`. The
count is exact for that encoding, not an estimate from byte length, and no
external API is called. The header row records the tokenizer name so a later
change to it is visible in the file rather than silent.

## Reporting

- `gum gain` — cumulative totals from the local ledger.
- `gum gain --by-op` — aggregate totals by `op_id`.
- `gum gain --since=2026-06-01T00:00:00Z` — include rows at or after an
  RFC3339 UTC timestamp.
- `gum gain --until=2026-06-03T00:00:00Z` — include rows at or before an
  RFC3339 UTC timestamp.
- `gum gain --fixture-replay --format=json` — replay the checked-in gain
  fixtures and print the machine-readable envelope.
- `gum.gain` (meta-tool) — the same data surfaced through MCP so the host
  client can render its own dashboard.

The ledger never stores argument values or response bodies, only token
counts, identifiers, and the two hashes above. It is plain JSONL, so read it
with any line-oriented tool.

## Retention

The ledger rotates once it passes 100 MB. Time-windowed reporting is controlled at read
time with `--since` and `--until`. There is no profile-specific retention
knob.

## Errors

- `GAIN_DISABLED` — accounting is off for this profile. Both the recorder
  and the reader consult one switch, so `gum config set gain.enabled=false`
  or `GUM_GAIN_DISABLED=1` makes `gum.gain` return this instead of zeroes.
- `GAIN_LEDGER_UNAVAILABLE` — GUM could not resolve the profile's data
  directory or open the ledger file there.

A row that fails JSON parse is skipped and the rest of the ledger still
reports. A torn trailing line from an interrupted write costs you that one
entry, not the report.

See `gum://help/toon-format` for why bytes saved on the wire translate to
tokens saved at the model.

# Token-savings ledger and reporting

GUM's "gain" ledger records, for every dispatch, how many tokens the host
client would have spent calling the raw upstream API directly versus how
many it actually spent through GUM. The delta is the **gain** — the reason
the tool exists.

## What gets recorded

Every successful dispatch appends one row to the local gain ledger,
`~/.local/share/gum/gain-ledger.jsonl`, containing:

| Field        | Meaning                                                       |
|--------------|---------------------------------------------------------------|
| `op_id`      | The op invoked (e.g. `gmail.users.messages.list`).            |
| `variant_id` | The resolved variant (rest/grpc/code-mode).                   |
| `format`     | The output wire format (`toon`, `json`, `markdown`).          |
| `bytes_in`   | Bytes the host client sent (request body + tool args).        |
| `bytes_out`  | Bytes returned to the host client after projection + format.  |
| `wall_ms`    | Wall-clock dispatch latency.                                  |
| `cache_hit`  | Whether the semantic response cache served the request.       |
| `timestamp`  | UTC RFC 3339 timestamp.                                       |

The ledger is principal-scoped: rows for one user never bleed into another
user's view.

## Token estimation

GUM converts `bytes_in` / `bytes_out` to tokens using the `cl100k_base`
tokenizer in BM25-only-v1 mode. No external model API is called — the
estimator is deterministic and runs entirely offline. Future versions may
add a per-model tokenizer when the upstream contract stabilises.

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

The gain ledger never includes argument values or response bodies; only the
byte counts and op identifiers. Run `gum gain --raw` to inspect the file
directly; it is plain JSONL.

## Retention

The ledger rotates by file size. Time-windowed reporting is controlled at read
time with `--since` and `--until`; v0.1.x does not expose a profile-specific
retention knob.

## Errors

- `GAIN_LEDGER_LOCKED` — concurrent writer in another process is holding
  the advisory lock for longer than 5 s. Retry; this should be rare.
- `GAIN_LEDGER_CORRUPT` — a row failed JSON parse. GUM quarantines the
  ledger to `gain.ledger.corrupt-<timestamp>` and starts fresh so reporting
  doesn't poison subsequent dispatches.

See `gum://help/toon-format` for why bytes saved on the wire translate to
tokens saved at the model.

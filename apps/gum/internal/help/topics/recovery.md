# Recovery artifacts and resource links

When an expression profile sets `recovery != "none"`, GUM tees the raw
upstream response to disk before applying lossy projection, so the host
client can recover the full payload if needed (spec §9.0).

## Recovery modes

- **`none`** — no tee. Lossy projection is destructive; the raw payload is
  unrecoverable. Use only for ops whose response is already small enough
  that the savings don't justify the tee cost.
- **`local_artifact`** — tee the raw body to disk, expose the path on the
  response envelope (`_expression.full_result_path`). The host client can
  read the file directly; no MCP resource is advertised.
- **`resource_link`** — tee plus emit a `gum://results/{hash}` resource
  link. The host client calls `resources/read` on the link to retrieve
  the decompressed payload.

The `tee_mode` knob composes with `recovery`:

| `recovery`        | `tee_mode` default | Behaviour                                              |
|-------------------|--------------------|--------------------------------------------------------|
| `none`            | `off`              | Never tee.                                             |
| `local_artifact`  | `always`           | Tee every dispatch; expose path only.                  |
| `resource_link`   | `always`           | Tee every dispatch; expose path + `gum://` resource.   |

Operators can override `tee_mode` to `failures` (tee only when the upstream
returns 4xx/5xx) or `off` (suppress tee even with `recovery=local_artifact`).
The combination `resource_link` + `tee_mode=off` is rejected at profile
validation — there would be nothing for the link to resolve to.

## On-disk layout

```
~/.local/share/gum/<profile>/tee/<YYYY-MM-DD>/<op_id>/<hash>.json.gz
```

- Mode 600, gzipped, content-typed by extension.
- Hash = HMAC-SHA-256(`<profile>/tee.secret`, `<op_id>:<variant_id>:<args_canonical>:<subject_fingerprint>`).
- Principal-scoped: two different subjects running the same op on the same
  args produce two distinct artifacts. Cross-principal handle reuse is
  prevented by construction.

## Retention

The default retention window is **24 hours**, configurable via
`output.tee_retention_hours` in `~/.config/gum/<profile>/config.toml`. Pruning
runs lazily on each tee write — old day-buckets are removed when GUM
notices them on disk.

## Reverse lookup

`resources/read` on `gum://results/{hash}` performs a directory scan of
the active profile's `tee/` tree, bounded by the retention window. A
sidecar index (BoltDB hash→path) is deferred to v0.3.0 when dynamic
resource subscription lands.

For v0.1.0 this means lookups take O(N) where N is the number of artifacts
in the last 24 hours per op. In practice, N is small enough (single-digit
thousands worst case) that the scan completes within the MCP-spec 100 ms
P95 budget.

## What if the artifact has been pruned?

`resources/read` returns the canonical envelope:

```json
{
  "error_code":   "RESULT_ARTIFACT_EXPIRED",
  "hash":         "<the hash>",
  "uri":          "gum://results/<hash>",
  "expires_at":   null,
  "user_message": "Result artifact expired; re-issue the originating operation to obtain a fresh result handle.",
  "suggestion":   "Re-issue the originating operation to obtain a fresh result handle."
}
```

Wrapped in a JSON-RPC error with `code = -32010`. Recovery is not
retryable; the originating op MUST be re-dispatched.

## Quick commands

- `gum profile validate <path>` — validate an expression profile that enables
  `recovery`.
- `gum profile test <path> --input fixture.json` — verify the profile's output
  shape before using it in automation.
- MCP clients recover `resource_link` payloads by calling `resources/read` on
  the emitted `gum://results/{hash}` URI. v0.1.x has no separate CLI
  `recover` command.

See `gum://help/profiles` for where the tee tree and secret live.

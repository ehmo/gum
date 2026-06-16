# Risor code execution and safety gates

Code mode lets the host client run a small Risor script against a fetched
upstream response *inside* GUM, so the model never sees the raw payload.
This is the dispatch path behind `gum.code` and the `code-mode` interface
kind on Tier A ops (spec §6.1).

## When to use it

Use code mode when:

1. The upstream response is large (>5 KiB) but only a small projection or
   aggregate is needed.
2. The projection logic is too irregular for the expression DSL — for
   example, "find the first message whose sender domain matches one of
   these three regexes, then return its body length".
3. The host client wants to avoid storing the raw payload in its context.

Do **not** use code mode for:

- Side effects (writes, deletes, network calls). The sandbox blocks all
  egress by default.
- Multi-step orchestration. Compose multiple `gum.read` / `gum.write` calls
  instead.

## Sandbox

The Risor interpreter runs with a strict allowlist:

- **Egress**: blocked by default. The active expression override may permit
  hosts via `egress_allow=["*.googleapis.com"]`; the global default
  allowlist is `["*.googleapis.com"]` only.
- **Filesystem**: read-only access to the upstream response body bound to
  `input`. No write access.
- **Time**: deterministic seeded random; wall-clock pinned to dispatch
  start to keep golden tests reproducible.
- **CPU**: 500 ms ceiling per invocation. Scripts that exceed the budget
  fail with `CODE_MODE_TIMEOUT` and the response is dropped.

## Script shape

```risor
// `input` is the upstream JSON response as a Risor value.
// `out` (assigned at the end) is what the host client sees.
let subjects = input.messages
  .filter(m => m.from.endswith("@example.com"))
  .map(m => m.subject);

out = {subjects: subjects, count: len(subjects)};
```

The final assignment to `out` is the projection. Anything else (including
prints) is discarded.

## Headless browser

For ops whose `binding.adapter_key` selects the headless-browser path, code
mode integrates with `gomoufox`, which drives the Firefox-based, anti-detect
**Camoufox** browser (chosen over plain Chrome/`chromedp` because Camoufox's
stealth profile evades bot detection on these unofficial-API surfaces). No
separate Chrome install is needed: run `gomoufox install` once to set up the
managed Python environment and the pinned Camoufox binary, or point
`GOMOUFOX_CAMOUFOX_PATH` at an existing Camoufox directory. Setup instructions
live in this topic because plugins also use this path — see the plugin
manifest's `system_requirements` field for the per-plugin contract.

## Quick commands

- `gum code <op> <args> --script=/path/to/projection.risor` — one-shot CLI.
- `gum code <op> <args> --stdin` — read the script from stdin.

## Errors

- `CODE_MODE_TIMEOUT` — script exceeded the 500 ms ceiling.
- `CODE_MODE_EGRESS_DENIED` — script attempted to call a host outside the
  active allowlist.
- `CODE_MODE_SYNTAX` — Risor parse error; line and column included in the
  envelope.

See `gum://help/field-masks` for a lighter-weight projection alternative.

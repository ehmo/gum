# Risor code execution and safety gates

Code mode runs a small Risor script inside GUM. The script drives catalog ops
itself through `gum_call` and `gum_parallel`, and only what it prints comes
back, so intermediate payloads never reach the model's context. This is the
dispatch path behind the `gum.code` meta-tool and the `gum code` CLI command
(spec §6.1).

## When to use it

1. A projection or aggregate over a large response, where only the summary
   needs to reach the model.
2. Several dependent calls, where each result decides the next. Round-tripping
   through the model would spend a turn per step.
3. A fan-out across many ops through `gum_parallel`, returning one combined
   answer.

Do not use code mode for:

- A projection the expression DSL already covers. See `gum://help/field-masks`
  for the cheaper path.
- Anything wanting a library, the filesystem, or a non-Google host. The
  sandbox has none of those.

Writes and destructive calls are not excluded. They are gated: see the
capability flags under Quick commands.

## Sandbox

The Risor v2 interpreter runs with nothing but the builtins GUM injects:

- **Filesystem and processes**: none. There is no file access and no
  `os/exec`.
- **Network**: no raw sockets. The only way out is `gum_http_get`, which
  refuses plain `http://`, refuses any host outside the egress allowlist
  (`*.googleapis.com` by default, extended by plugin-declared hosts),
  re-checks the allowlist after each redirect, refuses private, loopback,
  and link-local addresses, and caps the response body at 1 MiB.
- **CPU**: a ceiling of 100,000,000 VM instructions. A tight compute loop
  aborts there regardless of how fast the host is.
- **Wall clock**: 60 s, fixed. The step ceiling bounds compute; this bounds
  a script blocked on I/O.
- **Output**: 4096 bytes, cumulative across every `gum_print` and the
  script's return value together.

## Builtins

| Builtin | Purpose |
|---|---|
| `gum_call(op_id, args)` | Invoke a catalog op and return its result. |
| `gum_parallel(calls)` | Fan out a list of `{op_id, args}` concurrently. |
| `gum_print(value)` | Emit a value to the response body. |
| `gum_http_get(url)` | HTTPS GET to an allowlisted host. |
| `gum_confirm_destructive(op_id, key)` | Arm a one-shot destructive confirmation. |
| `gum_search(query)` | Catalog search. Still a stub inside code mode: it returns an empty list. |

Writes and destructive calls through `gum_call` need the matching capability
flag on the invocation, and a destructive call needs
`gum_confirm_destructive` first.

## Script shape

Only what `gum_print` writes reaches the caller. The script's return value is
charged against the same 4096-byte budget but is not part of the response, so
print what you want to read:

```risor
let labels = gum_call("gmail.users.labels.list", {"userId": "me"})
gum_print(labels)
```

Risor v2 declares with `let`. A Go-style `x := 1` is a parse error.

Risor v2 has no `for`, `while`, or `loop` keyword. Iterate with the stdlib:
`range()`, `list().each()`, `.map()`.

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

The script is the one positional argument, inline or as `@path/to/file.risor`:

- `gum code 'let n = 2 + 2; gum_print(n)'`
- `gum code @./script.risor`
- `gum code --allow-write @./script.risor` — authorise write-class
  `gum_call`s. Add `--yes` when stdin is not a terminal.
- `gum code --allow-destructive --destructive-budget=3 @./script.risor` — a
  budget of 1 to 20 is required with `--allow-destructive`. Narrow the blast
  radius further with `--destructive-scope=op_id[:resource_key]`.

## Errors

- `CODE_OUTPUT_LIMIT_EXCEEDED` — prints plus return value exceeded the
  output budget. The envelope carries `limit_bytes` and `printed_bytes`;
  the oversized value is never echoed back. A `gum_parallel` batch raises
  the same code before it dispatches when its input alone exceeds
  `code.output_limit_bytes` times the 8 workers; that envelope carries
  `limit_bytes` and `requested_bytes` instead.
- `REQUIRES_CONFIRMATION` — a write or destructive call without the
  capability flag, the consent, or a prior `gum_confirm_destructive`.
- `DESTRUCTIVE_BUDGET_EXCEEDED` — the script made more destructive calls
  than `--destructive-budget` allowed.
- `INVALID_ARGS` — a missing or malformed `source`, `language`, or
  destructive-envelope argument.

A parse error, the step ceiling, the wall-clock timeout, and an egress
refusal have no code of their own. Each arrives as `SERVICE_DOWN` carrying
the sandbox text, so read the message rather than branching on the code:
`sandbox: risor eval: parse error: ...`, `sandbox: step limit exceeded`,
`sandbox: script context deadline exceeded`, `EGRESS_HOST_DENIED`,
`EGRESS_PRIVATE_IP_DENIED`.

See `gum://help/field-masks` for a lighter-weight projection alternative.

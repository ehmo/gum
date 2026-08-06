# `gum code`

> Generated from `gum schema --json`. Do not edit this page by hand; run `make docs-commands`.

Run a Risor v2 script in the gum sandbox.

The sandbox is deliberately small: there is NO filesystem, os/exec, or raw
network access, and Risor v2 has NO for/while/loop keyword; iterate with the
stdlib instead (range(), list().each(), .map()).

Catalog access is provided through these injected builtins:

  gum_call(op_id, args)              Invoke a catalog op; returns its result.
                                     Destructive ops require a prior
                                     gum_confirm_destructive + --allow-destructive.
  gum_search(query)                  BM25-search the catalog; returns a list.
  gum_confirm_destructive(op_id, key) Arm a one-shot destructive confirmation.
  gum_parallel(calls)                Fan out a list of {op_id, args} concurrently.
  gum_print(value)                   Emit a value to the response body.
  gum_http_get(url)                  HTTPS GET to an allowlisted host.

Scripts may be passed inline or as @path/to/file.risor.

## Usage

```bash
gum code <script-or-@file> [flags]
```

## Parent

- [gum](gum.md)

## Arguments

| Name | Help |
| --- | --- |
| `script-or-@file` |  |

## Flags

| Flag | Type | Default | Help |
| --- | --- | --- | --- |
| `--allow-destructive` | `bool` | false | Authorise destructive sandbox ops |
| `--allow-write` | `bool` | false | Authorise sandbox writes |
| `--confirmed` | `bool` | false | Set the signed-confirmation flag for elevated sandbox ops |
| `--format` | `string` |  | Output format (toon\|json\|raw) |
| `--language` | `string` | risor | Sandbox language (only risor in v0.1.0) |
| `-o`<br>`--output` | `string` |  | Output format: json\|toon\|raw (raw script output remains raw) |
| `--timeout-sec` | `int` | 0 | Per-invocation timeout in seconds (0=default) |
| `--token` | `string` |  | Confirmation token returned by a prior elevated gum code attempt |
| `--log-format` | `string` | json | Log format: json\|text |
| `--log-level` | `string` | info | Log level: debug\|info\|warn\|error (overrides GUM_LOG_LEVEL) |
| `--profile` | `string` | default | Profile name to read/write config under |

## See also

- [gum](gum.md)
- [Command index](README.md)

---
title: Safety
description: "Risk gates, local credentials, confirmation tokens, and prompt boundaries in gum."
---

# Safety

`gum` is designed for agents that can make mistakes. The safety model is built
around local credentials, a small initial MCP surface, risk-specific invocation,
and explicit confirmation for destructive operations.

## Local credentials

OAuth refresh tokens, API keys, service-account config, and plugin credentials
are resolved on the host running gum. They are not copied into the MCP prompt.

Use stdin for OAuth client secrets:

```bash
printf '%s' "$GOOGLE_OAUTH_CLIENT_SECRET" \
  | gum auth use-oauth-client --client-id "$GOOGLE_OAUTH_CLIENT_ID" --secret-stdin
```

## Risk classes

| Class | CLI path | MCP path | Extra gate |
| --- | --- | --- | --- |
| Read | `gum read` | `gum.read` | Credential and scope checks |
| Write | `gum write --allow-write` | `gum.write` | Explicit write authorization |
| Destructive | `gum destructive --token ... --confirmed` | `gum.destructive` | Confirmation token |

`gum call` is available for direct dispatch and requires `--risk=read|write|destructive`.

## Sandboxed code

`gum code` runs Risor scripts with a small host API. The sandbox has no
filesystem, no `os/exec`, and no raw network access. Catalog calls still go
through dispatch.

```bash
gum code 'gum_print(gum_search("gmail labels"))'
gum code --allow-write @./script.risor
```

Use `--allow-write` or `--allow-destructive` only for scripts you have reviewed.

## Secrets in agent workflows

`gum` protects Google credentials it owns. Project secrets such as deploy keys
or service tokens still need a separate broker. For that workflow, use
[HASP](hasp.md) or another local secret broker rather than pasting values into
the prompt.

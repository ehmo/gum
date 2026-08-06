---
title: Sandbox
description: "Sandbox operations in gum's generated catalog."
service_group: "Internal"
---

# Sandbox

Sandbox has 1 operations in gum's generated catalog. Start with search when you know the task, use describe to inspect request fields and scopes, then dispatch through the command that matches the operation risk class.

| Count | Value |
| --- | --- |
| Family | Internal |
| Operations | 1 |
| Risk classes | 1 read |
| Auth strategies | 1 none |

## Start here

```bash
gum search "sandbox"
gum describe gum.code
gum read gum.code --args '{"fields":"id"}' --output json
```

## Auth

Auth strategies in this service: 1 none. Authenticate the strategy used by the operation you plan to call.

### No external auth

1. No external credential is required.
2. Inspect the operation shape before use:

```bash
gum describe gum.code
gum read gum.code --args '{"fields":"id"}' --output json
```

## Operations

| Operation | Risk | Auth | Summary |
| --- | --- | --- | --- |
| `gum.code` | `read` | `none` | Executes a Risor snippet that may call catalog ops via gum_call. |

## Next

- Use [API workflows](../api-workflows.md) for search, describe, invoke, and error handling.
- Use [Auth guides](../auth-guides/README.md) for service-specific Google setup.
- Use [Command index](../commands/README.md) for CLI flags and generated help.

---
title: Scholar
description: "Scholar operations in gum's generated catalog."
service_group: "Research and travel"
---

# Scholar

Scholar has 1 operations in gum's generated catalog. Start with search when you know the task, use describe to inspect request fields and scopes, then dispatch through the command that matches the operation risk class.

| Count | Value |
| --- | --- |
| Family | Research and travel |
| Operations | 1 |
| Risk classes | 1 read |
| Auth strategies | 1 plugin_managed |

## Start here

```bash
gum search "scholar"
gum describe scholar.search
gum read scholar.search --args '{"query":"<query>"}' --output json
```

## Auth

Auth strategies in this service: 1 plugin_managed. Authenticate the strategy used by the operation you plan to call.

### Plugin-managed auth

1. No Google OAuth client, API key, or service account is configured through gum for these operations.
2. Confirm the plugin-backed operation is available:

```bash
gum plugin list
gum describe scholar.search
```

3. Follow the plugin's upstream requirements, rate limits, and terms before calling it.
4. Verify with a read call:

```bash
gum read scholar.search --args '{"query":"<query>"}' --output json
```

## Operations

| Operation | Risk | Auth | Summary |
| --- | --- | --- | --- |
| `scholar.search` | `read` | `plugin_managed` | Search Google Scholar for academic papers, citations, and author profiles. Backed by the bundled google-scholar Shape 1 plugin. |

## Next

- Use [API workflows](../api-workflows.md) for search, describe, invoke, and error handling.
- Use [Auth guides](../auth-guides/README.md) for service-specific Google setup.
- Use [Command index](../commands/README.md) for CLI flags and generated help.

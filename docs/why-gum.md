---
title: Why gum
description: "The design tradeoffs behind gum's catalog-first CLI and MCP server."
---

# Why gum

Google APIs are large, uneven, and permission-heavy. An agent does not need
hundreds of methods in its prompt. It needs a way to find the right operation,
inspect the request shape, call through local credentials, and stop before a
risky side effect.

gum is built around that path. The first MCP roster stays small; the local
catalog supplies product coverage when a task needs it. The CLI uses the same
dispatcher, so terminal scripts and MCP clients hit the same auth resolver, risk
checks, output shaping, and audit paths.

## What this avoids

- Giant MCP tool lists that spend context before the task starts.
- Raw HTTP escape hatches that leave path construction, scopes, pagination, and
  delete safety to the caller.
- Token handling inside prompts. Google credentials stay on the host and are
  resolved from the selected profile.
- Separate implementations for CLI and agent workflows.

## Core choices

| Choice | Result |
| --- | --- |
| Catalog-first discovery | Use `gum search`, `gum describe`, and generated service pages to choose an operation before invoking it. |
| One dispatch kernel | CLI commands, MCP tools, and `gum.code` share the same execution path. |
| Profile-local credentials | `--profile` and `GUM_PROFILE` keep accounts, config, cache, and audit state separate. |
| Risk-specific commands | `gum read`, `gum write`, and `gum destructive` reject mismatched operation classes. |
| Local output shaping | Small JSON or TOON responses are easier for agents and scripts to parse. |
| Generated reference | `gum schema --json` drives command pages; the embedded catalog drives service pages. |

## When gum fits

Use gum when you need Google API access from a terminal, a script, or an MCP
client and you want the same local safety model in each place. It fits tasks
such as reading mail or files, creating drafts and calendar entries, updating
sheets, running Workspace admin checks, or giving an agent a bounded path to
long-tail Google APIs.

Use a direct Google client library when your application owns one fixed service
integration and you want typed application code instead of a catalog dispatch
surface. Use the Google Cloud Console or admin UI when the job is one-off
configuration rather than repeatable automation.

## What gum is not

`gum` is not a hosted Google integration, a replacement for Google Cloud IAM, or
a way around Google API quotas and consent. It does not grant access by itself.
Every call still depends on the Google account, Cloud project, enabled APIs,
OAuth scopes, service account, API key, or plugin credentials configured on the
local profile.

For setup details, start with [Auth](auth.md), [MCP server](mcp.md), and
[API workflows](api-workflows.md).

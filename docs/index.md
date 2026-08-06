---
title: Overview
permalink: /
description: "gum is a Go CLI and MCP stdio server for safer Google API access from agents, scripts, and terminals."
---

![gum logo](assets/gum-logo.png)

# Google APIs for agents and terminals

`gum` gives agents and terminal users a controlled path into Google APIs. It is
one Go binary, one local credential model, and one dispatch path for CLI calls,
MCP tool calls, and sandboxed code.

The surface is catalog-first. Instead of minting a top-level command for every
Google product, gum lets the caller search the embedded catalog, inspect the
operation contract, then dispatch through `gum read`, `gum write`, or
`gum destructive`.

## Try it

```bash
# Find Google API operations by task.
gum search "gmail messages"
gum describe gmail.users.messages.list

# Register a Google OAuth client, then authorize a small scope set.
printf '%s' "$GOOGLE_OAUTH_CLIENT_SECRET" \
  | gum auth use-oauth-client --client-id "$GOOGLE_OAUTH_CLIENT_ID" --secret-stdin
gum login --service gmail,calendar

# Make a read-class API call through the local profile.
gum read gmail.users.messages.list --args '{"userId":"me","maxResults":5}' --output json

# Install agent skills and MCP config after previewing the write plan.
gum setup --dry-run
gum setup --target codex --features skills,mcp --yes
```

## What gum covers

gum v1.0 ships with 222 catalog operations across 32 services. It covers Gmail,
Calendar, Drive, Docs, Sheets, Slides, Tasks, Admin, Vault, Chat, Meet,
Classroom, Forms, Apps Script, People, Photos, YouTube, Search Console, Google
Ads, Maps, Custom Search, and bundled plugin services such as Flights, Scholar,
Patents, Trends, and YouTube transcripts.

The [operations by service](services/) pages are generated from the embedded
catalog. They are the product-level entry point. The [command index](commands/)
is generated from `gum schema --json` and documents the CLI surface.

## What gum does

- Keeps the MCP tool roster small, then loads operation details only after
  `gum.search_apis` or `gum search` finds a candidate.
- Resolves Google credentials from a local profile instead of putting refresh
  tokens, service-account keys, API keys, or Ads developer tokens into prompts.
- Sends read, write, and destructive calls through separate commands and MCP
  tools. The dispatcher rejects a call when the operation risk class and command
  do not match.
- Shapes large Google responses before they hit an agent context window. Field
  masks, compact formats, and output profiles are part of the normal workflow.
- Installs agent skills and MCP client config for Codex, Claude, Cursor, and
  Gemini after showing the files it plans to write.

## Pick your path

- First install: [Install](install.md), then [Quickstart](quickstart.md).
- Product lookup: [Operations by service](services/) and [Service coverage](service-coverage.md).
- Auth setup: [Auth](auth.md) and the [service-specific auth guides](auth-guides/).
- Agent setup: [MCP server](mcp.md), [Agent setup](agent-setup.md), and [Automation](automation.md).
- Safe calls: [API workflows](api-workflows.md), [Safety](safety.md), and [Output shaping](output.md).
- CLI flags: generated [Command index](commands/).
- Local state and tests: [Paths and profiles](paths.md), then [Live testing](live-testing.md).
- Plugins: [Plugins](plugins.md) and the [plugin contract](plugin-contract.md).

## Project

`gum` is open source under the [MIT license](https://github.com/ehmo/gum/blob/main/LICENSE).
It is not affiliated with Google. Use it only with Google accounts, projects,
data, and APIs you are authorized to operate.

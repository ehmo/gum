---
title: MCP server
description: "Run gum as an MCP stdio server and expose Google API tools to agent clients."
---

# MCP server

`gum mcp --stdio` runs the MCP server. The public release supports stdio
transport.

```bash
gum mcp --stdio
```

Most users should let `gum setup` write client config instead of editing MCP
files by hand:

```bash
gum setup --dry-run
gum setup --target codex --features skills,mcp --yes
```

## Tool shape

The MCP surface starts small. Core meta-tools search, describe, and invoke the
catalog, while convenience tools cover common Gmail, Drive, Calendar, Docs,
Sheets, Slides, Tasks, and Flights paths.

The stable path for long-tail API access is:

```text
gum.search_apis -> gum.describe_op -> gum.read / gum.write / gum.destructive
```

The CLI mirrors the same model with `gum search`, `gum describe`, `gum read`,
`gum write`, and `gum destructive`.

## Safety boundary

MCP clients do not receive raw Google tokens. They call the local gum server,
and gum resolves credentials from the selected profile on that host. Writes and
destructive calls still pass through the same risk gate as CLI calls.

Use [Safety](safety.md) before exposing gum to unattended agents.

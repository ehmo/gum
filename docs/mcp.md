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

## Meta-tool tuning

`gum.search_apis` and `gum.describe_op` shape their own output. Five
per-profile config keys move the limits:

| Key | Default | Effect |
| --- | --- | --- |
| `meta_tools.search_apis.k` | 5 | Result count when the caller sends no `k`. Range 1-20. |
| `meta_tools.search_apis.truncate_strings.default_chars` | 120 | Character limit on summary-class fields in a result row. Range 60-400. |
| `meta_tools.search_apis.collapse_arrays.max_items` | binds `k` | Collapse threshold for the results array. Range 1-50. |
| `meta_tools.describe_op.max_variants` | 5 | Collapse threshold for the `variants[]` array. Range 1-50. |
| `meta_tools.describe_op.max_chars` | 400 | Character limit on `gum.describe_op` string fields. Range 100-2000. |

```bash
gum config set meta_tools.describe_op.max_chars=1200
```

A value outside its range is clamped and logged; an unparseable value falls
back to the default. These keys do not rebind an
[expression profile](expression-profile-dsl.md) and do not change the
registered tool schemas.

## Safety boundary

MCP clients do not receive raw Google tokens. They call the local gum server,
and gum resolves credentials from the selected profile on that host. Writes and
destructive calls still pass through the same risk gate as CLI calls.

Use [Safety](safety.md) before exposing gum to unattended agents.

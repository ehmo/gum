---
title: Agent setup
description: "Install gum skills and MCP config for Codex, Claude, Cursor, and Gemini."
---

# Agent setup

`gum setup` installs the files an agent client needs: skill text, MCP config, or
both. The command supports Codex, Claude, Cursor, Gemini, and `all`.

Preview first:

```bash
gum setup --dry-run
gum agents install --target all --features skills,mcp --dry-run --format json
```

Apply for one client:

```bash
gum setup --target codex --features skills,mcp --yes
```

Use `--scope project` for repo-local config when the agent should use gum only
inside one workspace.

## Reruns

Reruns are meant to be predictable: MCP config is merged, managed blocks are
replaced, and skill files are left alone unless `--force` is passed.

## Skills

The bundled skills document how an agent should search the catalog, describe an
operation, choose read/write/destructive tools, and keep credentials out of the
prompt.

```bash
gum skills list
gum skills show core
gum skills export --out ./gum-skills
```

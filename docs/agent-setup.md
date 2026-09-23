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

## Recorded transcripts

The repository keeps five captures under `docs/agent-contracts/`. Each one is
the stdout of a single command, byte for byte, so you can diff your own run
against a known-good result. Regenerate any of them with the command that
produced it:

| File | Command |
| --- | --- |
| `cli-skills-list.json` | `gum skills list --format=json` |
| `cli-skills-core.json` | `gum skills show core --format=json` |
| `cli-skills-hasp.json` | `gum skills show hasp --format=json` |
| `cli-skills-mcp.json` | `gum skills show mcp --format=json` |
| `cli-agents-install.json` | `HOME=/private/tmp/gum-home gum agents install --dry-run --format=json --target all --features skills,mcp` |

The skill bodies are embedded in the binary, so editing one moves its `sha256`
and `bytes`. `TestSkillsFixturesMatchLiveOutput` and
`TestAgentsInstallFixtureMatchesLiveOutput` re-run each command and fail when a
capture no longer matches, and the failure prints the command to rerun. The
install capture records absolute paths under the `HOME` it was taken with; the
gate ignores that prefix and compares the paths below it.

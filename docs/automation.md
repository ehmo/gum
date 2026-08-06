---
title: Automation
description: "Use gum in scripts, CI, and agent workflows without relying on interactive prompts."
---

# Automation

For scripts, treat stdout as data and stderr as diagnostics. Prefer explicit
formats and exact operation IDs.

```bash
gum search "drive files" --format json
gum describe drive.files.list
gum read drive.files.list \
  --args '{"pageSize":10,"fields":"files(id,name,mimeType),nextPageToken"}' \
  --output json
```

## Runtime schema

`gum schema --json` prints the command tree, arguments, aliases, and flags.
Docs use that output to generate the [command index](commands/).

```bash
gum schema --json > gum-schema.json
```

Use the schema when a wrapper needs to discover supported subcommands or detect
flag drift after an upgrade.

## Non-interactive writes

Read calls do not need write authorization. Write and destructive calls do.

```bash
gum write sheets.spreadsheets.values.update \
  --allow-write \
  --args '{"spreadsheetId":"<id>","range":"Sheet1!A1","valueInputOption":"RAW"}'
```

Destructive calls require a confirmation token. Do not fake this path in CI;
generate the token through the command flow that reports the exact operation and
resource being confirmed.

## Exit behavior

Dispatch errors are returned as structured envelopes when the command reaches
the dispatcher. CLI argument errors fail before dispatch and print a normal
command error.

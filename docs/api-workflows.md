---
title: API workflows
description: "Find, inspect, and invoke long-tail Google API operations with gum."
---

# API workflows

The normal gum workflow is search, describe, then invoke. That keeps a caller
from guessing operation names, request fields, OAuth scopes, or risk class.

If you are starting from a Google product instead of a task, use
[Operations by service](services/) first. Those pages are generated from the
same catalog as `gum search`.

## Find an operation

Start with task words:

```bash
gum search "drive files modified recently" --format json
gum search "gmail create draft"
gum search "search console inspect url"
```

Search returns catalog operations. Pick the operation whose service, title, and
summary match the task. For wrappers and agents, store the exact `op_id` after
the search step; do not pass natural language into an invocation command.

## Inspect the contract

Describe the operation before you call it:

```bash
gum describe drive.files.list
```

The description shows variants, required arguments, OAuth scopes, auth strategy,
output profile, and risk class. Treat the selected variant's risk class as
authoritative.

## Build the request

Use JSON for structured arguments:

```bash
gum read drive.files.list \
  --args '{"pageSize":10,"fields":"files(id,name,mimeType),nextPageToken"}' \
  --output json
```

Use `fields` when the upstream REST method supports partial response. Google
then omits the unused fields before gum receives the payload.

## Match the risk class

Read operations use `gum read`:

```bash
gum read calendar.events.list \
  --args '{"calendarId":"primary","maxResults":5}' \
  --output json
```

Write operations require the write command and an explicit write flag:

```bash
gum write sheets.spreadsheets.values.update \
  --allow-write \
  --args '{"spreadsheetId":"<id>","range":"Sheet1!A1","valueInputOption":"RAW"}'
```

Destructive operations use a two-step confirmation flow. Run the command once to
receive the confirmation envelope, review the operation and target, then retry
with `--confirmed --token <confirmation_token>`.

```bash
gum destructive <op_id> --args '<json>'
gum destructive <op_id> --args '<json>' --confirmed --token '<confirmation_token>'
```

## Keep responses small

Large Google responses can burn an agent context window quickly. Keep list and
get calls narrow unless the task needs full records.

```bash
gum read gmail.users.messages.list \
  --args '{"userId":"me","maxResults":10,"fields":"messages(id,threadId),nextPageToken"}' \
  --output json
```

See [Field masks](field-masks.md) and [Output shaping](output.md) for response
control.

## Use the same flow over MCP

MCP clients use the same sequence:

```text
gum.search_apis -> gum.describe_op -> gum.read / gum.write / gum.destructive
```

Convenience tools exist for common Gmail, Drive, Calendar, Docs, Sheets, Slides,
Tasks, and Flights paths. Long-tail access should still go through search and
describe first.

## Troubleshooting

- `OP_NOT_FOUND` means the operation ID is not in the resolved catalog for the
  active profile.
- `RISK_TOOL_MISMATCH` means the selected variant belongs to a different risk
  class than the command or MCP tool you used.
- `AUTH_REQUIRED` means no usable credential was found for the operation.
- `SCOPE_MISSING` means the credential exists but does not cover one or more
  required OAuth scopes.
- Google API errors such as `accessNotConfigured` and `insufficientPermissions`
  come from the upstream project or account setup.

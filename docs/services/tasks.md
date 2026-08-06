---
title: Tasks
description: "Tasks operations in gum's generated catalog."
service_group: "Workspace communication"
---

# Tasks

Tasks has 12 operations in gum's generated catalog. Start with search when you know the task, use describe to inspect request fields and scopes, then dispatch through the command that matches the operation risk class.

| Count | Value |
| --- | --- |
| Family | Workspace communication |
| Operations | 12 |
| Risk classes | 2 destructive, 4 read, 6 write |
| Auth strategies | 12 byo_oauth |

## Start here

```bash
gum search "tasks"
gum describe tasks.tasklists.get
gum read tasks.tasklists.get --args '{"tasklist":"<tasklist>"}' --output json
```

For write-class operations, gum requires the write command and an explicit write gate:

```bash
gum describe tasks.tasklists.insert
gum write tasks.tasklists.insert --allow-write --args '{"title":"<title>"}'
```

For destructive operations, run the call once for a confirmation envelope, review the target, then retry with the returned token:

```bash
gum destructive tasks.tasklists.delete --args '{"tasklist":"<tasklist>"}'
gum destructive tasks.tasklists.delete --args '{"tasklist":"<tasklist>"}' --confirmed --token '<confirmation_token>'
```

## Auth

Auth strategies in this service: 12 byo_oauth. Authenticate the strategy used by the operation you plan to call.

### Bring-your-own OAuth

1. In Google Cloud, enable Google Tasks API.
2. Configure the OAuth consent screen. Add your Google account as a test user when the app is still in testing mode.
3. Create an OAuth client ID with application type `Desktop app`.
4. Add the scopes this service needs to the consent screen.
5. Store the client in gum:

```bash
printf '%s' "$GOOGLE_OAUTH_CLIENT_SECRET" \
  | gum auth use-oauth-client --client-id "$GOOGLE_OAUTH_CLIENT_ID" --secret-stdin
```

6. Authorize this service:

```bash
gum login --service tasks
```

7. Verify the grant before dispatch:

```bash
gum auth status --scopes tasks,tasks.readonly
gum describe tasks.tasklists.get
```

Scopes used by these operations:

- `https://www.googleapis.com/auth/tasks`
- `https://www.googleapis.com/auth/tasks.readonly`

Service setup notes: [Tasks auth guide](../auth-guides/tasks.md).

## Operations

| Operation | Risk | Auth | Summary |
| --- | --- | --- | --- |
| `tasks.tasklists.delete` | `destructive` | `byo_oauth` | Delete a task list and all its tasks. Destructive — requires confirmation per §6.1. |
| `tasks.tasklists.get` | `read` | `byo_oauth` | Fetch a task list by id. |
| `tasks.tasklists.insert` | `write` | `byo_oauth` | Create a new task list (args.body: title). |
| `tasks.tasklists.list` | `read` | `byo_oauth` | List the authenticated user's task lists. |
| `tasks.tasklists.update` | `write` | `byo_oauth` | Rename a task list (args.body: title). |
| `tasks.tasks.clear` | `write` | `byo_oauth` | Clear all completed tasks from a task list (they become hidden). |
| `tasks.tasks.delete` | `destructive` | `byo_oauth` | Delete a task from a task list. Destructive — requires confirmation per §6.1. |
| `tasks.tasks.get` | `read` | `byo_oauth` | Fetch a single task by id. |
| `tasks.tasks.insert` | `write` | `byo_oauth` | Create a new task on the specified task list (args.body: title, notes, due, etc.). |
| `tasks.tasks.list` | `read` | `byo_oauth` | List tasks in the specified task list. |
| `tasks.tasks.move` | `write` | `byo_oauth` | Move a task to another position or parent within its list (reorder / nest). |
| `tasks.tasks.update` | `write` | `byo_oauth` | Update a task (mark complete via status=completed, edit title/notes/due). |

## Next

- Use [API workflows](../api-workflows.md) for search, describe, invoke, and error handling.
- Use [Auth guides](../auth-guides/README.md) for service-specific Google setup.
- Use [Command index](../commands/README.md) for CLI flags and generated help.

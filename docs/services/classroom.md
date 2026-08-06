---
title: Classroom
description: "Classroom operations in gum's generated catalog."
service_group: "People and education"
---

# Classroom

Classroom has 10 operations in gum's generated catalog. Start with search when you know the task, use describe to inspect request fields and scopes, then dispatch through the command that matches the operation risk class.

| Count | Value |
| --- | --- |
| Family | People and education |
| Operations | 10 |
| Risk classes | 1 destructive, 6 read, 3 write |
| Auth strategies | 10 byo_oauth |

## Start here

```bash
gum search "classroom"
gum describe classroom.courses.announcements.list
gum read classroom.courses.announcements.list --args '{"courseId":"<courseId>"}' --output json
```

For write-class operations, gum requires the write command and an explicit write gate:

```bash
gum describe classroom.courses.courseWork.create
gum write classroom.courses.courseWork.create --allow-write --args '{"courseId":"<courseId>"}'
```

For destructive operations, run the call once for a confirmation envelope, review the target, then retry with the returned token:

```bash
gum destructive classroom.courses.delete --args '{"id":"<id>"}'
gum destructive classroom.courses.delete --args '{"id":"<id>"}' --confirmed --token '<confirmation_token>'
```

## Auth

Auth strategies in this service: 10 byo_oauth. Authenticate the strategy used by the operation you plan to call.

### Bring-your-own OAuth

1. In Google Cloud, enable Google Classroom API.
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
gum login --service classroom
```

7. Verify the grant before dispatch:

```bash
gum auth status --scopes classroom.announcements.readonly,classroom.courses,classroom.courses.readonly,classroom.coursework.students,classroom.coursework.students.readonly,classroom.rosters.readonly
gum describe classroom.courses.announcements.list
```

Scopes used by these operations:

- `https://www.googleapis.com/auth/classroom.announcements.readonly`
- `https://www.googleapis.com/auth/classroom.courses`
- `https://www.googleapis.com/auth/classroom.courses.readonly`
- `https://www.googleapis.com/auth/classroom.coursework.students`
- `https://www.googleapis.com/auth/classroom.coursework.students.readonly`
- `https://www.googleapis.com/auth/classroom.rosters.readonly`

Service setup notes: [Classroom auth guide](../auth-guides/classroom-forms-meet-script.md).

## Operations

| Operation | Risk | Auth | Summary |
| --- | --- | --- | --- |
| `classroom.courses.announcements.list` | `read` | `byo_oauth` | List the announcements posted to a course stream. |
| `classroom.courses.courseWork.create` | `write` | `byo_oauth` | Create a coursework item (assignment) in a course (args.body: title, workType, …). |
| `classroom.courses.courseWork.get` | `read` | `byo_oauth` | Fetch a single coursework item by id. |
| `classroom.courses.courseWork.list` | `read` | `byo_oauth` | List the coursework (assignments) in a course. |
| `classroom.courses.create` | `write` | `byo_oauth` | Create a new course (args.body: name, ownerId, section). |
| `classroom.courses.delete` | `destructive` | `byo_oauth` | Delete a course by id. Destructive — requires confirmation per §6.1. |
| `classroom.courses.get` | `read` | `byo_oauth` | Fetch a course by id. |
| `classroom.courses.list` | `read` | `byo_oauth` | List courses the caller can access (filter by studentId/teacherId/courseStates). |
| `classroom.courses.students.list` | `read` | `byo_oauth` | List the students enrolled in a course. |
| `classroom.courses.update` | `write` | `byo_oauth` | Replace a course by id (args.body). |

## Next

- Use [API workflows](../api-workflows.md) for search, describe, invoke, and error handling.
- Use [Auth guides](../auth-guides/README.md) for service-specific Google setup.
- Use [Command index](../commands/README.md) for CLI flags and generated help.

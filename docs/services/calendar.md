---
title: Calendar
description: "Calendar operations in gum's generated catalog."
service_group: "Workspace communication"
---

# Calendar

Calendar has 31 operations in gum's generated catalog. Start with search when you know the task, use describe to inspect request fields and scopes, then dispatch through the command that matches the operation risk class.

| Count | Value |
| --- | --- |
| Family | Workspace communication |
| Operations | 31 |
| Risk classes | 4 destructive, 12 read, 15 write |
| Auth strategies | 31 byo_oauth |

## Start here

```bash
gum search "calendar events"
gum describe calendar.acl.get
gum read calendar.acl.get --args '{"calendarId":"primary","ruleId":"<ruleId>"}' --output json
```

For write-class operations, gum requires the write command and an explicit write gate:

```bash
gum describe calendar.acl.insert
gum write calendar.acl.insert --allow-write --args '{"calendarId":"primary"}'
```

For destructive operations, run the call once for a confirmation envelope, review the target, then retry with the returned token:

```bash
gum destructive calendar.acl.delete --args '{"calendarId":"primary","ruleId":"<ruleId>"}'
gum destructive calendar.acl.delete --args '{"calendarId":"primary","ruleId":"<ruleId>"}' --confirmed --token '<confirmation_token>'
```

## Auth

Auth strategies in this service: 31 byo_oauth. Authenticate the strategy used by the operation you plan to call.

### Bring-your-own OAuth

1. In Google Cloud, enable Google Calendar API.
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
gum login --service calendar
```

7. Verify the grant before dispatch:

```bash
gum auth status --scopes calendar,calendar.events,calendar.readonly,calendar.settings.readonly
gum describe calendar.acl.get
```

Scopes used by these operations:

- `https://www.googleapis.com/auth/calendar`
- `https://www.googleapis.com/auth/calendar.events`
- `https://www.googleapis.com/auth/calendar.readonly`
- `https://www.googleapis.com/auth/calendar.settings.readonly`

Service setup notes: [Calendar auth guide](../auth-guides/calendar.md).

## Operations

| Operation | Risk | Auth | Summary |
| --- | --- | --- | --- |
| `calendar.acl.delete` | `destructive` | `byo_oauth` | Permanently remove an access-control rule from a calendar. |
| `calendar.acl.get` | `read` | `byo_oauth` | Get a single access-control rule by id. |
| `calendar.acl.insert` | `write` | `byo_oauth` | Add an access-control rule to a calendar (share with user, group, or domain). |
| `calendar.acl.list` | `read` | `byo_oauth` | List the access-control rules attached to a calendar. |
| `calendar.acl.patch` | `write` | `byo_oauth` | Partial-update an access-control rule. |
| `calendar.acl.update` | `write` | `byo_oauth` | Replace an access-control rule (e.g. change role from reader to writer). |
| `calendar.calendarList.delete` | `write` | `byo_oauth` | Remove a calendar from the authenticated user's calendar list (unsubscribe). The underlying calendar is unaffected. |
| `calendar.calendarList.get` | `read` | `byo_oauth` | Get an entry from the authenticated user's calendar list. |
| `calendar.calendarList.insert` | `write` | `byo_oauth` | Add an existing calendar to the authenticated user's calendar list (subscribe). |
| `calendar.calendarList.list` | `read` | `byo_oauth` | Returns the calendars on the user's calendar list. |
| `calendar.calendarList.patch` | `write` | `byo_oauth` | Partial-update a calendar-list entry. |
| `calendar.calendarList.update` | `write` | `byo_oauth` | Replace a calendar-list entry (color, summary override, notification settings). |
| `calendar.calendars.clear` | `destructive` | `byo_oauth` | Delete all events from the primary calendar (the calendar itself is preserved). |
| `calendar.calendars.delete` | `destructive` | `byo_oauth` | Permanently delete a secondary calendar (primary cannot be deleted). |
| `calendar.calendars.get` | `read` | `byo_oauth` | Fetch metadata for the specified calendar (summary, timezone, location). |
| `calendar.calendars.insert` | `write` | `byo_oauth` | Create a secondary calendar owned by the authenticated user. |
| `calendar.calendars.patch` | `write` | `byo_oauth` | Partial-update a calendar's metadata. |
| `calendar.calendars.update` | `write` | `byo_oauth` | Full replacement of a calendar's metadata. |
| `calendar.colors.get` | `read` | `byo_oauth` | Return the set of color definitions for calendars and events. |
| `calendar.events.delete` | `destructive` | `byo_oauth` | Permanently delete an event from the specified calendar. |
| `calendar.events.get` | `read` | `byo_oauth` | Fetch a single event by id from the specified calendar. |
| `calendar.events.insert` | `write` | `byo_oauth` | Create an event on the specified calendar. JSON request body required (args.body): summary, start, end, attendees, etc. |
| `calendar.events.instances` | `read` | `byo_oauth` | List instances of a recurring event between optional time bounds. |
| `calendar.events.list` | `read` | `byo_oauth` | List events on the specified calendar. |
| `calendar.events.move` | `write` | `byo_oauth` | Move an event to a different calendar (organizer transfer). |
| `calendar.events.patch` | `write` | `byo_oauth` | Partial-update an existing event. Only fields included in the body are changed. |
| `calendar.events.quickAdd` | `write` | `byo_oauth` | Parse a free-form text string into an event (e.g. 'Lunch Tue 12pm'). |
| `calendar.events.update` | `write` | `byo_oauth` | Update an existing event on the specified calendar by full replacement. JSON request body required (args.body): the entire event resource. |
| `calendar.freebusy.query` | `read` | `byo_oauth` | Return free/busy intervals for a set of calendars over a time range. |
| `calendar.settings.get` | `read` | `byo_oauth` | Return a single named calendar setting (e.g. 'timezone'). |
| `calendar.settings.list` | `read` | `byo_oauth` | Return the authenticated user's calendar settings (locale, timezone, etc.). |

## Next

- Use [API workflows](../api-workflows.md) for search, describe, invoke, and error handling.
- Use [Auth guides](../auth-guides/README.md) for service-specific Google setup.
- Use [Command index](../commands/README.md) for CLI flags and generated help.

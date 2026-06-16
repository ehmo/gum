# Calendar quickstart

List events and calendars through `gum`. Calendar ops resolve to the typed
`calendar/v3` REST SDK; auth flows through the same chain as every Workspace
op (spec §7).

## Auth setup (one-time)

```
printf '%s' '<client_secret>' | gum auth use-oauth-client \
  --client-id <client_id> --secret-stdin
gum auth login --scope https://www.googleapis.com/auth/calendar.readonly
```

Required scopes for the v0.1.0 Calendar roster:

| Op                                  | Scope                                                |
|-------------------------------------|------------------------------------------------------|
| `calendar.calendarList.list`        | `https://www.googleapis.com/auth/calendar.readonly`  |
| `calendar.events.list`              | `https://www.googleapis.com/auth/calendar.readonly`  |

The narrower `calendar.events.readonly` scope is sufficient for `events.list`
alone; use the broader `calendar.readonly` when both ops are in play.

## First op: list your calendars

```
gum read calendar.calendarList.list
```

Returns every calendar the principal can see — including holidays, contacts'
birthdays, and any explicitly subscribed feeds. Pass
`fields="items(id,summary,primary)"` to project to just the IDs you need.

## Common patterns

**Today's events on the primary calendar**
```
gum read calendar.events.list calendarId=primary \
   timeMin="$(date -u +%Y-%m-%dT00:00:00Z)" \
   timeMax="$(date -u -v+1d +%Y-%m-%dT00:00:00Z)" \
   singleEvents=true orderBy=startTime
```

`singleEvents=true` expands recurring events to individual instances.
`orderBy=startTime` requires `singleEvents=true`; the API rejects the combo
otherwise.

**Search by free-text**
```
gum read calendar.events.list calendarId=primary q="standup" \
   timeMin="$(date -u +%Y-%m-%dT00:00:00Z)" singleEvents=true
```

**Read a specific event** (not in v0.1.0 roster — falls through to raw-HTTP)
```
gum http get calendar/v3/calendars/primary/events/<event_id>
```

## Output shapes you'll see

- `calendarList.list` → `{items: [{id, summary, primary, accessRole, ...}]}`
- `events.list`       → `{items: [{id, summary, start, end, attendees, ...}], nextPageToken}`

Each event's `start`/`end` is either `{date: "2026-05-24"}` (all-day) or
`{dateTime: "2026-05-24T10:00:00-07:00", timeZone: "America/Los_Angeles"}`
(timed). Always check both shapes when rendering.

`gum search calendar` returns every op_id in the indexed Calendar surface with
ranking. `gum describe calendar.events.list` shows the full ABI.

## Time-range tips

`timeMin` and `timeMax` are RFC3339 timestamps. Common gotchas:

- Both bounds must include an explicit timezone offset (`Z` for UTC, or
  `±HH:MM`). Naked datetimes are rejected with `INVALID_ARGUMENT`.
- `timeMax` is exclusive; the API never returns an event whose start equals
  `timeMax`.
- Recurring events without `singleEvents=true` return one row per series with
  a `recurrence[]` RRULE, not per-instance dates.

## Pagination

```
TOK=
while :; do
  PAGE=$(gum read calendar.events.list calendarId=primary \
           timeMin=2026-01-01T00:00:00Z pageToken=$TOK format=json)
  echo "$PAGE" | jq '.items[].summary'
  TOK=$(echo "$PAGE" | jq -r '.nextPageToken // empty')
  [ -z "$TOK" ] && break
done
```

## Errors

- `AUTH_SCOPE_MISSING` — your stored grant lacks `calendar.readonly`. Re-run
  `gum auth login --scope https://www.googleapis.com/auth/calendar.readonly`
  with the broader scope.
- `INVALID_ARGUMENT` — usually `timeMin` without a timezone, or `orderBy`
  without `singleEvents=true`.
- `NOT_FOUND` — `calendarId` doesn't exist, or the principal isn't subscribed
  to it. Try `calendarList.list` first to enumerate what's visible.
- `RATE_LIMITED` — Calendar returned 429. The dispatcher honours `Retry-After`.

## Related

- `gum://help/auth` — full credential chain
- `gum://help/field-masks` — projection grammar
- `gum://help/toon-format` — output encoding

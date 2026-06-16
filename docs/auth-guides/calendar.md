# Calendar

Enable the Google Calendar API in Google Cloud. Add Calendar scopes to the
OAuth consent screen.

Common scopes:

- `https://www.googleapis.com/auth/calendar.readonly`
- `https://www.googleapis.com/auth/calendar.events`
- `https://www.googleapis.com/auth/calendar`
- `https://www.googleapis.com/auth/calendar.settings.readonly`

Setup:

```shell
gum login --service calendar
gum read calendar.calendarList.list --args '{"maxResults":5}'
```

Use `calendar.readonly` for list and get operations. Use `calendar.events` or
`calendar` only for event writes.

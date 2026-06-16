# Google Auth Guides

gum v1 requires your own Google credentials. The default path is a Google
Desktop OAuth client that you create in your Google Cloud project.

## Universal BYO OAuth Setup

1. Open Google Cloud Console.
2. Create or select a project.
3. Configure the OAuth consent screen.
4. Add yourself as a test user if the app is in testing mode.
5. Create credentials: `OAuth client ID` with application type `Desktop app`.
6. Enable each API you plan to call.
7. Store the OAuth client in gum:

```shell
printf '%s' "$GOOGLE_OAUTH_CLIENT_SECRET" \
  | gum auth use-oauth-client --client-id "$GOOGLE_OAUTH_CLIENT_ID" --secret-stdin
```

8. Authorize only the services you need:

```shell
gum login --service gmail,calendar
gum doctor
```

Use `gum describe <op_id>` to inspect an endpoint before calling it. The
catalog entry shows the auth strategy, scopes, risk class, and example args.

## Supported API Guides

| API family | Guide | Auth path |
| --- | --- | --- |
| Gmail | [gmail.md](gmail.md) | BYO OAuth |
| Calendar | [calendar.md](calendar.md) | BYO OAuth |
| Drive | [drive.md](drive.md) | BYO OAuth |
| Docs, Sheets, Slides | [docs-sheets-slides.md](docs-sheets-slides.md) | BYO OAuth |
| Tasks | [tasks.md](tasks.md) | BYO OAuth |
| Search Console | [search-console.md](search-console.md) | BYO OAuth |
| YouTube Data API | [youtube.md](youtube.md) | BYO OAuth |
| People and Contacts | [people.md](people.md) | BYO OAuth |
| Photos Library | [photos-library.md](photos-library.md) | BYO OAuth plus Photos app setup |
| Chat | [chat.md](chat.md) | BYO OAuth plus Chat app setup |
| Classroom, Forms, Meet, Apps Script | [classroom-forms-meet-script.md](classroom-forms-meet-script.md) | BYO OAuth |
| Admin, Admin Reports, Cloud Identity, Groups Settings, Vault | [admin-cloud-vault.md](admin-cloud-vault.md) | BYO OAuth plus Workspace admin privileges |
| Google Ads | [google-ads.md](google-ads.md) | BYO OAuth plus developer token |
| Maps and Custom Search | [maps-custom-search.md](maps-custom-search.md) | API key |

## Scope Reference

These are the OAuth scopes present in the embedded v1 catalog for BYO OAuth
variants. Consent-screen scope names in Google Cloud must match the scopes you
request with `gum login`.

| Service | Scopes |
| --- | --- |
| admin | `admin.directory.group`, `admin.directory.group.member`, `admin.directory.group.member.readonly`, `admin.directory.group.readonly`, `admin.directory.user`, `admin.directory.user.readonly` |
| adminreports | `admin.reports.audit.readonly`, `admin.reports.usage.readonly` |
| calendar | `calendar`, `calendar.events`, `calendar.readonly`, `calendar.settings.readonly` |
| chat | `chat.memberships.readonly`, `chat.messages`, `chat.messages.readonly`, `chat.spaces.readonly` |
| classroom | `classroom.announcements.readonly`, `classroom.courses`, `classroom.courses.readonly`, `classroom.coursework.students`, `classroom.coursework.students.readonly`, `classroom.rosters.readonly` |
| cloudidentity | `cloud-identity.groups.readonly` |
| docs | `documents`, `documents.readonly` |
| drive | `drive`, `drive.readonly` |
| forms | `forms.body`, `forms.body.readonly`, `forms.responses.readonly` |
| gmail | `mail.google.com/`, `gmail.compose`, `gmail.labels`, `gmail.modify`, `gmail.readonly`, `gmail.send`, `gmail.settings.basic` |
| googleads | `adwords` |
| groupssettings | `apps.groups.settings` |
| indexing | `indexing` |
| meet | `meetings.space.created`, `meetings.space.readonly` |
| people | `contacts`, `contacts.readonly` |
| photoslibrary | `photoslibrary.appendonly`, `photoslibrary.readonly.appcreateddata` |
| script | `script.deployments`, `script.projects`, `script.projects.readonly` |
| searchconsole | `webmasters`, `webmasters.readonly` |
| sheets | `spreadsheets`, `spreadsheets.readonly` |
| slides | `presentations`, `presentations.readonly` |
| tasks | `tasks`, `tasks.readonly` |
| vault | `ediscovery`, `ediscovery.readonly` |
| youtube | `youtube`, `youtube.readonly` |

`gum login` accepts short scope names such as `gmail.readonly` and expands them
to full Google OAuth URLs. `gmail.metadata` is intentionally omitted from the
recommended login set when broader Gmail read scopes are present because it
blocks full message reads.

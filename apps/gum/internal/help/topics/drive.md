# Drive quickstart

Search, fetch, and share Google Drive files through `gum`. Drive ops resolve to
the typed `drive/v3` REST SDK; auth flows through the same chain as every
Workspace op (spec §7).

## Auth setup (one-time)

```
printf '%s' '<client_secret>' | gum auth use-oauth-client \
  --client-id <client_id> --secret-stdin
gum auth login --scope https://www.googleapis.com/auth/drive.readonly
```

Required scopes for the v0.1.0 Drive roster:

| Op                                  | Scope                                                |
|-------------------------------------|------------------------------------------------------|
| `drive.files.list`/`.get`           | `https://www.googleapis.com/auth/drive.readonly`     |
| `drive.permissions.create`          | `https://www.googleapis.com/auth/drive`              |

The metadata-only mode (`drive.metadata.readonly`) is accepted by
`files.list`/`.get` when you only need names and ids; the canonical scope above
is the safe default.

## First op: list files

```
gum read drive.files.list pageSize=10 fields="files(id,name,mimeType)"
```

Drive's default response includes `kind`, `incompleteSearch`, and every field
on every file, which inflates the wire payload by 5-10x. Always pass `fields=`
to project upstream. See `gum://help/field-masks` for the grammar.

## Common patterns

**Search by name**
```
gum read drive.files.list q="name contains 'budget' and trashed=false" \
   fields="files(id,name,modifiedTime)"
```

**Filter by folder**
```
gum read drive.files.list q="'<folder_id>' in parents and trashed=false" \
   fields="files(id,name,mimeType)"
```

**Fetch a single file's metadata**
```
gum read drive.files.get fileId=<file_id> fields="id,name,mimeType,owners,modifiedTime"
```

**Share a file** (write — adds a permission)
```
gum write drive.permissions.create fileId=<file_id> \
   role=reader type=user emailAddress=you@example.com
```

The default `sendNotificationEmail=true` fires a Drive email. Pass
`sendNotificationEmail=false` to suppress.

## Output shapes you'll see

- `files.list` → `{files: [{id, name, mimeType, ...}], nextPageToken, incompleteSearch}`
- `files.get`  → `{id, name, mimeType, owners, modifiedTime, ...}`
- `permissions.create` → `{id, type, role, emailAddress?}`

`gum search drive` returns every op_id in the indexed Drive surface with
ranking. `gum describe drive.files.list` shows the full ABI.

## Search-query syntax

Drive `q=` is its own mini-language. Useful operators:

| Operator              | Example                                              |
|-----------------------|------------------------------------------------------|
| `name contains '...'` | `name contains 'budget'`                             |
| `mimeType = '...'`    | `mimeType = 'application/vnd.google-apps.folder'`   |
| `'<id>' in parents`   | `'1aBcD...' in parents`                              |
| `trashed = false`     | combine with everything to skip trash                |
| `modifiedTime > ...`  | `modifiedTime > '2026-01-01T00:00:00'`               |

Drive rejects malformed queries with `INVALID_ARGUMENT`. `gum describe
drive.files.list` carries the full operator reference.

## Pagination

`files.list` returns `nextPageToken`. The dispatcher does not auto-paginate.

```
TOK=
while :; do
  PAGE=$(gum read drive.files.list pageSize=100 pageToken=$TOK \
           fields="files(id,name),nextPageToken" format=json)
  echo "$PAGE" | jq '.files[].id'
  TOK=$(echo "$PAGE" | jq -r '.nextPageToken // empty')
  [ -z "$TOK" ] && break
done
```

## Errors

- `AUTH_SCOPE_MISSING` — your stored grant lacks `auth/drive`. Re-run
  `gum auth login --scope https://www.googleapis.com/auth/drive` with the
  broader scope.
- `NOT_FOUND` — file id doesn't exist or the principal can't see it. Drive
  doesn't distinguish, so check the share state.
- `INVALID_ARGUMENT` (`q=`) — Drive rejected the query string. See the
  operator table above.
- `RATE_LIMITED` — Drive returned 429. The dispatcher honours `Retry-After`.

## Related

- `gum://help/auth` — full credential chain
- `gum://help/field-masks` — projection grammar
- `gum://help/toon-format` — output encoding

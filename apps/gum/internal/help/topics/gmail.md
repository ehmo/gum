# Gmail quickstart

Send, list, and manage Gmail messages through `gum`. All Gmail ops resolve to
the typed `gmail/v1` REST SDK; auth flows through the same chain as every
Workspace op (spec §7).

## Auth setup (one-time)

```
printf '%s' '<client_secret>' | gum auth use-oauth-client \
  --client-id <client_id> --secret-stdin
gum auth login --scope https://www.googleapis.com/auth/gmail.readonly
```

Required scopes for the v0.1.0 Gmail roster:

| Op                                  | Scope                                                 |
|-------------------------------------|-------------------------------------------------------|
| `gmail.users.messages.list`/`.get`  | `https://www.googleapis.com/auth/gmail.readonly`      |
| `gmail.users.labels.list`           | `https://www.googleapis.com/auth/gmail.readonly`      |
| `gmail.users.messages.send`         | `https://www.googleapis.com/auth/gmail.send`          |
| `gmail.users.drafts.create`         | `https://www.googleapis.com/auth/gmail.compose`       |
| `gmail.users.messages.trash`        | `https://www.googleapis.com/auth/gmail.modify`        |

The first invocation under a new alias may trigger a live canary if the scope
is `live_canary_required` (spec §7.3). The canary is a single read against
`gmail.users.getProfile` — no user-visible side effects.

## First op: list messages

```
gum read gmail.users.messages.list userId=me maxResults=5
```

Returns a TOON-shaped envelope with the `messages` array; the default field
mask trims to `id,threadId` so the response stays under 1 KB.

Pass `format=json` to disable TOON. Pass `fields=id,threadId,snippet` to widen
the projection on the upstream side (cheaper than letting the default mask drop
fields after the wire roundtrip).

## Common patterns

**Search inbox**
```
gum read gmail.users.messages.list userId=me q="from:noreply newer_than:7d"
```

**Read a message with the full body**
```
gum read gmail.users.messages.get userId=me id=<msg_id> format=full
```

**Send a message** (encoded via the convenience tool `gmail_send`)
```
gum write gmail.users.messages.send userId=me \
   raw=$(echo -e 'To: you@example.com\r\nSubject: hi\r\n\r\nhello' | base64)
```

**Create a draft** (write — no email leaves your account)
```
gum write gmail.users.drafts.create userId=me raw=<base64-rfc822>
```

**Trash a message** (destructive — requires a signed confirmation token)
```
gum destructive gmail.users.messages.trash userId=me id=<msg_id>
# copy confirmation_token from the error envelope, then:
gum destructive gmail.users.messages.trash userId=me id=<msg_id> \
  --confirmed --token <confirmation_token>
```

## Output shapes you'll see

- `messages.list` → `{messages: [{id, threadId}], nextPageToken, resultSizeEstimate}`
- `messages.get` (full) → MIME tree under `payload.parts[]`
- `messages.send` → `{id, threadId, labelIds: ["SENT"]}`
- `drafts.create` → `{id, message: {id, threadId, labelIds: ["DRAFT"]}}`

`gum search gmail` returns every op_id in the indexed Gmail surface with
ranking. `gum describe gmail.users.messages.list` shows the full ABI.

## Field-mask tips

Gmail responses are heavy by default. Use `fields=` to project upstream:

```
gum read gmail.users.messages.list userId=me \
   fields="messages(id,threadId,snippet),nextPageToken"
```

The dispatcher rewrites `fields` into the upstream `?fields=` parameter, so the
wire payload is already trimmed before TOON re-encoding. See
`gum://help/field-masks` for the full grammar.

## Pagination

`messages.list` returns `nextPageToken`. The dispatcher does not auto-paginate.
Loop manually:

```
TOK=
while :; do
  PAGE=$(gum read gmail.users.messages.list userId=me pageToken=$TOK format=json)
  echo "$PAGE" | jq '.messages[].id'
  TOK=$(echo "$PAGE" | jq -r '.nextPageToken // empty')
  [ -z "$TOK" ] && break
done
```

## Errors

- `AUTH_SCOPE_MISSING` — your stored grant lacks `gmail.send` or
  `gmail.modify`. Re-run `gum auth login --scope <scope-url>` with the
  broader scope.
- `RATE_LIMITED` — Gmail returned 429. The dispatcher honours
  `Retry-After`; back off and retry.
- `INVALID_ARGUMENT` (`q=` syntax) — Gmail rejects malformed search queries.
  `gum describe gmail.users.messages.list` has the operator reference.

## Related

- `gum://help/auth` — full credential chain
- `gum://help/field-masks` — projection grammar
- `gum://help/toon-format` — output encoding

---
title: People
description: "People operations in gum's generated catalog."
service_group: "People and education"
---

# People

People has 10 operations in gum's generated catalog. Start with search when you know the task, use describe to inspect request fields and scopes, then dispatch through the command that matches the operation risk class.

| Count | Value |
| --- | --- |
| Family | People and education |
| Operations | 10 |
| Risk classes | 2 destructive, 5 read, 3 write |
| Auth strategies | 10 byo_oauth |

## Start here

```bash
gum search "people"
gum describe people.connections.list
gum read people.connections.list --args '{"fields":"id"}' --output json
```

For write-class operations, gum requires the write command and an explicit write gate:

```bash
gum describe people.contactGroups.create
gum write people.contactGroups.create --allow-write --args '{"fields":"id"}'
```

For destructive operations, run the call once for a confirmation envelope, review the target, then retry with the returned token:

```bash
gum destructive people.contactGroups.delete --args '{"resourceName":"<resourceName>"}'
gum destructive people.contactGroups.delete --args '{"resourceName":"<resourceName>"}' --confirmed --token '<confirmation_token>'
```

## Auth

Auth strategies in this service: 10 byo_oauth. Authenticate the strategy used by the operation you plan to call.

### Bring-your-own OAuth

1. In Google Cloud, enable People API.
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
gum login --service people
```

7. Verify the grant before dispatch:

```bash
gum auth status --scopes contacts,contacts.readonly
gum describe people.connections.list
```

Scopes used by these operations:

- `https://www.googleapis.com/auth/contacts`
- `https://www.googleapis.com/auth/contacts.readonly`

Service setup notes: [People auth guide](../auth-guides/people.md).

## Operations

| Operation | Risk | Auth | Summary |
| --- | --- | --- | --- |
| `people.connections.list` | `read` | `byo_oauth` | List the authenticated user's contacts (requires personFields, e.g. names,emailAddresses,phoneNumbers). |
| `people.contactGroups.create` | `write` | `byo_oauth` | Create a new contact group/label (args.body.contactGroup.name). |
| `people.contactGroups.delete` | `destructive` | `byo_oauth` | Delete a contact group by resourceName. Destructive — requires confirmation per §6.1. |
| `people.contactGroups.get` | `read` | `byo_oauth` | Fetch a contact group by resourceName (contactGroups/NNN). |
| `people.contactGroups.list` | `read` | `byo_oauth` | List the user's contact groups (labels). |
| `people.people.createContact` | `write` | `byo_oauth` | Create a new contact (args.body: names, emailAddresses, phoneNumbers, …). |
| `people.people.deleteContact` | `destructive` | `byo_oauth` | Delete a contact by resourceName. Destructive — requires confirmation per §6.1. |
| `people.people.get` | `read` | `byo_oauth` | Fetch a contact or profile by resourceName (people/cNNN, or people/me). Requires personFields. |
| `people.people.searchContacts` | `read` | `byo_oauth` | Search the user's contacts by query (requires query + readMask). |
| `people.people.updateContact` | `write` | `byo_oauth` | Update a contact by resourceName (requires updatePersonFields; pass the current etag in the body). |

## Next

- Use [API workflows](../api-workflows.md) for search, describe, invoke, and error handling.
- Use [Auth guides](../auth-guides/README.md) for service-specific Google setup.
- Use [Command index](../commands/README.md) for CLI flags and generated help.

---
title: Data Manager
description: "Data Manager operations in gum's generated catalog."
service_group: "Ads and maps"
---

# Data Manager

Data Manager has 2 operations in gum's generated catalog. Start with search when you know the task, use describe to inspect request fields and scopes, then dispatch through the command that matches the operation risk class.

| Count | Value |
| --- | --- |
| Family | Ads and maps |
| Operations | 2 |
| Risk classes | 1 read, 1 write |
| Auth strategies | 2 byo_oauth |

## Start here

```bash
gum search "data manager"
gum describe datamanager.requestStatus.retrieve
gum read datamanager.requestStatus.retrieve --args '{"requestId":"<requestId>"}' --output json
```

For write-class operations, gum requires the write command and an explicit write gate:

```bash
gum describe datamanager.events.ingest
gum write datamanager.events.ingest --allow-write --args '{"body":{"destinations":[{"operatingAccount":{"accountType":"GOOGLE_ADS","accountId":"<customer-id>"},"productDestinationId":"<conversion-action-id>"}],"events":[{"eventTimestamp":"<RFC-3339-time>","eventSource":"WEB","adIdentifiers":{"gclid":"<click-id>"}}],"validateOnly":true}}'
```

## Auth

Auth strategies in this service: 2 byo_oauth. Authenticate the strategy used by the operation you plan to call.

### Bring-your-own OAuth

1. In Google Cloud, enable Data Manager API.
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
gum login --service datamanager
```

7. Verify the grant before dispatch:

```bash
gum auth status --scopes datamanager
gum describe datamanager.requestStatus.retrieve
```

Scopes used by these operations:

- `https://www.googleapis.com/auth/datamanager`

Service setup notes: [Data Manager auth guide](../auth-guides/google-ads.md#data-manager).

## Operations

| Operation | Risk | Auth | Summary |
| --- | --- | --- | --- |
| `datamanager.events.ingest` | `write` | `byo_oauth` | Send conversion events to a Google Ads or Display & Video 360 account (args.body: destinations[] with operatingAccount {accountType:GOOGLE_ADS, accountId} and productDestinationId = the conversion action id, plus events[] with eventTimestamp, eventSource, adIdentifiers {gclid\|gbraid\|wbraid}, conversionValue, currency, and transactionId). At most 2000 events per request. Set validateOnly=true in the body to check a batch without recording it. For live uploads, check the returned requestId with datamanager.requestStatus.retrieve after 30 minutes; processing can take 24 hours. Status lookup is unavailable for validateOnly requests. |
| `datamanager.requestStatus.retrieve` | `read` | `byo_oauth` | Read what Google did with an earlier Data Manager request. Needs the requestId returned by a live ingest call. Wait 30 minutes before checking; processing can take 24 hours. Not available for validateOnly requests. Returns per-destination ingestion status, error info, and warnings. |

## Next

- Use [API workflows](../api-workflows.md) for search, describe, invoke, and error handling.
- Use [Auth guides](../auth-guides/README.md) for service-specific Google setup.
- Use [Command index](../commands/README.md) for CLI flags and generated help.

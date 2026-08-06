---
title: Admin Reports
description: "Admin Reports operations in gum's generated catalog."
service_group: "Workspace administration"
---

# Admin Reports

Admin Reports has 4 operations in gum's generated catalog. Start with search when you know the task, use describe to inspect request fields and scopes, then dispatch through the command that matches the operation risk class.

| Count | Value |
| --- | --- |
| Family | Workspace administration |
| Operations | 4 |
| Risk classes | 4 read |
| Auth strategies | 4 byo_oauth |

## Start here

```bash
gum search "admin reports"
gum describe adminreports.activities.list
gum read adminreports.activities.list --args '{"applicationName":"<applicationName>","userKey":"<userKey>"}' --output json
```

## Auth

Auth strategies in this service: 4 byo_oauth. Authenticate the strategy used by the operation you plan to call.

### Bring-your-own OAuth

1. In Google Cloud, enable Admin Reports API.
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
gum login --service adminreports
```

7. Verify the grant before dispatch:

```bash
gum auth status --scopes admin.reports.audit.readonly,admin.reports.usage.readonly
gum describe adminreports.activities.list
```

Scopes used by these operations:

- `https://www.googleapis.com/auth/admin.reports.audit.readonly`
- `https://www.googleapis.com/auth/admin.reports.usage.readonly`

Service setup notes: [Admin Reports auth guide](../auth-guides/admin-cloud-vault.md).

## Operations

| Operation | Risk | Auth | Summary |
| --- | --- | --- | --- |
| `adminreports.activities.list` | `read` | `byo_oauth` | List audit-log activity events for a user + application (userKey=all or an email; applicationName=login\|admin\|drive\|token\|…). |
| `adminreports.customerUsageReports.get` | `read` | `byo_oauth` | Fetch customer-level usage parameters for a date (YYYY-MM-DD). |
| `adminreports.entityUsageReports.get` | `read` | `byo_oauth` | Fetch usage parameters for an entity (e.g. gplus_communities) on a date. |
| `adminreports.userUsageReport.get` | `read` | `byo_oauth` | Fetch per-user usage parameters for a user (userKey=all or an email) on a date. |

## Next

- Use [API workflows](../api-workflows.md) for search, describe, invoke, and error handling.
- Use [Auth guides](../auth-guides/README.md) for service-specific Google setup.
- Use [Command index](../commands/README.md) for CLI flags and generated help.

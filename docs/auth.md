---
title: Auth
description: "How gum resolves Google OAuth, API keys, service accounts, ADC, Google Ads credentials, and plugin-owned credentials."
---

# Auth

`gum` calls Google APIs with credentials from the selected local profile. It
does not ship a hidden Google account, a shared OAuth client, or a background
cloud identity.

## Pick the auth path

Use `gum describe <op_id>` before setup. The operation record shows the auth
strategy, scopes, risk class, and required arguments.

```bash
gum describe gmail.users.messages.list
gum auth setup gmail.users.messages.list
```

| Catalog auth strategy | Use it for | Setup command |
| --- | --- | --- |
| `byo_oauth` | Gmail, Calendar, Drive, Docs, Sheets, Slides, Tasks, YouTube Data API, People, Photos, Chat, Classroom, Meet, Apps Script, Admin, Vault, Search Console, Google Ads | `gum auth use-oauth-client`, then `gum login` |
| `api_key` | Maps, Places, Routes, Custom Search | `gum auth use-api-key --stdin` |
| `service_account` | Operations or plugins that explicitly accept a Google service account key | `gum auth use-service-account <key.json>` |
| `adc` | Hosts already running under Google Application Default Credentials | Configure ADC on the host |
| `plugin_managed` | Plugin-backed operations whose upstream auth is owned by the plugin | Follow the plugin's setup |
| `none` | Local or metadata operations that do not call an external API | No credential setup |

## Before any auth setup

1. Choose the profile that will own the credential.

```bash
gum --profile work auth status
```

2. Identify the operation you plan to call.

```bash
gum search "search console sites"
gum describe searchconsole.sites.list
```

3. Read the service page for product prerequisites. Google project setup still
matters: the API must be enabled, the OAuth consent screen or API key must
permit the API, and the signed-in account must have access to the target data.

## Bring-your-own OAuth

Use this for most Google Workspace and Google product APIs.

1. In Google Cloud Console, create or select the project that will own the OAuth
   client.
2. Enable each API you plan to call. For example, Search Console operations need
   the Search Console API; YouTube operations need the YouTube Data API v3.
3. Configure the OAuth consent screen. If the app is in testing mode, add the
   Google account you will sign in with as a test user.
4. Add the scopes reported by `gum describe` or by the service page.
5. Create credentials: `OAuth client ID`, application type `Desktop app`.
6. Store the OAuth client in gum.

```bash
printf '%s' "$GOOGLE_OAUTH_CLIENT_SECRET" \
  | gum auth use-oauth-client --client-id "$GOOGLE_OAUTH_CLIENT_ID" --secret-stdin
```

7. Authorize the service or exact scopes. Service names match the catalog names
   used in `docs/services`.

```bash
gum login --service gmail,calendar
gum login --service searchconsole
gum login --scope https://www.googleapis.com/auth/webmasters.readonly
```

8. Verify the grant before dispatch.

```bash
gum auth status --scopes webmasters.readonly
gum read searchconsole.sites.list --output json
```

OAuth is successful when `gum auth status` can resolve a token for the requested
scope and the first read call reaches Google. If Google returns
`accessNotConfigured`, the API is disabled in the project that owns the OAuth
client. If gum returns `SCOPE_MISSING`, run `gum login` again with the missing
scope or service.

## API key

Use this for the v1 catalog's Maps, Places, Routes, and Custom Search
operations.

1. Enable the required API in Google Cloud.
2. Create an API key.
3. Restrict the key to the API and to the host, referrer, or environment that
   should use it.
4. Store the key in gum.

```bash
printf '%s' "$GOOGLE_API_KEY" | gum auth use-api-key --stdin
```

5. Verify with a read operation.

```bash
gum describe maps.timezone.get
gum read maps.timezone.get --args '{"location":"37.7749,-122.4194","timestamp":0}' --output json
```

API-key auth is successful when gum can read the stored key and Google accepts
the key for the enabled API. `PERMISSION_DENIED`, `REQUEST_DENIED`, or quota
errors usually mean the key is restricted incorrectly, the API is disabled, or
the Cloud project is not allowed to use that API.

## Service account

Use this only for operations or plugins whose auth strategy requires a service
account key. A service account is not a substitute for user OAuth on APIs that
require a user grant.

1. Create or select a Google Cloud service account.
2. Grant only the roles required for the target API.
3. If the API requires Workspace domain-wide delegation, enable delegation and
   authorize the client ID in the Workspace Admin console.
4. Download the JSON key to the host running gum.
5. Store the key in the selected profile.

```bash
gum auth use-service-account ./service-account.json
```

6. Verify the operation that requires the service account.

```bash
gum auth setup <op_id>
gum describe <op_id>
```

Service-account auth is successful when `gum auth setup <op_id>` no longer
reports a missing key and the target API accepts the service account identity.
Workspace APIs may still reject the call if delegation, scopes, or admin roles
are incomplete.

## ADC

Use Application Default Credentials when gum runs inside an environment that
already provides Google credentials, such as a configured developer workstation,
Cloud Shell, a VM, or a workload identity setup.

1. Configure ADC on the host. For local development this is usually done outside
   gum.
2. Confirm the host can expose ADC to the process running gum.
3. Probe the scope you need.

```bash
gum auth probe --strategy adc --scopes webmasters.readonly
```

4. Run the target operation.

```bash
gum read searchconsole.sites.list --output json
```

ADC auth is successful when `gum auth probe --strategy adc` returns non-secret
token metadata for the requested scope. If the probe fails, fix ADC on the host;
storing an OAuth client in gum does not repair ADC.

## Google Ads

Google Ads uses OAuth plus a developer token.

1. Enable the Google Ads API in Google Cloud.
2. Create and store a Desktop OAuth client as described in the OAuth section.
3. Store the developer token.

```bash
printf '%s' "$GOOGLE_ADS_DEVELOPER_TOKEN" | gum auth use-ads-developer-token --stdin
```

4. Authorize the Ads scope.

```bash
gum login --service googleads
```

5. Verify both the OAuth grant and the Ads operation shape.

```bash
gum auth status --scopes adwords
gum describe googleads.keywordPlanIdeas.generateKeywordHistoricalMetrics
```

Google Ads auth is successful only when OAuth succeeds, the developer token is
stored, and the signed-in account can access the customer ID used in the call.
Google can still reject requests when the token is pending, the customer ID is
wrong, or the account lacks access.

## Plugin-managed auth

Plugin-managed operations do not use gum's Google OAuth client, API key, ADC, or
service-account store unless the plugin explicitly documents that dependency.

1. Inspect the operation.

```bash
gum describe youtube.transcripts.get
```

2. Check the plugin inventory.

```bash
gum plugin list
gum plugin info <plugin-name>
```

3. Complete the plugin's own credential or prerequisite setup.
4. Run the read call.

```bash
gum read youtube.transcripts.get --args '{"video_id":"<video-id>"}' --output json
```

Plugin-managed auth is successful when the plugin reports ready state and the
operation returns data from its upstream source.

## Troubleshooting

- `AUTH_REQUIRED`: gum could not resolve a credential for the operation.
- `SCOPE_MISSING`: the credential exists but lacks at least one required OAuth
  scope.
- `accessNotConfigured`: Google says the API is disabled in the Cloud project
  that owns the credential.
- `PERMISSION_DENIED`: the credential exists, but Google rejected the account,
  API key, Workspace role, customer ID, property, or resource permissions.
- Workspace admin APIs require a managed Workspace domain and suitable admin
  privileges. Consumer accounts cannot satisfy Admin SDK or Vault admin flows.

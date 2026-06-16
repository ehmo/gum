# Authentication and credential setup

GUM resolves credentials at dispatch time from the resolved catalog variant's
`auth_strategy`. The public v1 auth paths are:

1. **`byo_oauth`** — an operator-supplied Google Desktop OAuth client. The
   OAuth client and refresh token live in the OS keychain under the active
   profile.
2. **`adc`** — Google Application Default Credentials. Honours
   `GOOGLE_APPLICATION_CREDENTIALS`, then the user's gcloud well-known
   location, then metadata credentials on Google Cloud.
3. **`api_key`** — API key from the OS keychain or `GUM_API_KEY`.
4. **`service_account_key`** — downloaded service-account JSON path from
   `GUM_SERVICE_ACCOUNT_KEY`.
5. **`gum_oauth`** — reserved internal strategy. It is not the public v1 login
   path.

## Where credentials live

| Source                | Storage                                                   |
|-----------------------|-----------------------------------------------------------|
| `byo_oauth`           | OS keychain entry created by `gum auth use-oauth-client`. |
| `gum_oauth`           | Reserved; not configured through public v1 login.         |
| `api_key`             | OS keychain or `GUM_API_KEY`.                             |
| `service_account_key` | `GUM_SERVICE_ACCOUNT_KEY` path.                           |
| `adc`                 | Whatever the standard ADC library resolves.               |

GUM never prints client secrets, refresh tokens, API keys, or access tokens in
status output.

## Scope discipline

Every OAuth grant is checked against the resolved variant's required scopes. If
a grant is too narrow, re-run `gum auth login --scope <scope-url>` with the
broader scope set; never patch the catalog.

## Quick commands

- `gum auth status` — show resolved provider and scope coverage.
- `gum auth use-oauth-client --client-id <id> --secret-stdin` — store your
  Google Desktop OAuth client in the OS keychain.
- `gum auth login --scope <scope-url>` — run the loopback + PKCE browser flow
  and store a refresh token for the requested scopes.
- `gum auth probe --scopes <scope>` — acquire a token and print non-secret
  metadata before retrying an operation.

## Errors

- `BYO_OAUTH_CLIENT_NOT_CONFIGURED` — no usable OAuth client is available for
  the requested scopes. Run `gum auth use-oauth-client`.
- `AUTH_SCOPE_MISSING` — the stored grant's scopes do not cover the op. Re-run
  `gum auth login --scope <scope-url>` with the broader scope.
- `AUTH_REFRESH_FAILED` / `GUM_OAUTH_TOKEN_EXCHANGE_FAILED` — the token
  endpoint rejected refresh. Usually the refresh token was revoked or the
  OAuth client is missing its secret.

See `gum://help/profiles` for how runtime profiles scope keychain entries.

## Google Cloud Console setup (byo_oauth)

`byo_oauth` uses an OAuth 2.0 Desktop client that you create in the Google
Cloud Console. The same setup applies to catalog variants that declare
`auth_strategy = byo_oauth`.

1. **Pick or create a project.** Open
   <https://console.cloud.google.com/projectcreate>, name it, and select the
   right billing account. The project ID may appear as `quota_project_id` in
   `gum auth status`.

2. **Enable APIs.** Visit
   <https://console.cloud.google.com/apis/library> and enable each Google API
   used by the ops you plan to call. Common examples are Gmail API, Google
   Calendar API, Google Drive API, Sheets API, and Search Console API.

3. **Configure the OAuth consent screen.** Go to
   <https://console.cloud.google.com/apis/credentials/consent>. Choose
   **External** unless your Workspace admin has set up an Internal app. Add the
   scopes you need; the scope strings come from the catalog. While the app is
   in Testing, add each operator as a test user.

4. **Create OAuth credentials.** Open
   <https://console.cloud.google.com/apis/credentials> -> **Create credentials**
   -> **OAuth client ID** -> **Desktop app**. Download the JSON. It contains
   `client_id` and usually `client_secret`.

5. **Register the client with GUM.** Run:

   ```
   printf '%s' '<client_secret>' | gum auth use-oauth-client \
     --client-id <client_id> --secret-stdin
   ```

   Use `--secret-file <path>` instead if you downloaded a client JSON and want
   to extract the secret outside the shell. The secret is stored in the OS
   keychain and is not written to profile config.

6. **Authorize scopes.** Run:

   ```
   gum auth login --scope https://www.googleapis.com/auth/gmail.readonly
   ```

   Repeat `--scope`, or comma-separate values, when an operation needs multiple
   scopes. Headless environments can use `--no-browser` and open the printed URL
   manually.

After the grant, `gum auth status` and `gum auth probe --scopes <scope>` should
show non-secret token metadata. If dispatch returns `AUTH_SCOPE_MISSING`,
authorize the broader scope set with `gum auth login --scope ...`.

See `gum://help/plugins` for the parallel walkthrough used by
`plugin_managed` auth (`gum plugin setup <name>`).

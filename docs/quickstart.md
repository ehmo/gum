---
title: Quickstart
description: "Register a Google OAuth client, authorize gum, inspect the catalog, and make the first read call."
---

# Quickstart

This path uses your own Google Cloud project and a Desktop OAuth client. That
keeps the consent screen, enabled APIs, quotas, verification state, and refresh
tokens under your control.

## 1. Create the Google client

In Google Cloud Console:

1. Create or select a project.
2. Configure the OAuth consent screen.
3. Create an OAuth client ID with application type `Desktop app`.
4. Enable the APIs you plan to call.

For service-specific setup notes, use the [auth guides](auth-guides/).

## 2. Store the client in gum

```bash
printf '%s' "$GOOGLE_OAUTH_CLIENT_SECRET" \
  | gum auth use-oauth-client --client-id "$GOOGLE_OAUTH_CLIENT_ID" --secret-stdin
```

Passing the secret through stdin keeps it out of shell history.

## 3. Authorize a small scope set

```bash
gum login --service gmail,calendar
gum auth status
gum doctor
```

Use `--no-browser` on SSH hosts or dev containers.

## 4. Inspect the catalog

```bash
gum search "gmail messages"
gum describe gmail.users.messages.list
gum schema --json > gum-command-schema.json
```

`search` finds operations by text. `describe` prints the selected catalog entry,
including variants and example arguments. `schema --json` prints the CLI command
tree used by the generated docs.

## 5. Make a read call

```bash
gum read gmail.users.messages.list \
  --args '{"userId":"me","maxResults":5}' \
  --output json
```

If Google returns `accessNotConfigured`, enable that API in the Cloud project
that owns the OAuth client, wait for propagation, and retry. If gum returns
`SCOPE_MISSING`, rerun `gum login` with the service or exact scope the error
reports.

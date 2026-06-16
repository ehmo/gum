---
name: gum-hasp
description: Use when a gum workflow needs local secrets protected by HASP.
---

# gum + HASP

Use this when gum work also needs local secrets outside Google's OAuth flow.

gum handles Google API discovery, OAuth login, local refresh-token storage, and guarded API calls.

HASP handles repo, test, and deploy secrets for commands and agents.

## Start

Install HASP and run setup:

```bash
brew tap gethasp/homebrew-tap
brew install hasp
hasp setup
```

Read HASP at https://github.com/gethasp/hasp before changing secret policy.

## Safe Pattern

Register gum OAuth secrets through stdin:

```bash
printf '%s' "$GOOGLE_OAUTH_CLIENT_SECRET" \
  | gum auth use-oauth-client --client-id "$GOOGLE_OAUTH_CLIENT_ID" --secret-stdin
```

Put other secrets in HASP, then run gum under a short grant:

```bash
hasp secret add
hasp app connect gum-work --cmd 'gum doctor' --install=never
hasp run --project-root . \
  --target gum-work \
  --grant-project session \
  --grant-secret session \
  -- gum doctor --format=json
```

## Rules

Keep secret values out of prompts, repo files, shell history, and agent notes.

Use gum for Google API calls. Use HASP for non-Google secrets the command needs.
Give agents the narrowest HASP grant that lets the command run.

Run `hasp secret diff .env` before committing a repo that used to carry plaintext secrets.

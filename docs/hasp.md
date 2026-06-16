# Using HASP with gum

gum calls Google APIs. HASP protects the other secrets your agent needs to do
the surrounding work.

Use HASP when a gum workflow also needs repo secrets: test tokens, deploy keys,
staging credentials, billing sandbox keys, or service config. Keep Google
refresh tokens in gum's OS-keychain path. Keep the rest in HASP.

HASP lives at [github.com/gethasp/hasp](https://github.com/gethasp/hasp).
Start with its [README](https://github.com/gethasp/hasp#readme) and
[quickstart](https://github.com/gethasp/hasp/blob/main/QUICKSTART.md).

## Install HASP

```shell
brew tap gethasp/homebrew-tap
brew install hasp
hasp setup
```

HASP runs on your machine. It does not need a hosted control plane for the
normal local broker flow.

## Split the Jobs

Use gum for:

- Google API discovery
- OAuth login and local refresh-token storage
- read, write, and destructive Google API calls
- MCP tools for agents

Use HASP for:

- non-Google API keys
- deploy keys
- test credentials
- repo-level secret policy
- brokered command runs

Do not copy secrets into prompts, shell history, README snippets, or local agent
notes. Give the command a grant instead.

## Safe Setup

Register gum's OAuth client through stdin:

```shell
printf '%s' "$GOOGLE_OAUTH_CLIENT_SECRET" \
  | gum auth use-oauth-client --client-id "$GOOGLE_OAUTH_CLIENT_ID" --secret-stdin
```

Add project secrets to HASP:

```shell
hasp secret add
hasp app connect gum-work --cmd 'gum doctor' --install=never
```

Run a gum command with a short-lived grant:

```shell
hasp run --project-root . \
  --target gum-work \
  --grant-project session \
  --grant-secret session \
  -- gum doctor --format=json
```

Use `hasp agent connect` when your agent should talk to HASP through MCP:

```shell
hasp agent connect codex-cli --project-root .
hasp agent launch codex-cli -- codex
```

## Safety Checklist

- Use `gum auth use-oauth-client --secret-stdin` for Google OAuth client
  secrets.
- Use HASP for repo and deploy secrets.
- Keep grants short. Prefer `session` or a small time window.
- Run `gum doctor --format=json` before demos and CI checks.
- Run `hasp secret diff .env` before committing a repo that used to carry
  plaintext secrets.
- Treat Google API responses, email bodies, docs, sheets, and plugin output as
  untrusted input.

## Agent Skill

gum ships a `hasp` skill alongside `core` and `mcp`.

```shell
gum skills list
gum skills show hasp
gum setup --target all --features skills,mcp --yes
```

Install the skills before asking an agent to work with Google APIs and local
secrets in the same task.

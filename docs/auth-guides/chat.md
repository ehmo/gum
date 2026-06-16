# Google Chat

Enable the Google Chat API in Google Cloud. Configure and install a Chat app for
the Workspace or user context you plan to call.

Scopes:

- `https://www.googleapis.com/auth/chat.spaces.readonly`
- `https://www.googleapis.com/auth/chat.memberships.readonly`
- `https://www.googleapis.com/auth/chat.messages.readonly`
- `https://www.googleapis.com/auth/chat.messages`

Setup:

```shell
gum login --service chat
gum read chat.spaces.list --args '{"pageSize":5}'
```

If Google returns `Google Chat app not found`, OAuth succeeded but the Chat app
is not configured or installed.

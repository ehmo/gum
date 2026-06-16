# Gmail

Enable the Gmail API in Google Cloud. Add the scopes you need to the OAuth
consent screen.

Common scopes:

- `https://www.googleapis.com/auth/gmail.readonly`
- `https://www.googleapis.com/auth/gmail.modify`
- `https://www.googleapis.com/auth/gmail.send`
- `https://mail.google.com/`

Setup:

```shell
gum login --service gmail
gum read gmail.users.messages.list --args '{"userId":"me","maxResults":5}'
```

Use write scopes only when you need label, draft, send, or message mutation
operations. If Google rejects consent, remove unused Gmail scopes from the
login request and authorize only the endpoint you need.

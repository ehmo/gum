# People and Contacts

Enable the People API in Google Cloud. Add Contacts scopes to the OAuth consent
screen.

Scopes:

- `https://www.googleapis.com/auth/contacts.readonly`
- `https://www.googleapis.com/auth/contacts`

Setup:

```shell
gum login --service people
gum read people.people.connections.list --args '{"resourceName":"people/me","personFields":"names,emailAddresses","pageSize":5}'
```

Some `people/me` requests can also require the basic profile scope in Google
account contexts. If Google returns `PERMISSION_DENIED`, inspect the endpoint
with `gum describe` and authorize the exact missing scope from the error.

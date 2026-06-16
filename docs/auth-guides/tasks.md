# Tasks

Enable the Google Tasks API in Google Cloud. Add Tasks scopes to the OAuth
consent screen.

Scopes:

- `https://www.googleapis.com/auth/tasks.readonly`
- `https://www.googleapis.com/auth/tasks`

Setup:

```shell
gum login --service tasks
gum read tasks.tasklists.list --args '{"maxResults":5}'
```

Task and task-list writes use the full `tasks` scope.

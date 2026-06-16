# Drive

Enable the Google Drive API in Google Cloud. Add Drive scopes to the OAuth
consent screen.

Common scopes:

- `https://www.googleapis.com/auth/drive.readonly`
- `https://www.googleapis.com/auth/drive`

Setup:

```shell
gum login --service drive
gum read drive.files.list --args '{"pageSize":5,"fields":"files(id,name,mimeType)"}'
```

Drive write operations can move, copy, update, or delete user content. Run
`gum describe <op_id>` before writes and check the risk class.

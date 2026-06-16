# Admin, Cloud Identity, Groups Settings, and Vault

Enable the Admin SDK, Admin Reports API, Cloud Identity API, Groups Settings
API, and Vault API only when your Workspace tenant needs them. The signed-in
account must have the required Workspace admin privileges.

Services and scopes:

- Admin Directory: `admin.directory.user.readonly`, `admin.directory.user`, `admin.directory.group.readonly`, `admin.directory.group`, `admin.directory.group.member.readonly`, `admin.directory.group.member`
- Admin Reports: `admin.reports.audit.readonly`, `admin.reports.usage.readonly`
- Cloud Identity: `cloud-identity.groups.readonly`
- Groups Settings: `apps.groups.settings`
- Vault: `ediscovery.readonly`, `ediscovery`

Setup:

```shell
gum login --service admin,adminreports,cloudidentity,groupssettings,vault
gum read admin.users.list --args '{"customer":"my_customer","maxResults":5}'
```

Admin and Vault writes can affect tenant-wide state. Use the narrowest scope
and inspect `gum describe <op_id>` before calling write operations.

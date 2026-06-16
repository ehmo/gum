# Security

Report suspected vulnerabilities by private email or a private GitHub advisory.
Do not open a public issue for a security report.

gum runs with the permissions of the local user. It can read local credential
stores, call Google APIs, launch plugin subprocesses, and serve MCP tools over
stdio to an agent client. Treat the CLI and MCP server as privileged local
software.

Default posture:

- Public v1 builds use credentials registered by the operator: a Google Desktop
  OAuth client, API key, service account, or ADC credential.
- Risky operations go through the shared dispatch policy gate.
- Write and destructive calls require explicit operator flags and confirmation
  where the catalog marks the operation as high risk.
- MCP uses stdio only. gum does not open a network listener for MCP.
- Plugin subprocesses are user-level code. macOS and Linux confinement is
  enforced where the platform backend is available; unsupported platforms fail
  closed for plugin spawn.
- Secrets are never accepted as normal operation arguments. Store OAuth clients,
  API keys, service-account keys, and Google Ads developer tokens through the
  `gum auth` commands.

Useful local checks:

```shell
cd apps/gum
go test -race -count=1 ./...
go vet ./...
go run golang.org/x/vuln/cmd/govulncheck@v1.3.0 ./...
goreleaser check
```

---
title: Security
description: "Security posture and reporting guidance for gum."
---

# Security

Report suspected vulnerabilities by private email or a private GitHub advisory.
Do not open a public issue for a security report.

gum is privileged local software. It runs with the permissions of the local
user, reads configured credential stores, calls Google APIs, starts plugin
subprocesses, and serves MCP tools over stdio to an agent client.

## Default posture

- Public builds use operator-registered credentials: BYO Google Desktop OAuth,
  API key, service-account key, or ADC.
- Write and destructive operations go through explicit command gates.
- MCP uses stdio. gum does not open an MCP network listener.
- Plugin subprocesses are user-level code. macOS and Linux confinement is
  enforced where the platform backend is available; unsupported platforms fail
  closed for plugin spawn.
- Secrets are configured through `gum auth` commands, not normal operation
  arguments.

## Local checks

```bash
cd apps/gum
go test -race -count=1 ./...
go vet ./...
go run golang.org/x/vuln/cmd/govulncheck@v1.3.0 ./...
goreleaser check
```

The repository security policy remains the source of truth:
[`SECURITY.md`](https://github.com/ehmo/gum/blob/main/SECURITY.md).

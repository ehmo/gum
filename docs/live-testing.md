---
title: Live testing
description: "Run gum checks that use real Google credentials or live plugin subprocesses."
---

# Live testing

The default gum test path stays offline. Use live checks only with a dedicated
Google account, a test Cloud project, enabled APIs, and quota you are prepared
to spend.

## Offline checks first

Run these before any live check:

```bash
cd apps/gum
go test ./...
gum gain --fixture-replay --format toon
gum gain --fixture-replay --format json
```

The fixture replay is deterministic and local. It is the right way to compare
output shaping and token-savings behavior across builds.

## Live dispatch tests

The `live` build tag keeps live dispatch tests out of the default suite. These
tests use ambient Google credentials from the test host.

```bash
cd apps/gum
GUM_LIVE_TEST_ACCOUNT=user@example.com go test -tags=live ./internal/dispatch/
```

The read tests call Gmail and Calendar list operations. The write test sends a
Gmail message from the live account to itself. Use a test account, then remove
the message after the run if your policy requires it.

## Flights plugin smoke test

The Flights live test skips itself unless the `fli` subprocess is available on
`PATH` or `GUM_FLIGHTS_MCP_BIN` points at it.

```bash
cd apps/gum
GUM_LIVE_FLIGHTS=1 GUM_FLIGHTS_MCP_BIN=/path/to/fli go test ./cmd/gum -run TestFlights
```

This test exercises the plugin bridge. The offline release gate does not depend
on it.

## Plugin canaries

For installed plugins, use the canary command to verify that gum can start the
subprocess under the active profile:

```bash
gum canary --plugin <name> --live
```

A failed canary prints a JSON envelope and exits non-zero. A credentialed plugin
installed without required credentials stays in setup state until you configure
those credentials and a later canary succeeds.

## Operating rules

- Use a test Google account and a test Cloud project.
- Enable only the APIs required for the check.
- Keep live checks out of normal CI unless that CI job owns the account and
  cleanup policy.
- Treat upstream API failures as setup evidence first: disabled APIs, missing
  scopes, account policy, quota, and service availability are common causes.

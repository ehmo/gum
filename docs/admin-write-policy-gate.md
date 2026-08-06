# Admin SDK Write Policy Gate

GUM currently includes selected Admin Directory writes for depth coverage, but
high-blast-radius Admin writes must remain excluded until a narrower gate exists.
This design defines the gate required before any additional Admin write surface
is promoted.

## Scope

Allowed to consider under this gate:

- group create/update/delete in a disposable test domain;
- group-member insert/delete where both group and member are fixture-owned;
- user create/update/delete only for fixture-owned suspended test users.

Excluded until a later policy:

- domain-wide settings, security, billing, mobile/device, Chrome, Vault, and
  investigation writes;
- role, privilege, super-admin, delegation, recovery, password reset, 2SV, and
  account-suspension changes for non-fixture users;
- bulk operations, transfer operations, and any operation that can affect data
  retention, legal holds, mail routing, or organization-wide access.

## Risk Classes

Admin writes use the normal catalog `risk_class` values plus an Admin-specific
`admin_policy.blast_radius` classification in generator metadata:

| Class | Meaning | Catalog eligibility |
| --- | --- | --- |
| `admin_fixture_write` | Mutates only a fixture-owned user, group, or member created for tests. | Eligible after this gate and destructive confirmation. |
| `admin_reversible_write` | Mutates a real directory object but has a bounded, audited rollback path. | Not eligible for v0.1 broad preview. |
| `admin_high_blast_write` | Can affect account access, privileges, retention, security posture, billing, domains, or many users. | Excluded. |

The generator and catalog validator fail if an Admin write lacks this
classification. Unknown Admin writes default to `admin_high_blast_write` and are
excluded from the embedded catalog.

## Promotion Requirements

An Admin write can be enabled only when all of these are true:

- the operation is classified as `admin_fixture_write`;
- the request schema marks identity and body fields explicitly, with no unknown
  body passthrough;
- fixture ownership can be proven from arguments before dispatch;
- `--risk=destructive` is required for destructive operations and write methods
  cannot pass through the read-risk gate;
- the caller has supplied the existing destructive confirmation token when
  required by dispatch;
- audit logging records op id, resource key, fixture marker, profile, risk
  class, and upstream request id when available;
- rollback or cleanup command is documented for the fixture.

## Fixture Strategy

Use a dedicated Workspace test domain or sub-organization. Fixture resources must
carry a deterministic marker such as `gum-fixture-<run-id>` in group email,
display name, external ID, or another field available before dispatch.

Minimum fixture set:

- one disposable group for group update/delete;
- one disposable user for user update/delete, created suspended and outside any
  privileged org unit;
- one disposable member relationship between those resources.

The test runner must create or verify the fixture before write tests and clean
it up after the run. Cleanup failure is a test failure and must leave enough
metadata for manual cleanup.

## Required Tests

Before enabling another Admin write, add tests that prove:

- unknown Admin writes are excluded or fail generation;
- every emitted Admin write has a blast-radius classification;
- high-blast-radius operations are absent from `catalog.json`;
- read risk cannot execute Admin write/destructive variants;
- destructive Admin operations require the confirmation path;
- fixture ownership checks reject non-fixture users, groups, and members;
- request fields cover all required path/query/body fields for the operation.

This is intentionally stricter than non-Admin write coverage. Admin APIs can
change tenant-wide state, so the default must be exclusion until the operation is
small, fixture-bound, and testable.

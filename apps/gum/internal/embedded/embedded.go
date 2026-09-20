// Package embedded exposes runtime registries and the build-time catalog as
// byte slices that are baked into the binary at compile time.
//
// Tests that need to mutate these contents should use a local copy; the
// returned slices are shared across the process.
package embedded

import (
	"embed"
	_ "embed"
)

//go:embed data/tier-a-roster.v1.json
var TierARosterJSON []byte

// SchemaFS is the first-party JSON Schema 2020-12 store served by
// gum://schema/{ref}. The build-time generator that populates the directory
// is deferred to v0.2.0 (see bd show gum-zev5); until then only the
// `test-fixture.v1.json` placeholder ships so the embed wiring compiles and
// the schema-resource happy path can be exercised in tests. Files prefixed
// with `_` (READMEs, design notes) are excluded by go:embed.
//
//go:embed schemas
var SchemaFS embed.FS

//go:embed data/auth-managed-scopes.v1.json
var AuthManagedScopesJSON []byte

// GumOAuthClientID and GumOAuthClientSecret are vestigial in v1. gum owns no
// OAuth client: no production code reads either variable, and a byo_oauth
// variant with no operator-registered client returns
// BYO_OAUTH_CLIENT_NOT_CONFIGURED pointing at `gum auth use-oauth-client`.
//
// Both stay empty in every build. Release builds MUST NOT inject them through
// ldflags, CI environment, HASP targets, or build scripts, and the names
// GUM_OAUTH_CLIENT_ID and GUM_OAUTH_CLIENT_SECRET must not appear in any build
// surface. They are declared only so the regression tests can set them and
// prove that an injected managed client changes nothing: see
// auth.TestResolveAuthIgnoresInjectedManagedClient.
var (
	GumOAuthClientID     = ""
	GumOAuthClientSecret = ""
)

//go:embed data/auth-managed-scopes.v1.schema.json
var AuthManagedScopesSchemaJSON []byte

//go:embed data/help-topics.v1.json
var HelpTopicsJSON []byte

//go:embed data/expression-profile-dsl.json
var ExpressionProfileDSLJSON []byte

// CatalogJSON is the build-time generated Google capability catalog.
// When the binary is built without a generated catalog, this slice is empty
// (the file `catalog.json` does not exist in the embedded/ tree and the build
// tag `embed_catalog` is off). Runtime code MUST treat an empty value as
// "no catalog available" rather than fail.
var CatalogJSON = catalogJSON

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

// GumOAuthClientID is the public client_id of gum's built-in managed Desktop
// OAuth client (the "gum-oauth" Google Cloud project). It is non-confidential
// by Google's Installed-App classification, but it is NOT committed: the managed
// client is rotated out-of-band, so a hard-coded id goes stale the moment the
// client is rebuilt. Like the secret, it is delivered from the HASP vault
// (GUM_OAUTH_CLIENT_ID) and injected at build/release time via the linker, e.g.
//
//	-ldflags "-X github.com/ehmo/gum/internal/embedded.GumOAuthClientID=$GUM_OAUTH_CLIENT_ID"
//
// When empty (plain dev builds, no HASP) the built-in managed client is treated
// as unavailable and callers fall back to the registered-client (BYO) path.
var GumOAuthClientID = ""

// GumOAuthClientSecret is the managed Desktop client's secret. Google requires
// it at the token-exchange step even for PKCE Installed-App clients, but it is
// NEVER committed: it stays empty in source (and therefore in dev builds) and
// is injected only at build/release time via the linker, e.g.
//
//	-ldflags "-X github.com/ehmo/gum/internal/embedded.GumOAuthClientSecret=$GUM_OAUTH_CLIENT_SECRET"
//
// from the HASP vault item GUM_OAUTH_CLIENT_SECRET. When empty (dev builds)
// the built-in managed client is treated as unavailable and callers fall back
// to the registered-client path.
var GumOAuthClientSecret = ""

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

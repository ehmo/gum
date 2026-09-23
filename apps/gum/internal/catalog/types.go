// Package catalog holds the build-time Google capability catalog ABI (spec.md §5.3, §14).
//
// Types only in this package; generation logic lives in cmd/gen-catalog.
package catalog

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Sentinel errors for typed matching in tests.
var (
	ErrMissingRequiredField            = errors.New("catalog: missing required field")
	ErrUnknownRiskClass                = errors.New("catalog: unknown risk_class")
	ErrUnknownAuthStrategy             = errors.New("catalog: unknown auth_strategy")
	ErrUnknownAuthComponent            = errors.New("catalog: AUTH_COMPONENT_UNKNOWN")
	ErrUnknownBackendKind              = errors.New("catalog: UNKNOWN_BACKEND_KIND")
	ErrUnknownInterfaceKind            = errors.New("catalog: UNKNOWN_INTERFACE_KIND")
	ErrUnknownAdminBlastRadius         = errors.New("catalog: unknown admin blast_radius")
	ErrMissingAdminPolicy              = errors.New("catalog: missing admin_policy")
	ErrAdminFixtureOwnership           = errors.New("catalog: admin fixture ownership violation")
	ErrUnknownStability                = errors.New("catalog: unknown stability")
	ErrDanglingDefaultVariantID        = errors.New("catalog: default_variant_id references a variant_id not in variants[]")
	ErrDuplicateOpID                   = errors.New("catalog: duplicate op_id")
	ErrDuplicateVariantID              = errors.New("catalog: duplicate variant_id within an op")
	ErrUnsupportedBindingSchemaVersion = errors.New("catalog: BINDING_SCHEMA_UNSUPPORTED")
	ErrUnsupportedCatalogSchemaVersion = errors.New("catalog: CATALOG_SCHEMA_UNSUPPORTED")
	ErrServiceRootTemplateDeferred     = errors.New("catalog: SERVICE_ROOT_TEMPLATE_DEFERRED")
	ErrNoDefaultDeclared               = errors.New("catalog: request field declares no default")

	// The three GRPC_ROUTING_HEADER_* codes are the spec §7 registry names for
	// the catalog-abi.md `routing_headers` invariant. Rule 1's alphabet has no
	// registry code of its own, so its sentinel carries a plain message.
	ErrGRPCRoutingHeaderInvalid     = errors.New("catalog: routing_headers entry violates the closed alphabet")
	ErrGRPCRoutingHeaderNotFound    = errors.New("catalog: GRPC_ROUTING_HEADER_NOT_FOUND")
	ErrGRPCRoutingHeaderDuplicate   = errors.New("catalog: GRPC_ROUTING_HEADER_DUPLICATE")
	ErrGRPCRoutingHeaderNotRequired = errors.New("catalog: GRPC_ROUTING_HEADER_NOT_REQUIRED")

	ErrUnknownExecutionSupport           = errors.New("catalog: unknown execution_support")
	ErrMissingUnsupportedCapabilities    = errors.New("catalog: execution_support requires unsupported_capabilities")
	ErrUnexpectedUnsupportedCapabilities = errors.New("catalog: execution_support \"full\" forbids unsupported_capabilities")
	ErrUndeclaredUnsupportedCapability   = errors.New("catalog: unsupported_capabilities atom is not declared in capabilities")
	ErrPartialWithNoExecutableCapability = errors.New("catalog: execution_support \"partial\" blocks every declared capability")

	// ErrUnknownCapability is the spec §913 build-time and load-time code
	// UNKNOWN_CAPABILITY. The capabilities enum is closed, so an atom outside
	// it is a typo or an unpromoted experiment, and either one would reach
	// gum.describe_op as a claim gum cannot honour.
	ErrUnknownCapability = errors.New("catalog: UNKNOWN_CAPABILITY")

	// ErrExperimentalCapabilityNotSchemaOnly is the second half of the §913
	// rule: an `x-` atom is searchable metadata only, so the variant carrying
	// it must declare execution_support "schema_only".
	ErrExperimentalCapabilityNotSchemaOnly = errors.New("catalog: experimental x- capability requires execution_support \"schema_only\"")
)

// SupportedCatalogSchemaVersions is the set of catalog_schema_version values the loader accepts.
var SupportedCatalogSchemaVersions = []int{1}

// SupportedBindingSchemaVersions is the set of binding_schema_version values the loader accepts.
var SupportedBindingSchemaVersions = []int{1}

// ── Closed-enum types ───────────────────────────────────────────────────────

// hasXPrefix reports whether s carries an experimental "x-" vendor extension prefix.
func hasXPrefix(s string) bool {
	return len(s) > 2 && s[0] == 'x' && s[1] == '-'
}

// RiskClass is a closed enum per spec.md §5.1.2.
type RiskClass string

const (
	RiskClassRead        RiskClass = "read"
	RiskClassWrite       RiskClass = "write"
	RiskClassDestructive RiskClass = "destructive"
)

// Valid reports whether r is a known RiskClass.
func (r RiskClass) Valid() bool {
	switch r {
	case RiskClassRead, RiskClassWrite, RiskClassDestructive:
		return true
	}
	return false
}

// ExecutionSupport is a closed enum per spec.md §918. It states how much of a
// variant's declared `capabilities[]` the dispatcher can actually execute.
type ExecutionSupport string

const (
	// ExecutionSupportFull means every declared atom executes. §925 forbids
	// `unsupported_capabilities` on this value.
	ExecutionSupportFull ExecutionSupport = "full"
	// ExecutionSupportPartial means at least one declared atom executes and at
	// least one does not. §925 requires `unsupported_capabilities` to list
	// every non-executable atom, which is a strict subset of `capabilities[]`.
	ExecutionSupportPartial ExecutionSupport = "partial"
	// ExecutionSupportTypedExecutorRequired means no declared atom executes
	// through generic dispatch. Invocation returns UNSUPPORTED_CAPABILITY.
	ExecutionSupportTypedExecutorRequired ExecutionSupport = "typed_executor_required"
	// ExecutionSupportSchemaOnly means the variant is metadata-only.
	ExecutionSupportSchemaOnly ExecutionSupport = "schema_only"
)

// Valid reports whether e is a known ExecutionSupport. The empty string is not
// valid here; Op.Validate treats an omitted field as ExecutionSupportFull
// before calling this.
func (e ExecutionSupport) Valid() bool {
	switch e {
	case ExecutionSupportFull, ExecutionSupportPartial,
		ExecutionSupportTypedExecutorRequired, ExecutionSupportSchemaOnly:
		return true
	}
	return false
}

// Stability is a closed enum per spec.md §5.1.
type Stability string

const (
	StabilityStable Stability = "stable"
	StabilityBeta   Stability = "beta"
	StabilityAlpha  Stability = "alpha"
)

// Valid reports whether s is a known Stability.
func (s Stability) Valid() bool {
	switch s {
	case StabilityStable, StabilityBeta, StabilityAlpha:
		return true
	}
	return false
}

// BackendKind is a closed enum per docs/catalog-abi.md "Backend Kind".
type BackendKind string

const (
	BackendKindTypedRestSDK  BackendKind = "typed-rest-sdk"
	BackendKindDiscoveryREST BackendKind = "discovery-rest"
	BackendKindRawHTTP       BackendKind = "raw-http"
	BackendKindGRPCSDK       BackendKind = "grpc-sdk"
	BackendKindMCPPlugin     BackendKind = "mcp-plugin"
	BackendKindGRPCPlugin    BackendKind = "grpc-plugin"
	// BackendKindMapsSDK selects internal/adapters/maps/ which wraps
	// googlemaps.github.io/maps for the Maps Web Service family (Routes,
	// Directions, Geocoding, Places, …). Spec §14 line 3335.
	BackendKindMapsSDK BackendKind = "maps-sdk"
	// BackendKindGenAI selects internal/adapters/genai/ which wraps
	// google.golang.org/genai for Gemini generateContent and friends.
	// Spec §14 line 3334.
	BackendKindGenAI BackendKind = "gen-ai"
	// BackendKindGoogleAdsSDK selects internal/adapters/googleads/ which calls
	// the Google Ads API (googleads.googleapis.com) Keyword Planner methods.
	// Unlike typed-rest-sdk it injects the developer-token and login-customer-id
	// headers server-side: the developer token is a secret sourced from the OS
	// keychain / env, never an invocation arg (so it stays out of the audit log,
	// args_canonical, the cache key, and the MCP tool-call context).
	BackendKindGoogleAdsSDK BackendKind = "google-ads-sdk"
)

// Valid reports whether b is a known BackendKind (stable or experimental x- prefix).
func (b BackendKind) Valid() bool {
	switch b {
	case BackendKindTypedRestSDK, BackendKindDiscoveryREST, BackendKindRawHTTP,
		BackendKindGRPCSDK, BackendKindMCPPlugin, BackendKindGRPCPlugin,
		BackendKindMapsSDK, BackendKindGenAI, BackendKindGoogleAdsSDK:
		return true
	}
	return hasXPrefix(string(b))
}

// InterfaceKind is a closed enum per docs/catalog-abi.md "Interface Kind".
type InterfaceKind string

const (
	InterfaceKindDiscoveryREST InterfaceKind = "discovery-rest"
	InterfaceKindGRPC          InterfaceKind = "grpc"
	InterfaceKindPluginMCP     InterfaceKind = "plugin-mcp"
	InterfaceKindPluginGRPC    InterfaceKind = "plugin-grpc"
	InterfaceKindSDKNative     InterfaceKind = "sdk-native"
)

// Valid reports whether ik is a known InterfaceKind.
func (ik InterfaceKind) Valid() bool {
	switch ik {
	case InterfaceKindDiscoveryREST, InterfaceKindGRPC, InterfaceKindPluginMCP,
		InterfaceKindPluginGRPC, InterfaceKindSDKNative:
		return true
	}
	return hasXPrefix(string(ik))
}

// AuthStrategy is a closed enum per spec.md §7.
// Note: docs/catalog-abi.md §auth_strategy normalizes 'service_account' but spec §7 uses
// 'service_account_key'; both are accepted.
type AuthStrategy string

const (
	AuthStrategyADC               AuthStrategy = "adc"
	AuthStrategyBYOOAuth          AuthStrategy = "byo_oauth"
	AuthStrategyGUMOAuth          AuthStrategy = "gum_oauth"
	AuthStrategyAPIKey            AuthStrategy = "api_key"
	AuthStrategyServiceAccountKey AuthStrategy = "service_account_key"
	AuthStrategyServiceAccount    AuthStrategy = "service_account" // catalog-abi alias
	AuthStrategyNone              AuthStrategy = "none"
	AuthStrategyCompound          AuthStrategy = "compound"
	AuthStrategyPluginManaged     AuthStrategy = "plugin_managed"
)

// Valid reports whether as is a known AuthStrategy.
func (as AuthStrategy) Valid() bool {
	switch as {
	case AuthStrategyADC, AuthStrategyBYOOAuth, AuthStrategyGUMOAuth, AuthStrategyAPIKey,
		AuthStrategyServiceAccountKey, AuthStrategyServiceAccount,
		AuthStrategyNone, AuthStrategyCompound, AuthStrategyPluginManaged:
		return true
	}
	return false
}

// AuthComponentKind is the closed enum of prerequisite component kinds from
// spec.md §7. An unknown kind fails catalog build and plugin install with
// AUTH_COMPONENT_UNKNOWN unless it carries the "x-" informational prefix.
type AuthComponentKind string

const (
	AuthComponentOAuthScopes          AuthComponentKind = "oauth_scopes"
	AuthComponentOAuthClient          AuthComponentKind = "oauth_client"
	AuthComponentAPIEnabledProject    AuthComponentKind = "api_enabled_project"
	AuthComponentAPIKey               AuthComponentKind = "api_key"
	AuthComponentDeveloperToken       AuthComponentKind = "developer_token"
	AuthComponentCustomerID           AuthComponentKind = "customer_id"
	AuthComponentLoginCustomerID      AuthComponentKind = "login_customer_id"
	AuthComponentBillingEnabled       AuthComponentKind = "billing_enabled"
	AuthComponentManagerAccount       AuthComponentKind = "manager_account"
	AuthComponentWorkspaceAdminTrust  AuthComponentKind = "workspace_admin_trust"
	AuthComponentDomainWideDelegation AuthComponentKind = "domain_wide_delegation"
	AuthComponentServiceAccountKey    AuthComponentKind = "service_account_key"
	AuthComponentConsentVerification  AuthComponentKind = "consent_verification"
	AuthComponentOAuthConsentScreen   AuthComponentKind = "oauth_consent_screen"
	AuthComponentQuotaProject         AuthComponentKind = "quota_project"
	AuthComponentServiceAllowlist     AuthComponentKind = "service_allowlist"
	AuthComponentOrgPolicyException   AuthComponentKind = "org_policy_exception"
	AuthComponentOAuthClientSecret    AuthComponentKind = "oauth_client_secret"
	AuthComponentAccountPermission    AuthComponentKind = "account_permission"
)

// AuthComponentKinds is the §7 list in spec order. Tests snapshot its length,
// so a spec revision that adds or drops a kind fails loudly here first.
var AuthComponentKinds = []AuthComponentKind{
	AuthComponentOAuthScopes, AuthComponentOAuthClient, AuthComponentAPIEnabledProject,
	AuthComponentAPIKey, AuthComponentDeveloperToken, AuthComponentCustomerID,
	AuthComponentLoginCustomerID, AuthComponentBillingEnabled, AuthComponentManagerAccount,
	AuthComponentWorkspaceAdminTrust, AuthComponentDomainWideDelegation,
	AuthComponentServiceAccountKey, AuthComponentConsentVerification,
	AuthComponentOAuthConsentScreen, AuthComponentQuotaProject, AuthComponentServiceAllowlist,
	AuthComponentOrgPolicyException, AuthComponentOAuthClientSecret, AuthComponentAccountPermission,
}

// Valid reports whether k is a standardized kind or an "x-" informational one.
func (k AuthComponentKind) Valid() bool {
	if slices.Contains(AuthComponentKinds, k) {
		return true
	}
	return hasXPrefix(string(k))
}

// AuthComponent is one prerequisite of an authorized variant, per spec.md §7
// and docs/catalog-abi.md "auth_components[]". It is a descriptor, never a
// value: secret material lives only in the OS keychain, so this record
// carries the kind and the setup hint that name it.
type AuthComponent struct {
	Kind AuthComponentKind `json:"kind"`
	// Optional marks a component that only some accounts need, such as the
	// Ads login_customer_id a manager account requires. The zero value is
	// "required", so a forgotten flag fails safe.
	Optional bool `json:"optional,omitempty"`
	// Secret marks a component `gum auth setup` collects into the keychain.
	Secret bool `json:"secret,omitempty"`
	// External marks a step GUM can explain but cannot complete, such as
	// getting an Ads developer token approved for standard access.
	External bool `json:"external,omitempty"`
	// SetupHint is user-facing copy. It MUST NOT carry a credential value.
	SetupHint string `json:"setup_hint,omitempty"`
}

// AdminBlastRadius classifies Admin SDK write variants beyond the normal
// read/write/destructive risk class. Unknown Admin writes are treated as high
// blast radius by policy and must not enter the broad preview catalog.
type AdminBlastRadius string

const (
	AdminBlastRadiusFixtureWrite AdminBlastRadius = "admin_fixture_write"
	AdminBlastRadiusReversible   AdminBlastRadius = "admin_reversible_write"
	AdminBlastRadiusHighBlast    AdminBlastRadius = "admin_high_blast_write"
	AdminFixtureMarkerPrefix                      = "gum-fixture-"
)

func (a AdminBlastRadius) Valid() bool {
	switch a {
	case AdminBlastRadiusFixtureWrite, AdminBlastRadiusReversible, AdminBlastRadiusHighBlast:
		return true
	}
	return false
}

// ── Struct types ────────────────────────────────────────────────────────────

// Catalog is the top-level catalog.json shape per spec.md §5.3.
type Catalog struct {
	CatalogSchemaVersion int    `json:"catalog_schema_version"`
	GeneratedAt          string `json:"generated_at"`
	GeneratorVersion     string `json:"generator_version"`
	Ops                  []Op   `json:"ops"`
}

// Validate validates the Catalog and all contained Ops.
// Returns a typed error (one of the Err* sentinels) on the first validation failure.
func (c *Catalog) Validate() error {
	// 1. Check catalog_schema_version is supported.
	if !slices.Contains(SupportedCatalogSchemaVersions, c.CatalogSchemaVersion) {
		return fmt.Errorf("catalog_schema_version %d: %w", c.CatalogSchemaVersion, ErrUnsupportedCatalogSchemaVersion)
	}

	// 2. generated_at non-empty and parses as RFC 3339.
	if c.GeneratedAt == "" {
		return fmt.Errorf("field generated_at: %w", ErrMissingRequiredField)
	}
	if _, err := time.Parse(time.RFC3339, c.GeneratedAt); err != nil {
		return fmt.Errorf("field generated_at: %w", ErrMissingRequiredField)
	}

	// 3. generator_version non-empty.
	if c.GeneratorVersion == "" {
		return fmt.Errorf("field generator_version: %w", ErrMissingRequiredField)
	}

	// 4. Ops non-nil (empty slice is allowed; nil is treated as empty).
	// Go zero value for []Op is nil; treat as valid (empty is allowed).

	// 5. Validate each op; reject a duplicate op_id (findOp returns the FIRST
	// match, so a second op with the same id is silently unreachable — and could
	// shadow the real op with a different risk_class).
	seenOpIDs := make(map[string]struct{}, len(c.Ops))
	for i := range c.Ops {
		if _, dup := seenOpIDs[c.Ops[i].OpID]; dup {
			return fmt.Errorf("op %s: %w", c.Ops[i].OpID, ErrDuplicateOpID)
		}
		seenOpIDs[c.Ops[i].OpID] = struct{}{}
		if err := c.Ops[i].Validate(); err != nil {
			return err
		}
	}

	return nil
}

// Op is a single operation entry in catalog.json per spec.md §5.3.
type Op struct {
	OpID                 string            `json:"op_id"`
	OpSchemaVersion      int               `json:"op_schema_version"`
	Title                string            `json:"title"`
	Summary              string            `json:"summary"`
	ParamsRequired       [][]string        `json:"params_required,omitempty"`
	ParamsOptional       [][]string        `json:"params_optional,omitempty"`
	ResponseRef          string            `json:"response_ref,omitempty"`
	Paginated            bool              `json:"paginated,omitempty"`
	Tags                 []string          `json:"tags,omitempty"`
	ServiceFamily        string            `json:"service_family,omitempty"`
	Service              string            `json:"service,omitempty"`
	DefaultVariantID     string            `json:"default_variant_id"`
	Variants             []Variant         `json:"variants"`
	DeprecatedOpIDs      []string          `json:"deprecated_op_ids,omitempty"`
	DeprecatedVariantIDs []string          `json:"deprecated_variant_ids,omitempty"`
	SupersededVariantIDs map[string]string `json:"superseded_variant_ids,omitempty"`
	// RequestFields describes the operation's request parameters with enough
	// detail for the CLI to derive typed flags and route each value to the right
	// place (URL path, query string, or JSON body). Populated from the source
	// Google Discovery document (or hand-authored for manual ops). Optional and
	// additive: ops without it fall back to the opaque body:=json grammar.
	RequestFields []RequestField `json:"request_fields,omitempty"`
	// ExampleArgs overlays the args map `gum describe` synthesizes for this op.
	// The synthesizer covers required fields only, so an optional field whose
	// omission changes what the answer means has to be curated here. Keyword
	// Planner geo and language are the case that forced it: omit either and the
	// figures cover every location or every language, and nothing in the
	// response says so. Additive and optional: ops without it keep the
	// synthesized example unchanged, so older binaries ignore it safely.
	ExampleArgs map[string]any `json:"example_args,omitempty"`
}

// RequestFieldLocation is where a request field is carried in the HTTP call.
type RequestFieldLocation string

const (
	// RequestFieldPath is substituted into the URL path template ({name}).
	RequestFieldPath RequestFieldLocation = "path"
	// RequestFieldQuery is appended to the query string.
	RequestFieldQuery RequestFieldLocation = "query"
	// RequestFieldBody is assembled into the JSON request body.
	RequestFieldBody RequestFieldLocation = "body"
	// RequestFieldArg is a top-level invocation argument with no HTTP routing —
	// used by plugin / non-HTTP ops whose args pass straight to the executor.
	// It stays in the args map as-is (the body assembler only moves "body").
	RequestFieldArg RequestFieldLocation = "arg"
	// RequestFieldHeader is carried as an HTTP request header. The arg name →
	// header name mapping lives in the variant's Binding.HTTP.HeaderParams (the
	// adapter has the binding but not the op's RequestFields), so the field
	// documents/validates the input while the binding routes it. Used by APIs
	// like Places (New) / Routes that require an X-Goog-FieldMask header.
	RequestFieldHeader RequestFieldLocation = "header"
)

// RequestField is one input parameter of an operation, decomposed from the
// source API schema so the CLI can offer a typed flag and the dispatch path can
// route the value correctly. The deterministic §12.0 grammar still accepts the
// opaque forms (key=value, body:=json); RequestField is purely additive,
// enabling the ergonomic flag/wizard surface on top.
type RequestField struct {
	Name     string               `json:"name"`
	Location RequestFieldLocation `json:"location"`
	Type     string               `json:"type"`                // string|integer|number|boolean|array|object
	ItemType string               `json:"item_type,omitempty"` // element type when Type=="array"
	Enum     []string             `json:"enum,omitempty"`
	Required bool                 `json:"required,omitempty"`
	// Default is the value the dispatcher sends when the caller omits this arg
	// (encoded as a string; DefaultValue decodes it to the declared Type). It is
	// load-bearing, not documentation: whatever a field declares here is what
	// goes on the wire, so a field must not declare a default whose effect
	// differs from omitting the arg (gum-3gcv). Leave it empty when omission is
	// meant to reach the upstream API untouched.
	Default     string `json:"default,omitempty"`
	Format      string `json:"format,omitempty"` // e.g. date|date-time|google-datetime|int64
	Description string `json:"description,omitempty"`
}

// HasDefault reports whether the field declares a default value.
func (f RequestField) HasDefault() bool { return f.Default != "" }

// DefaultValue decodes the declared Default string into the JSON value the
// dispatcher injects when the arg is absent, using the field's declared Type
// (and ItemType for arrays). An array default accepts either a JSON array
// literal ("[\"a\",\"b\"]") or a bare scalar, which becomes a one-element array.
// An object default must be a JSON object literal.
//
// It returns an error when the declared default cannot be decoded to the
// declared type, so a catalog invariant test can reject the field at build time
// instead of the dispatcher sending a wrongly-typed value at run time.
func (f RequestField) DefaultValue() (any, error) {
	if f.Default == "" {
		return nil, fmt.Errorf("field %s: %w", f.Name, ErrNoDefaultDeclared)
	}
	switch f.Type {
	case "array":
		if strings.HasPrefix(strings.TrimSpace(f.Default), "[") {
			var arr []any
			if err := json.Unmarshal([]byte(f.Default), &arr); err != nil {
				return nil, fmt.Errorf("field %s: default %q is not a JSON array: %w", f.Name, f.Default, err)
			}
			return arr, nil
		}
		elem, err := decodeScalarDefault(f.Name, f.Default, f.ItemType)
		if err != nil {
			return nil, err
		}
		return []any{elem}, nil
	case "object":
		var obj map[string]any
		if err := json.Unmarshal([]byte(f.Default), &obj); err != nil {
			return nil, fmt.Errorf("field %s: default %q is not a JSON object: %w", f.Name, f.Default, err)
		}
		return obj, nil
	default:
		return decodeScalarDefault(f.Name, f.Default, f.Type)
	}
}

// decodeScalarDefault parses s as the named JSON scalar type. An empty or
// unrecognized type is treated as string, matching the catalog's convention that
// an unset type means an opaque string arg.
func decodeScalarDefault(field, s, typ string) (any, error) {
	switch typ {
	case "integer":
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("field %s: default %q is not an integer: %w", field, s, err)
		}
		return n, nil
	case "number":
		n, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return nil, fmt.Errorf("field %s: default %q is not a number: %w", field, s, err)
		}
		return n, nil
	case "boolean":
		b, err := strconv.ParseBool(s)
		if err != nil {
			return nil, fmt.Errorf("field %s: default %q is not a boolean: %w", field, s, err)
		}
		return b, nil
	}
	return s, nil
}

// Validate validates the Op and its variants.
func (op *Op) Validate() error {
	// 1. OpID non-empty.
	if op.OpID == "" {
		return fmt.Errorf("op (empty id): %w", ErrMissingRequiredField)
	}

	// 2. OpSchemaVersion > 0.
	if op.OpSchemaVersion <= 0 {
		return fmt.Errorf("op %s: field op_schema_version: %w", op.OpID, ErrMissingRequiredField)
	}

	// 3. Title non-empty.
	if op.Title == "" {
		return fmt.Errorf("op %s: field title: %w", op.OpID, ErrMissingRequiredField)
	}

	// 4. Summary non-empty.
	if op.Summary == "" {
		return fmt.Errorf("op %s: field summary: %w", op.OpID, ErrMissingRequiredField)
	}

	// 5. DefaultVariantID non-empty.
	if op.DefaultVariantID == "" {
		return fmt.Errorf("op %s: field default_variant_id: %w", op.OpID, ErrMissingRequiredField)
	}

	// 6. Variants non-empty.
	if len(op.Variants) == 0 {
		return fmt.Errorf("op %s: field variants: %w", op.OpID, ErrMissingRequiredField)
	}

	// 7. DefaultVariantID must reference an existing variant.
	if !slices.ContainsFunc(op.Variants, func(v Variant) bool { return v.VariantID == op.DefaultVariantID }) {
		return fmt.Errorf("op %s: %w", op.OpID, ErrDanglingDefaultVariantID)
	}

	// 8. Validate each variant; reject a duplicate variant_id (resolveVariant
	// iterates by index and the second one with a shared id is silently shadowed).
	seenVariantIDs := make(map[string]struct{}, len(op.Variants))
	for _, v := range op.Variants {
		if v.VariantID == "" {
			return fmt.Errorf("op %s: variant (empty id): %w", op.OpID, ErrMissingRequiredField)
		}
		if _, dup := seenVariantIDs[v.VariantID]; dup {
			return fmt.Errorf("op %s: variant %s: %w", op.OpID, v.VariantID, ErrDuplicateVariantID)
		}
		seenVariantIDs[v.VariantID] = struct{}{}
		// variant_schema_version is a required version gate (catalog-abi.md): a
		// future dispatch-shape change keys on it, so a variant shipped with 0
		// (forgotten) must fail fast at catalog build, not mis-dispatch later.
		if v.VariantSchemaVersion <= 0 {
			return fmt.Errorf("op %s: variant %s: field variant_schema_version: %w", op.OpID, v.VariantID, ErrMissingRequiredField)
		}
		if !v.Stability.Valid() {
			return fmt.Errorf("op %s: variant %s: %w", op.OpID, v.VariantID, ErrUnknownStability)
		}
		if !v.InterfaceKind.Valid() {
			return fmt.Errorf("op %s: variant %s: %w", op.OpID, v.VariantID, ErrUnknownInterfaceKind)
		}
		if !v.BackendKind.Valid() {
			return fmt.Errorf("op %s: variant %s: %w", op.OpID, v.VariantID, ErrUnknownBackendKind)
		}
		if !v.RiskClass.Valid() {
			return fmt.Errorf("op %s: variant %s: %w", op.OpID, v.VariantID, ErrUnknownRiskClass)
		}
		if v.AuthStrategy != "" && !v.AuthStrategy.Valid() {
			return fmt.Errorf("op %s: variant %s: %w", op.OpID, v.VariantID, ErrUnknownAuthStrategy)
		}
		for _, comp := range v.AuthComponents {
			if !comp.Kind.Valid() {
				return fmt.Errorf("op %s: variant %s: auth_components kind %q: %w", op.OpID, v.VariantID, comp.Kind, ErrUnknownAuthComponent)
			}
		}
		if err := v.validateCapabilities(op.OpID); err != nil {
			return err
		}
		if err := v.validateExecutionSupport(op.OpID); err != nil {
			return err
		}
		if op.Service == "admin" && v.RiskClass != RiskClassRead {
			if err := v.AdminPolicy.Validate(); err != nil {
				return fmt.Errorf("op %s: variant %s: admin_policy: %w", op.OpID, v.VariantID, err)
			}
		}
		if v.Binding != nil {
			if !slices.Contains(SupportedBindingSchemaVersions, v.Binding.BindingSchemaVersion) {
				return fmt.Errorf("op %s: variant %s: %w", op.OpID, v.VariantID, ErrUnsupportedBindingSchemaVersion)
			}
			if v.Binding.AdapterKey == "" {
				return fmt.Errorf("op %s: variant %s: binding field adapter_key: %w", op.OpID, v.VariantID, ErrMissingRequiredField)
			}
			if v.Binding.OperationKey == "" {
				return fmt.Errorf("op %s: variant %s: binding field operation_key: %w", op.OpID, v.VariantID, ErrMissingRequiredField)
			}
		}
		if v.ServiceRootTemplate != "" {
			return fmt.Errorf("op %s: variant %s: service_root_template %q: %w", op.OpID, v.VariantID, v.ServiceRootTemplate, ErrServiceRootTemplateDeferred)
		}
		if err := op.validateRoutingHeaders(v); err != nil {
			return err
		}
	}

	return nil
}

// validateExecutionSupport enforces the spec §925 binding between a variant's
// `execution_support` and its `unsupported_capabilities`. The catalog is the
// declaration site: `gum.describe_op` reads the field straight through, so an
// unchecked variant here becomes a wrong `DescribeOpResult` downstream.
//
// An omitted `execution_support` resolves to "full", which is what every
// variant shipped today means by leaving it out.
func (v Variant) validateExecutionSupport(opID string) error {
	support := v.ExecutionSupport
	if support == "" {
		support = ExecutionSupportFull
	}
	if !support.Valid() {
		return fmt.Errorf("op %s: variant %s: execution_support %q: %w", opID, v.VariantID, v.ExecutionSupport, ErrUnknownExecutionSupport)
	}

	if support == ExecutionSupportFull {
		if len(v.UnsupportedCapabilities) > 0 {
			return fmt.Errorf("op %s: variant %s: %w", opID, v.VariantID, ErrUnexpectedUnsupportedCapabilities)
		}
		return nil
	}

	// Every listed atom must be one the variant declares. A name outside
	// `capabilities[]` is a typo or a stale atom, and it would reach the
	// describe_op payload unchecked.
	for _, atom := range v.UnsupportedCapabilities {
		if !slices.Contains(v.Capabilities, atom) {
			return fmt.Errorf("op %s: variant %s: unsupported_capabilities %q: %w", opID, v.VariantID, atom, ErrUndeclaredUnsupportedCapability)
		}
	}

	if support == ExecutionSupportPartial {
		// §918 defines "partial" as a mixed variant: at least one declared
		// atom executes and at least one does not.
		if len(v.UnsupportedCapabilities) == 0 {
			return fmt.Errorf("op %s: variant %s: execution_support %q: %w", opID, v.VariantID, support, ErrMissingUnsupportedCapabilities)
		}
		for _, atom := range v.Capabilities {
			if !slices.Contains(v.UnsupportedCapabilities, atom) {
				return nil
			}
		}
		return fmt.Errorf("op %s: variant %s: %w", opID, v.VariantID, ErrPartialWithNoExecutableCapability)
	}

	// "typed_executor_required" and "schema_only" block every declared atom,
	// so the list must name all of them. A variant that declares no atoms has
	// nothing to list and passes with an empty list.
	for _, atom := range v.Capabilities {
		if !slices.Contains(v.UnsupportedCapabilities, atom) {
			return fmt.Errorf("op %s: variant %s: execution_support %q omits capability %q: %w", opID, v.VariantID, support, atom, ErrMissingUnsupportedCapabilities)
		}
	}

	return nil
}

// Variant is an executable backend variant per spec.md §5.3 and docs/catalog-abi.md.
type Variant struct {
	VariantID             string           `json:"variant_id"`
	VariantSchemaVersion  int              `json:"variant_schema_version"`
	Version               string           `json:"version,omitempty"`
	Stability             Stability        `json:"stability"`
	InterfaceKind         InterfaceKind    `json:"interface_kind"`
	BackendKind           BackendKind      `json:"backend_kind"`
	Preferred             bool             `json:"preferred,omitempty"`
	RiskClass             RiskClass        `json:"risk_class"`
	AuthStrategy          AuthStrategy     `json:"auth_strategy,omitempty"`
	AuthComponents        []AuthComponent  `json:"auth_components,omitempty"`
	ConfirmationPolicy    string           `json:"confirmation_policy,omitempty"`
	Capabilities          []string         `json:"capabilities,omitempty"`
	Scopes                []string         `json:"scopes,omitempty"`
	DefaultFields         string           `json:"default_fields,omitempty"`
	DefaultPageSize       int              `json:"default_page_size,omitempty"`
	DefaultFormat         string           `json:"default_format,omitempty"`
	OutputProfile         string           `json:"output_profile,omitempty"`
	NullElisionSafeFields []string         `json:"null_elision_safe_fields,omitempty"`
	ExecutionSupport      ExecutionSupport `json:"execution_support,omitempty"`
	// UnsupportedCapabilities names the atoms of Capabilities that this variant
	// cannot execute. §925 binds it to ExecutionSupport: absent on "full",
	// every non-executable atom on "partial", every blocking atom on
	// "typed_executor_required" and "schema_only".
	UnsupportedCapabilities []string     `json:"unsupported_capabilities,omitempty"`
	RiskOverride            bool         `json:"risk_override,omitempty"`
	RiskOverrideReason      string       `json:"risk_override_reason,omitempty"`
	AdminPolicy             *AdminPolicy `json:"admin_policy,omitempty"`
	StubExpires             string       `json:"stub_expires,omitempty"`
	Quarantined             bool         `json:"quarantined,omitempty"`
	Annotations             *Annotation  `json:"annotations,omitempty"`
	Binding                 *Binding     `json:"binding,omitempty"`
	// ServiceRootTemplate is a deferred field — see docs/catalog-abi.md §57-85.
	// Validation rejects any variant that sets this field until the feature is implemented.
	ServiceRootTemplate string `json:"service_root_template,omitempty"`
}

// AdminPolicy is required on Admin SDK write/destructive variants. It records
// the Admin-specific blast-radius decision that allowed the variant into the
// catalog and the fixture ownership gate future live tests must satisfy.
type AdminPolicy struct {
	BlastRadius              AdminBlastRadius `json:"blast_radius"`
	FixtureOwnershipRequired bool             `json:"fixture_ownership_required,omitempty"`
	FixtureMarkerPrefix      string           `json:"fixture_marker_prefix,omitempty"`
	FixtureResourceKeys      []string         `json:"fixture_resource_keys,omitempty"`
}

func (p *AdminPolicy) Validate() error {
	if p == nil {
		return ErrMissingAdminPolicy
	}
	if !p.BlastRadius.Valid() {
		return ErrUnknownAdminBlastRadius
	}
	if p.BlastRadius == AdminBlastRadiusFixtureWrite {
		if !p.FixtureOwnershipRequired {
			return fmt.Errorf("fixture ownership required: %w", ErrMissingAdminPolicy)
		}
		if p.FixtureMarkerPrefix == "" {
			return fmt.Errorf("fixture marker prefix: %w", ErrMissingAdminPolicy)
		}
		if len(p.FixtureResourceKeys) == 0 {
			return fmt.Errorf("fixture resource keys: %w", ErrMissingAdminPolicy)
		}
	}
	return nil
}

// Binding is the backend-kind-specific binding object per docs/catalog-abi.md.
type Binding struct {
	BindingSchemaVersion int    `json:"binding_schema_version"`
	AdapterKey           string `json:"adapter_key"`
	OperationKey         string `json:"operation_key"`
	RequestRef           string `json:"request_ref,omitempty"`
	ResponseRef          string `json:"response_ref,omitempty"`

	// REST fields (typed-rest-sdk, discovery-rest, raw-http)
	GoPkg  string       `json:"go_pkg,omitempty"`
	GoCall string       `json:"go_call,omitempty"`
	HTTP   *HTTPBinding `json:"http,omitempty"`

	// gRPC-sdk fields
	ProtoService   string   `json:"proto_service,omitempty"`
	ProtoMethod    string   `json:"proto_method,omitempty"`
	RequestType    string   `json:"request_type,omitempty"`
	ResponseType   string   `json:"response_type,omitempty"`
	RoutingHeaders []string `json:"routing_headers,omitempty"`

	// sdk-native fields
	SDKResource string `json:"sdk_resource,omitempty"`
	SDKMethod   string `json:"sdk_method,omitempty"`

	// plugin fields (mcp-plugin, grpc-plugin)
	PluginName string `json:"plugin_name,omitempty"`
	ToolName   string `json:"tool_name,omitempty"`
	RPCService string `json:"rpc_service,omitempty"`
	RPCMethod  string `json:"rpc_method,omitempty"`
}

// HTTPBinding holds HTTP method and path for REST variants.
type HTTPBinding struct {
	Method string `json:"method"`
	Path   string `json:"path"`
	// HeaderParams maps an invocation arg name to the HTTP request header it is
	// sent as (e.g. {"fieldMask": "X-Goog-FieldMask"}). Args listed here are
	// routed to headers by the REST adapter instead of the query string. Empty
	// for the vast majority of ops.
	HeaderParams map[string]string `json:"header_params,omitempty"`
}

// Scope describes an OAuth scope requirement.
type Scope struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// Capability names an executable capability atom per spec.md §5.8.
type Capability = string

// CapabilityLROReturn marks a variant whose upstream method returns a
// google.longrunning.Operation rather than the finished resource. It is the
// single LRO classification the Catalog ABI carries: spec.md §5.8 lists the
// atom in the closed capabilities enum, and the §6.1 code-mode gate keys on
// it (gum-bgli).
const CapabilityLROReturn Capability = "lro_return"

// ReturnsLRO reports whether the variant is classified lro_return.
func (v Variant) ReturnsLRO() bool {
	return slices.Contains(v.Capabilities, CapabilityLROReturn)
}

// DefaultVariantReturnsLRO reports whether the op's default variant is
// classified lro_return. An op whose default_variant_id names no variant
// reports false; Op.Validate rejects that record at catalog build, so the
// only callers that can observe it hold a hand-built Op.
func (op *Op) DefaultVariantReturnsLRO() bool {
	for _, v := range op.Variants {
		if v.VariantID == op.DefaultVariantID {
			return v.ReturnsLRO()
		}
	}
	return false
}

// Annotation holds MCP tool hint flags per spec.md §4.1 / go-sdk v1.6.0 semantics.
type Annotation struct {
	ReadOnly   bool `json:"readOnly,omitempty"`
	OpenWorld  bool `json:"openWorld,omitempty"`
	Idempotent bool `json:"idempotent,omitempty"`
}

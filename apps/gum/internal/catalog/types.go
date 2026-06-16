// Package catalog holds the build-time Google capability catalog ABI (spec.md §5.3, §14).
//
// Types only in this package; generation logic lives in cmd/gen-catalog.
package catalog

import (
	"errors"
	"fmt"
	"slices"
	"time"
)

// Sentinel errors for typed matching in tests.
var (
	ErrMissingRequiredField            = errors.New("catalog: missing required field")
	ErrUnknownRiskClass                = errors.New("catalog: unknown risk_class")
	ErrUnknownAuthStrategy             = errors.New("catalog: unknown auth_strategy")
	ErrUnknownBackendKind              = errors.New("catalog: unknown backend_kind")
	ErrUnknownInterfaceKind            = errors.New("catalog: unknown interface_kind")
	ErrUnknownAdminBlastRadius         = errors.New("catalog: unknown admin blast_radius")
	ErrMissingAdminPolicy              = errors.New("catalog: missing admin_policy")
	ErrAdminFixtureOwnership           = errors.New("catalog: admin fixture ownership violation")
	ErrUnknownStability                = errors.New("catalog: unknown stability")
	ErrDanglingDefaultVariantID        = errors.New("catalog: default_variant_id references a variant_id not in variants[]")
	ErrDuplicateOpID                   = errors.New("catalog: duplicate op_id")
	ErrDuplicateVariantID              = errors.New("catalog: duplicate variant_id within an op")
	ErrUnsupportedBindingSchemaVersion = errors.New("catalog: unsupported binding_schema_version")
	ErrUnsupportedCatalogSchemaVersion = errors.New("catalog: unsupported catalog_schema_version")
	ErrServiceRootTemplateDeferred     = errors.New("catalog: SERVICE_ROOT_TEMPLATE_DEFERRED")
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
	AuthStrategyWorkloadIdentity  AuthStrategy = "workload_identity"
	AuthStrategyImpersonation     AuthStrategy = "impersonation"
	AuthStrategyNone              AuthStrategy = "none"
	AuthStrategyCompound          AuthStrategy = "compound"
	AuthStrategyPluginManaged     AuthStrategy = "plugin_managed"
)

// Valid reports whether as is a known AuthStrategy.
func (as AuthStrategy) Valid() bool {
	switch as {
	case AuthStrategyADC, AuthStrategyBYOOAuth, AuthStrategyGUMOAuth, AuthStrategyAPIKey,
		AuthStrategyServiceAccountKey, AuthStrategyServiceAccount,
		AuthStrategyWorkloadIdentity, AuthStrategyImpersonation,
		AuthStrategyNone, AuthStrategyCompound, AuthStrategyPluginManaged:
		return true
	}
	return false
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
	Name        string               `json:"name"`
	Location    RequestFieldLocation `json:"location"`
	Type        string               `json:"type"`                // string|integer|number|boolean|array|object
	ItemType    string               `json:"item_type,omitempty"` // element type when Type=="array"
	Enum        []string             `json:"enum,omitempty"`
	Required    bool                 `json:"required,omitempty"`
	Default     string               `json:"default,omitempty"`
	Format      string               `json:"format,omitempty"` // e.g. date|date-time|google-datetime|int64
	Description string               `json:"description,omitempty"`
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
	}

	return nil
}

// Variant is an executable backend variant per spec.md §5.3 and docs/catalog-abi.md.
type Variant struct {
	VariantID             string        `json:"variant_id"`
	VariantSchemaVersion  int           `json:"variant_schema_version"`
	Version               string        `json:"version,omitempty"`
	Stability             Stability     `json:"stability"`
	InterfaceKind         InterfaceKind `json:"interface_kind"`
	BackendKind           BackendKind   `json:"backend_kind"`
	Preferred             bool          `json:"preferred,omitempty"`
	RiskClass             RiskClass     `json:"risk_class"`
	AuthStrategy          AuthStrategy  `json:"auth_strategy,omitempty"`
	ConfirmationPolicy    string        `json:"confirmation_policy,omitempty"`
	Capabilities          []string      `json:"capabilities,omitempty"`
	Scopes                []string      `json:"scopes,omitempty"`
	DefaultFields         string        `json:"default_fields,omitempty"`
	DefaultPageSize       int           `json:"default_page_size,omitempty"`
	DefaultFormat         string        `json:"default_format,omitempty"`
	OutputProfile         string        `json:"output_profile,omitempty"`
	NullElisionSafeFields []string      `json:"null_elision_safe_fields,omitempty"`
	ExecutionSupport      string        `json:"execution_support,omitempty"`
	RiskOverride          bool          `json:"risk_override,omitempty"`
	RiskOverrideReason    string        `json:"risk_override_reason,omitempty"`
	AdminPolicy           *AdminPolicy  `json:"admin_policy,omitempty"`
	StubExpires           string        `json:"stub_expires,omitempty"`
	Quarantined           bool          `json:"quarantined,omitempty"`
	Annotations           *Annotation   `json:"annotations,omitempty"`
	Binding               *Binding      `json:"binding,omitempty"`
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

// Annotation holds MCP tool hint flags per spec.md §4.1 / go-sdk v1.6.0 semantics.
type Annotation struct {
	ReadOnly   bool `json:"readOnly,omitempty"`
	OpenWorld  bool `json:"openWorld,omitempty"`
	Idempotent bool `json:"idempotent,omitempty"`
}

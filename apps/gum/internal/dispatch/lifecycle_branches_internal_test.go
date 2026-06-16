package dispatch

import (
	"context"
	"testing"

	"github.com/ehmo/gum/internal/catalog"
)

// TestFindOpNilSnapshot pins lifecycle.go:493-495 — findOp returns nil when
// the dispatcher has no catalog snapshot. A zero-value dispatcher (snapshot
// nil) must not panic dereferencing d.snapshot.Ops.
func TestFindOpNilSnapshot(t *testing.T) {
	d := &dispatcher{}
	if op := d.findOp("anything.op"); op != nil {
		t.Errorf("findOp on nil snapshot = %v; want nil", op)
	}
}

// TestValidateParamsSkipsMalformedPairs pins the `len(pair) != 2 → continue`
// guards for both ParamsRequired (lifecycle.go:537-538) and ParamsOptional
// (lifecycle.go:553-554). A catalog param entry that is not a [name,type]
// pair is malformed and must be skipped rather than indexed out of range.
func TestValidateParamsSkipsMalformedPairs(t *testing.T) {
	op := &catalog.Op{
		// Each slice mixes one malformed (len!=2) entry with one valid pair
		// so the loop must hit both the continue arm and the normal path.
		ParamsRequired: [][]string{{"onlyone"}, {"q", "string"}},
		ParamsOptional: [][]string{{"a", "b", "c"}, {"limit", "number"}},
	}
	args := map[string]any{"q": "hi", "limit": float64(5)}
	missing, unknown, typeErrors := validateParams(op, args)
	if len(missing) != 0 {
		t.Errorf("missing = %v; want none (malformed required pair skipped, q provided)", missing)
	}
	if len(unknown) != 0 {
		t.Errorf("unknown = %v; want none", unknown)
	}
	if len(typeErrors) != 0 {
		t.Errorf("typeErrors = %v; want none", typeErrors)
	}
}

// TestValidateParamsAdmitsRequestFields pins that RequestFields are admitted as
// valid params even when an op declares a narrower (hand-authored)
// params_optional allowlist — otherwise a typed flag / key=value for a real
// Discovery-derived parameter would be wrongly rejected as "unknown".
func TestValidateParamsAdmitsRequestFields(t *testing.T) {
	op := &catalog.Op{
		// Hand-authored allowlist is narrower than the real param set.
		ParamsOptional: [][]string{{"customer", "string"}},
		RequestFields: []catalog.RequestField{
			{Name: "customer", Location: catalog.RequestFieldQuery, Type: "string"},
			{Name: "orderBy", Location: catalog.RequestFieldQuery, Type: "string"},  // not in params_optional
			{Name: "startDate", Location: catalog.RequestFieldBody, Type: "string"}, // body -> "body" key
		},
	}
	// orderBy (a RequestField not in params_optional) and body must be accepted.
	_, unknown, _ := validateParams(op, map[string]any{"orderBy": "EMAIL", "body": map[string]any{"startDate": "x"}})
	if len(unknown) != 0 {
		t.Errorf("unknown = %v; want none (RequestFields + body must be admitted)", unknown)
	}
	// A genuinely unknown key is still rejected.
	_, unknown2, _ := validateParams(op, map[string]any{"bogusKey": "x"})
	if len(unknown2) != 1 || unknown2[0] != "bogusKey" {
		t.Errorf("unknown = %v; want [bogusKey]", unknown2)
	}
}

// TestValidateParamsRequestFieldsOnlyRejectsUnknown pins the gum-gatw fix: an op
// with RequestFields but NO params_required/params_optional (the 84 enriched
// ops) now rejects unknown args locally instead of forwarding them to Google.
func TestValidateParamsRequestFieldsOnlyRejectsUnknown(t *testing.T) {
	op := &catalog.Op{
		RequestFields: []catalog.RequestField{
			{Name: "userId", Location: catalog.RequestFieldPath, Type: "string"},
			{Name: "q", Location: catalog.RequestFieldQuery, Type: "string"},
			{Name: "maxResults", Location: catalog.RequestFieldQuery, Type: "integer"},
		},
	}
	_, unknown, _ := validateParams(op, map[string]any{"userId": "me", "emailq": "hello"})
	if len(unknown) != 1 || unknown[0] != "emailq" {
		t.Errorf("unknown = %v; want [emailq] (RequestFields-only schema must reject unknown args)", unknown)
	}
}

// TestValidateParamsRequestFieldsOnlyAcceptsKnown pins that valid RequestField
// names are accepted when no params lists exist.
func TestValidateParamsRequestFieldsOnlyAcceptsKnown(t *testing.T) {
	op := &catalog.Op{
		RequestFields: []catalog.RequestField{
			{Name: "userId", Location: catalog.RequestFieldPath, Type: "string"},
			{Name: "q", Location: catalog.RequestFieldQuery, Type: "string"},
		},
	}
	_, unknown, _ := validateParams(op, map[string]any{"userId": "me", "q": "from:bob"})
	if len(unknown) != 0 {
		t.Errorf("unknown = %v; want none for known RequestField names", unknown)
	}
}

// TestValidateParamsRequestFieldsOnlyAdmitsBody pins that the reserved "body"
// key is always allowed even when no RequestField has location=body — enriched
// POST ops carry body:=json but their RequestFields list only path/query params.
func TestValidateParamsRequestFieldsOnlyAdmitsBody(t *testing.T) {
	op := &catalog.Op{
		RequestFields: []catalog.RequestField{
			{Name: "userId", Location: catalog.RequestFieldPath, Type: "string"},
			{Name: "id", Location: catalog.RequestFieldPath, Type: "string"},
		},
	}
	_, unknown, _ := validateParams(op, map[string]any{"userId": "me", "id": "msg123", "body": map[string]any{}})
	if len(unknown) != 0 {
		t.Errorf("unknown = %v; want none — 'body' must always be allowed (gum-gatw risk 1)", unknown)
	}
}

// TestValidateParamsRequestFieldsOnlyAdmitsHostControl pins that host-control
// keys (fields, pageToken, pageSize, maxResults) are always accepted, even when
// the Discovery doc for this op does not list them as parameters.
func TestValidateParamsRequestFieldsOnlyAdmitsHostControl(t *testing.T) {
	op := &catalog.Op{
		RequestFields: []catalog.RequestField{
			{Name: "userId", Location: catalog.RequestFieldPath, Type: "string"},
		},
	}
	_, unknown, _ := validateParams(op, map[string]any{
		"userId":     "me",
		"fields":     "id,name",
		"pageToken":  "nextPageToken",
		"pageSize":   10,
		"maxResults": 50,
	})
	if len(unknown) != 0 {
		t.Errorf("unknown = %v; want none — host-control keys must always be allowed (gum-gatw risk 2)", unknown)
	}
}

// TestValidateParamsRequestFieldsOnlyAdmitsSystemParams pins that Google's
// global system parameters (absent from per-method RequestFields) are accepted
// on an enriched op rather than wrongly rejected as unknown.
func TestValidateParamsRequestFieldsOnlyAdmitsSystemParams(t *testing.T) {
	op := &catalog.Op{
		RequestFields: []catalog.RequestField{
			{Name: "userId", Location: catalog.RequestFieldPath, Type: "string"},
		},
	}
	_, unknown, _ := validateParams(op, map[string]any{
		"userId":      "me",
		"alt":         "json",
		"prettyPrint": false,
		"quotaUser":   "tenant-42",
		"$.xgafv":     "2",
	})
	if len(unknown) != 0 {
		t.Errorf("unknown = %v; want none — Google global system params must be allowed", unknown)
	}
}

// TestValidateParamsTrulyOpenSchemaAcceptsAnything pins that an op with neither
// params lists nor RequestFields stays fully open (preserves the behavior of
// searchconsole.sites.list and calendar.colors.get).
func TestValidateParamsTrulyOpenSchemaAcceptsAnything(t *testing.T) {
	op := &catalog.Op{}
	missing, unknown, typeErrors := validateParams(op, map[string]any{"anyKey": "v", "anotherKey": 42})
	if missing != nil || unknown != nil || typeErrors != nil {
		t.Errorf("(missing=%v, unknown=%v, typeErrors=%v); want all nil for truly open schema", missing, unknown, typeErrors)
	}
}

// TestResolveVariantOpNotFound pins resolveVariant's defensive op==nil arm
// (lifecycle.go:860-863). resolveVariant re-looks-up the op; calling it with
// an OpID absent from the snapshot must surface OP_NOT_FOUND rather than
// panic.
func TestResolveVariantOpNotFound(t *testing.T) {
	d := &dispatcher{snapshot: &catalog.Catalog{CatalogSchemaVersion: 1}}
	_, se := d.resolveVariant(context.Background(), &Invocation{OpID: "ghost.op"})
	if se == nil {
		t.Fatal("resolveVariant on missing op = nil error; want OP_NOT_FOUND")
	}
	if se.ErrCode != ErrCodeOpNotFound {
		t.Errorf("ErrCode = %q; want %q", se.ErrCode, ErrCodeOpNotFound)
	}
}

// TestResolveVariantAllQuarantinedNoDefault pins lifecycle.go:909-915 — when
// no variant_id is pinned, no default_variant_id matches, and every variant
// is quarantined, filterQuarantined yields an empty active set and the
// resolver must return VARIANT_QUARANTINED naming the first variant.
func TestResolveVariantAllQuarantinedNoDefault(t *testing.T) {
	op := catalog.Op{
		OpID:            "q.op",
		OpSchemaVersion: 1,
		// No DefaultVariantID so Step 1 is skipped; both variants quarantined
		// so Step 2's active set is empty.
		Variants: []catalog.Variant{
			{VariantID: "v1", Quarantined: true},
			{VariantID: "v2", Quarantined: true},
		},
	}
	d := &dispatcher{snapshot: &catalog.Catalog{CatalogSchemaVersion: 1, Ops: []catalog.Op{op}}}
	_, se := d.resolveVariant(context.Background(), &Invocation{OpID: "q.op"})
	if se == nil {
		t.Fatal("resolveVariant all-quarantined = nil error; want VARIANT_QUARANTINED")
	}
	if se.ErrCode != ErrCodeVariantQuarantined {
		t.Errorf("ErrCode = %q; want %q", se.ErrCode, ErrCodeVariantQuarantined)
	}
	if se.Detail["variant_id"] != "v1" {
		t.Errorf("variant_id detail = %v; want first variant v1", se.Detail["variant_id"])
	}
}

// TestValidateParamsEnforcesRequiredPathField pins the audit fix: an enriched op
// (RequestFields, no params lists) with a required PATH field reports it missing
// when omitted, so the MCP path gets a clean INVALID_ARGS instead of a malformed
// URL / opaque 400.
func TestValidateParamsEnforcesRequiredPathField(t *testing.T) {
	op := &catalog.Op{
		RequestFields: []catalog.RequestField{
			{Name: "userId", Location: catalog.RequestFieldPath, Type: "string", Required: true},
			{Name: "q", Location: catalog.RequestFieldQuery, Type: "string"},
		},
	}
	missing, _, _ := validateParams(op, map[string]any{"q": "x"})
	if len(missing) != 1 || missing[0] != "userId" {
		t.Errorf("missing = %v; want [userId] (required path field must be enforced)", missing)
	}
	missing2, _, _ := validateParams(op, map[string]any{"userId": "me", "q": "x"})
	if len(missing2) != 0 {
		t.Errorf("missing = %v; want none when the path field is provided", missing2)
	}
}

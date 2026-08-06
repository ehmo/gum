package dispatch

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/embedded"
)

// applyFieldDefaults is the gum-3gcv fix: before it, nothing in the codebase
// read catalog.RequestField.Default, so `gum describe` advertised defaults the
// dispatcher never sent. These tests pin the injection rules and the two
// properties that make the injection safe — a caller-supplied value always wins,
// and body fields land under the reserved "body" arg.

func TestApplyFieldDefaultsInjectsOmittedArg(t *testing.T) {
	op := &catalog.Op{
		OpID: "test.op",
		RequestFields: []catalog.RequestField{
			{Name: "network", Location: catalog.RequestFieldArg, Type: "string", Default: "GOOGLE_SEARCH"},
			{Name: "maxCpcMicros", Location: catalog.RequestFieldArg, Type: "integer", Default: "1000000"},
			{Name: "adult", Location: catalog.RequestFieldArg, Type: "boolean", Default: "false"},
			{Name: "geo", Location: catalog.RequestFieldArg, Type: "array", ItemType: "string", Default: "geoTargetConstants/2840"},
		},
	}
	args := map[string]any{}
	if w := applyFieldDefaults(op, args); len(w) != 0 {
		t.Fatalf("unexpected warnings: %v", w)
	}
	if got := args["network"]; got != "GOOGLE_SEARCH" {
		t.Errorf("network = %#v; want \"GOOGLE_SEARCH\"", got)
	}
	if got := args["maxCpcMicros"]; got != int64(1000000) {
		t.Errorf("maxCpcMicros = %#v; want int64(1000000)", got)
	}
	if got := args["adult"]; got != false {
		t.Errorf("adult = %#v; want false", got)
	}
	geo, ok := args["geo"].([]any)
	if !ok || len(geo) != 1 || geo[0] != "geoTargetConstants/2840" {
		t.Errorf("geo = %#v; want [\"geoTargetConstants/2840\"]", args["geo"])
	}
}

// TestApplyFieldDefaultsNeverOverwritesCaller is the property that keeps the
// injection safe: a value the caller passed is what gets sent, including an
// explicit zero value and an explicit null.
func TestApplyFieldDefaultsNeverOverwritesCaller(t *testing.T) {
	op := &catalog.Op{
		OpID: "test.op",
		RequestFields: []catalog.RequestField{
			{Name: "network", Location: catalog.RequestFieldArg, Type: "string", Default: "GOOGLE_SEARCH"},
			{Name: "maxCpcMicros", Location: catalog.RequestFieldArg, Type: "integer", Default: "1000000"},
			{Name: "adult", Location: catalog.RequestFieldArg, Type: "boolean", Default: "true"},
			{Name: "explicitNull", Location: catalog.RequestFieldArg, Type: "string", Default: "fallback"},
		},
	}
	args := map[string]any{
		"network":      "GOOGLE_SEARCH_AND_PARTNERS",
		"maxCpcMicros": int64(0), // explicit zero must survive
		"adult":        false,    // explicit false must survive a `true` default
		"explicitNull": nil,
	}
	applyFieldDefaults(op, args)
	if got := args["network"]; got != "GOOGLE_SEARCH_AND_PARTNERS" {
		t.Errorf("network = %#v; caller value overwritten", got)
	}
	if got := args["maxCpcMicros"]; got != int64(0) {
		t.Errorf("maxCpcMicros = %#v; explicit zero overwritten", got)
	}
	if got := args["adult"]; got != false {
		t.Errorf("adult = %#v; explicit false overwritten", got)
	}
	if got, ok := args["explicitNull"]; !ok || got != nil {
		t.Errorf("explicitNull = %#v (present=%v); explicit null overwritten", got, ok)
	}
}

// TestApplyFieldDefaultsRoutesBodyFields checks that a body-location default
// lands inside args["body"], where validateParams admits it and the executors
// serialize it. A top-level injection would be rejected as an unknown arg.
func TestApplyFieldDefaultsRoutesBodyFields(t *testing.T) {
	op := &catalog.Op{
		OpID: "searchconsole.searchanalytics.query",
		RequestFields: []catalog.RequestField{
			{Name: "rowLimit", Location: catalog.RequestFieldBody, Type: "integer", Default: "1000"},
			{Name: "startRow", Location: catalog.RequestFieldBody, Type: "integer", Default: "0"},
		},
	}
	args := map[string]any{"body": map[string]any{"startDate": "2026-01-01"}}
	applyFieldDefaults(op, args)

	if _, leaked := args["rowLimit"]; leaked {
		t.Error("rowLimit injected at the top level; body fields must go under args[\"body\"]")
	}
	body, ok := args["body"].(map[string]any)
	if !ok {
		t.Fatalf("body = %#v; want map", args["body"])
	}
	if got := body["rowLimit"]; got != int64(1000) {
		t.Errorf("body.rowLimit = %#v; want int64(1000)", got)
	}
	if got := body["startRow"]; got != int64(0) {
		t.Errorf("body.startRow = %#v; want int64(0)", got)
	}
	if got := body["startDate"]; got != "2026-01-01" {
		t.Errorf("body.startDate = %#v; caller field lost", got)
	}

	// The injected args must survive validation, not become "unknown".
	if _, unknown, _ := validateParams(op, args); len(unknown) != 0 {
		t.Errorf("validateParams rejected the injected defaults as unknown: %v", unknown)
	}
}

func TestApplyFieldDefaultsCreatesBodyWhenAbsent(t *testing.T) {
	op := &catalog.Op{
		OpID: "test.op",
		RequestFields: []catalog.RequestField{
			{Name: "rowLimit", Location: catalog.RequestFieldBody, Type: "integer", Default: "1000"},
		},
	}
	args := map[string]any{}
	applyFieldDefaults(op, args)
	body, ok := args["body"].(map[string]any)
	if !ok {
		t.Fatalf("body = %#v; want a created map", args["body"])
	}
	if got := body["rowLimit"]; got != int64(1000) {
		t.Errorf("body.rowLimit = %#v; want int64(1000)", got)
	}
}

// TestApplyFieldDefaultsExplicitBodyFieldWins mirrors assembleRequestBody: a
// body field the caller wrote by hand is never replaced.
func TestApplyFieldDefaultsExplicitBodyFieldWins(t *testing.T) {
	op := &catalog.Op{
		OpID: "test.op",
		RequestFields: []catalog.RequestField{
			{Name: "rowLimit", Location: catalog.RequestFieldBody, Type: "integer", Default: "1000"},
		},
	}
	args := map[string]any{"body": map[string]any{"rowLimit": int64(5)}}
	applyFieldDefaults(op, args)
	body := args["body"].(map[string]any)
	if got := body["rowLimit"]; got != int64(5) {
		t.Errorf("body.rowLimit = %#v; caller value overwritten", got)
	}
}

// TestApplyFieldDefaultsLeavesNonObjectBodyAlone pins the same guard
// assembleRequestBody uses: a caller who passed a non-object body (a raw JSON
// array or a pre-serialized string) knows what they are doing.
func TestApplyFieldDefaultsLeavesNonObjectBodyAlone(t *testing.T) {
	op := &catalog.Op{
		OpID: "test.op",
		RequestFields: []catalog.RequestField{
			{Name: "rowLimit", Location: catalog.RequestFieldBody, Type: "integer", Default: "1000"},
		},
	}
	args := map[string]any{"body": `{"rowLimit":5}`}
	applyFieldDefaults(op, args)
	if got := args["body"]; got != `{"rowLimit":5}` {
		t.Errorf("body = %#v; a non-object body must pass through untouched", got)
	}
}

// TestApplyFieldDefaultsSkipsRequiredFields keeps a missing required arg a clean
// INVALID_ARGS instead of a silent placeholder on the wire.
func TestApplyFieldDefaultsSkipsRequiredFields(t *testing.T) {
	op := &catalog.Op{
		OpID: "test.op",
		RequestFields: []catalog.RequestField{
			{Name: "customerId", Location: catalog.RequestFieldPath, Type: "string", Required: true, Default: "1234567890"},
		},
	}
	args := map[string]any{}
	applyFieldDefaults(op, args)
	if v, ok := args["customerId"]; ok {
		t.Errorf("customerId = %#v; a required field's default must not be injected", v)
	}
	missing, _, _ := validateParams(op, args)
	if len(missing) != 1 || missing[0] != "customerId" {
		t.Errorf("missing = %v; want [customerId]", missing)
	}
}

// TestApplyFieldDefaultsWarnsOnUndecodableDefault covers a plugin-supplied
// catalog op, which no build-time invariant test gates. The call proceeds
// without the field and the caller is told why.
func TestApplyFieldDefaultsWarnsOnUndecodableDefault(t *testing.T) {
	op := &catalog.Op{
		OpID: "plugin.op",
		RequestFields: []catalog.RequestField{
			{Name: "limit", Location: catalog.RequestFieldArg, Type: "integer", Default: "one thousand"},
			{Name: "rows", Location: catalog.RequestFieldBody, Type: "integer", Default: "lots"},
		},
	}
	args := map[string]any{}
	warnings := applyFieldDefaults(op, args)
	if len(warnings) != 2 {
		t.Fatalf("warnings = %v; want one per undecodable default", warnings)
	}
	if _, ok := args["limit"]; ok {
		t.Errorf("limit = %#v; an undecodable default must not be injected", args["limit"])
	}
	if body, ok := args["body"].(map[string]any); ok {
		if _, present := body["rows"]; present {
			t.Errorf("body.rows = %#v; an undecodable default must not be injected", body["rows"])
		}
	}
	for _, w := range warnings {
		if !strings.Contains(w, "plugin.op") {
			t.Errorf("warning %q does not name the op", w)
		}
	}
}

func TestApplyFieldDefaultsNoDefaultsIsNoOp(t *testing.T) {
	op := &catalog.Op{
		OpID: "test.op",
		RequestFields: []catalog.RequestField{
			{Name: "q", Location: catalog.RequestFieldQuery, Type: "string"},
			{Name: "startDate", Location: catalog.RequestFieldBody, Type: "string"},
		},
	}
	args := map[string]any{"q": "hi"}
	applyFieldDefaults(op, args)
	if len(args) != 1 {
		t.Errorf("args = %#v; want only the caller's q (no body created)", args)
	}
}

// TestParseAndValidateAppliesDefaultsBeforeHashing is the end-to-end property
// that makes the fix observable: the args the dispatcher validates, hashes, and
// hands to the adapter are the args that go on the wire. An omitted arg and an
// explicitly-passed default must therefore produce the same ArgsHash, so they
// share a cache entry and read the same in the audit log.
func TestParseAndValidateAppliesDefaultsBeforeHashing(t *testing.T) {
	op := catalog.Op{
		OpID:             "test.defaults",
		OpSchemaVersion:  1,
		Title:            "Test",
		Summary:          "Test",
		DefaultVariantID: "test.v1",
		Variants:         []catalog.Variant{{VariantID: "test.v1", VariantSchemaVersion: 1}},
		RequestFields: []catalog.RequestField{
			{Name: "q", Location: catalog.RequestFieldQuery, Type: "string", Required: true},
			{Name: "network", Location: catalog.RequestFieldArg, Type: "string", Default: "GOOGLE_SEARCH"},
		},
	}
	d := &dispatcher{snapshot: &catalog.Catalog{Ops: []catalog.Op{op}}}

	omitted := &Invocation{OpID: "test.defaults", Args: map[string]any{"q": "hi"}}
	got, serr := d.parseAndValidate(context.Background(), omitted)
	if serr != nil {
		t.Fatalf("parseAndValidate: %v", serr)
	}
	if v := omitted.Args["network"]; v != "GOOGLE_SEARCH" {
		t.Fatalf("network = %#v; the default was not injected into the invocation args", v)
	}

	explicit := &Invocation{OpID: "test.defaults", Args: map[string]any{"q": "hi", "network": "GOOGLE_SEARCH"}}
	want, serr := d.parseAndValidate(context.Background(), explicit)
	if serr != nil {
		t.Fatalf("parseAndValidate (explicit): %v", serr)
	}
	if got.ArgsHash != want.ArgsHash {
		t.Errorf("ArgsHash differs between omitted (%s) and explicit (%s) default; "+
			"the cache key and audit record must not depend on whether the caller typed the default",
			got.ArgsHash, want.ArgsHash)
	}
}

// TestParseAndValidateShippedGoogleAdsDefaults is the gum-3gcv repro against the
// catalog gum actually ships. Calling generateKeywordHistoricalMetrics with only
// the required args must leave geoTargetConstants and language unset — omission
// means all locations and all languages, which is what the field descriptions
// promise — while keywordPlanNetwork, whose declared default matches the Ads
// API's own behaviour, is now filled in for real.
func TestParseAndValidateShippedGoogleAdsDefaults(t *testing.T) {
	var cat catalog.Catalog
	if err := json.Unmarshal(embedded.CatalogJSON, &cat); err != nil {
		t.Fatalf("unmarshal embedded catalog: %v", err)
	}
	d := &dispatcher{snapshot: &cat}
	inv := &Invocation{
		OpID: "googleads.keywordPlanIdeas.generateKeywordHistoricalMetrics",
		Args: map[string]any{
			"customerId": "1234567890",
			"keywords":   []any{"lead in protein powder"},
		},
	}
	if _, serr := d.parseAndValidate(context.Background(), inv); serr != nil {
		t.Fatalf("parseAndValidate: %v", serr)
	}
	for _, name := range []string{"geoTargetConstants", "language"} {
		if v, ok := inv.Args[name]; ok {
			t.Errorf("%s = %#v; omitting it must query all locations/languages, not a US/English default", name, v)
		}
	}
	if got := inv.Args["keywordPlanNetwork"]; got != "GOOGLE_SEARCH" {
		t.Errorf("keywordPlanNetwork = %#v; the declared default must now be sent", got)
	}
}

// TestParseAndValidateDefaultsAreJSONSerializable guards the executors: every
// injected default must marshal, because typedrestsdk serializes args["body"]
// and encodes the rest into the query string.
func TestParseAndValidateDefaultsAreJSONSerializable(t *testing.T) {
	op := catalog.Op{
		OpID:             "test.defaults",
		OpSchemaVersion:  1,
		Title:            "Test",
		Summary:          "Test",
		DefaultVariantID: "test.v1",
		Variants:         []catalog.Variant{{VariantID: "test.v1", VariantSchemaVersion: 1}},
		RequestFields: []catalog.RequestField{
			{Name: "geo", Location: catalog.RequestFieldArg, Type: "array", ItemType: "string", Default: "geoTargetConstants/2840"},
			{Name: "opts", Location: catalog.RequestFieldArg, Type: "object", Default: `{"k":1}`},
			{Name: "rowLimit", Location: catalog.RequestFieldBody, Type: "integer", Default: "1000"},
		},
	}
	d := &dispatcher{snapshot: &catalog.Catalog{Ops: []catalog.Op{op}}}
	inv := &Invocation{OpID: "test.defaults", Args: map[string]any{}}
	if _, serr := d.parseAndValidate(context.Background(), inv); serr != nil {
		t.Fatalf("parseAndValidate: %v", serr)
	}
	if _, err := json.Marshal(inv.Args); err != nil {
		t.Fatalf("injected defaults do not marshal: %v", err)
	}
}

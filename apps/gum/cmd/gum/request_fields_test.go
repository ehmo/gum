package main

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/catalog"
)

// scFields is a synthetic RequestField set modeling searchconsole.searchanalytics.query:
// siteUrl as a path param; startDate/rowLimit/dimensions as body fields.
func scFields() []catalog.RequestField {
	return []catalog.RequestField{
		{Name: "siteUrl", Location: catalog.RequestFieldPath, Type: "string", Required: true},
		{Name: "startDate", Location: catalog.RequestFieldBody, Type: "string", Format: "date"},
		{Name: "endDate", Location: catalog.RequestFieldBody, Type: "string", Format: "date"},
		{Name: "rowLimit", Location: catalog.RequestFieldBody, Type: "integer"},
		{Name: "dimensions", Location: catalog.RequestFieldBody, Type: "array", ItemType: "string"},
	}
}

// TestAssembleRequestBodyRoutesAndCoerces pins the core: flat body fields move
// into the "body" map with correct types, path fields stay top-level, and
// repeated array values become a typed slice.
func TestAssembleRequestBodyRoutesAndCoerces(t *testing.T) {
	args := map[string]any{
		"siteUrl":    "sc-domain:turek.co",   // path → stays top-level
		"startDate":  "2026-04-28",           // body string
		"endDate":    "2026-05-28",           // body string
		"rowLimit":   "10",                   // body integer (string → int64)
		"dimensions": []any{"query", "page"}, // body array (from repeated key)
		"alt":        "json",                 // not a field → stays top-level (query)
	}
	got := assembleRequestBody(args, scFields())

	if got["siteUrl"] != "sc-domain:turek.co" {
		t.Errorf("siteUrl should stay top-level, got %v", got["siteUrl"])
	}
	if got["alt"] != "json" {
		t.Errorf("unknown key alt should stay top-level, got %v", got["alt"])
	}
	for _, k := range []string{"startDate", "endDate", "rowLimit", "dimensions"} {
		if _, present := got[k]; present {
			t.Errorf("body field %q should have moved out of top-level args", k)
		}
	}
	body, ok := got[bodyArgKey].(map[string]any)
	if !ok {
		t.Fatalf("body not assembled: %#v", got[bodyArgKey])
	}
	if body["startDate"] != "2026-04-28" || body["endDate"] != "2026-05-28" {
		t.Errorf("body dates wrong: %#v", body)
	}
	if body["rowLimit"] != int64(10) {
		t.Errorf("rowLimit = %#v, want int64(10)", body["rowLimit"])
	}
	if !reflect.DeepEqual(body["dimensions"], []any{"query", "page"}) {
		t.Errorf("dimensions = %#v, want [query page]", body["dimensions"])
	}
}

// TestAssembleRequestBodyNoFieldsIsNoop pins backward compatibility: with no
// RequestFields (every op today), args are returned untouched — body:=json and
// the §12.0 grammar keep working.
func TestAssembleRequestBodyNoFieldsIsNoop(t *testing.T) {
	args := map[string]any{"siteUrl": "x", "body": map[string]any{"startDate": "2026-04-28"}}
	got := assembleRequestBody(args, nil)
	if !reflect.DeepEqual(got, args) {
		t.Errorf("no-fields should be a no-op, got %#v", got)
	}
}

// TestAssembleRequestBodyExplicitBodyWins pins that an explicit body:=json field
// is preserved and a flat field of the same name does not clobber it.
func TestAssembleRequestBodyExplicitBodyWins(t *testing.T) {
	args := map[string]any{
		"siteUrl":   "x",
		"startDate": "2026-04-28",                              // flat
		bodyArgKey:  map[string]any{"startDate": "2000-01-01"}, // explicit body wins
	}
	got := assembleRequestBody(args, scFields())
	body := got[bodyArgKey].(map[string]any)
	if body["startDate"] != "2000-01-01" {
		t.Errorf("explicit body should win, got startDate=%v", body["startDate"])
	}
	if _, present := got["startDate"]; present {
		t.Error("flat startDate should be consumed even when explicit body wins")
	}
}

// TestArrayRequestFields pins extraction of array-typed field names for the parser.
func TestArrayRequestFields(t *testing.T) {
	got := arrayRequestFields(scFields())
	if !reflect.DeepEqual(got, []string{"dimensions"}) {
		t.Errorf("arrayRequestFields = %v, want [dimensions]", got)
	}
}

// TestRenderSkeleton pins that the skeleton groups fields by location and shows
// required/enum/default hints in copy-pasteable key=value form.
func TestRenderSkeleton(t *testing.T) {
	fields := []catalog.RequestField{
		{Name: "siteUrl", Location: catalog.RequestFieldPath, Type: "string", Required: true},
		{Name: "dimensions", Location: catalog.RequestFieldBody, Type: "array", ItemType: "string", Enum: []string{"date", "query", "page"}},
		{Name: "rowLimit", Location: catalog.RequestFieldBody, Type: "integer", Default: "1000"},
	}
	var b bytes.Buffer
	if err := renderSkeleton(&b, "searchconsole.searchanalytics.query", fields); err != nil {
		t.Fatalf("renderSkeleton: %v", err)
	}
	out := b.String()
	for _, want := range []string{
		"path parameters", "siteUrl=<string>", "required",
		"body fields", "dimensions=<string>", "repeatable", "choices: date|query|page",
		"rowLimit=<integer>", "default: 1000",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("skeleton missing %q in:\n%s", want, out)
		}
	}
}

// TestValidateEnumArgs pins case-insensitive enum acceptance and rejection.
func TestValidateEnumArgs(t *testing.T) {
	fields := scFields()
	fields = append(fields, catalog.RequestField{Name: "dimensions", Location: catalog.RequestFieldBody, Type: "array", ItemType: "string", Enum: []string{"date", "query", "page"}})

	// Valid (mixed case) passes.
	if err := validateEnumArgs(map[string]any{"dimensions": []any{"QUERY", "page"}}, fields); err != nil {
		t.Errorf("valid enum values rejected: %v", err)
	}
	// Invalid is rejected with the choices.
	err := validateEnumArgs(map[string]any{"dimensions": []any{"bogus"}}, fields)
	if err == nil || !strings.Contains(err.Error(), "date|query|page") {
		t.Errorf("expected enum rejection listing choices, got %v", err)
	}
}

// TestValidateEnumArgsTypedScalarRejected is the audit regression: a typed
// scalar (e.g. from `field:=5`) supplied to a string enum field must be
// validated, not silently skipped. flattenToStrings returns nil for a bare
// scalar, so the default arm previously did zero iterations and let it through.
func TestValidateEnumArgsTypedScalarRejected(t *testing.T) {
	fields := []catalog.RequestField{
		{Name: "mode", Location: catalog.RequestFieldQuery, Type: "string", Enum: []string{"auto", "manual"}},
	}
	// A number where a string enum is expected → local rejection with choices.
	err := validateEnumArgs(map[string]any{"mode": float64(5)}, fields)
	if err == nil || !strings.Contains(err.Error(), "auto|manual") {
		t.Errorf("typed scalar 5 against enum should be rejected with choices; got %v", err)
	}
	// A bool likewise.
	if err := validateEnumArgs(map[string]any{"mode": true}, fields); err == nil {
		t.Error("bool against a string enum should be rejected; got nil")
	}
	// A valid value still passes (numeric-string enum match).
	numFields := []catalog.RequestField{
		{Name: "n", Location: catalog.RequestFieldQuery, Type: "string", Enum: []string{"1", "5", "10"}},
	}
	if err := validateEnumArgs(map[string]any{"n": float64(5)}, numFields); err != nil {
		t.Errorf("numeric 5 matching enum [1 5 10] should pass; got %v", err)
	}
}

// TestValidateEnumArgsBodyJSONScalar pins gum-dws4: a string enum field nested
// inside an explicit body:=json object is validated and normalized to canonical
// case, just like a flat field.
func TestValidateEnumArgsBodyJSONScalar(t *testing.T) {
	fields := []catalog.RequestField{
		{Name: "aggregationType", Location: catalog.RequestFieldBody, Type: "string",
			Enum: []string{"AUTO", "MANUAL"}},
	}
	args := map[string]any{
		bodyArgKey: map[string]any{"aggregationType": "auto"},
	}
	if err := validateEnumArgs(args, fields); err != nil {
		t.Fatalf("valid body-JSON enum rejected: %v", err)
	}
	body := args[bodyArgKey].(map[string]any)
	if body["aggregationType"] != "AUTO" {
		t.Errorf("aggregationType not normalized: got %v, want AUTO", body["aggregationType"])
	}
}

// TestValidateEnumArgsBodyJSONArray pins that a []any enum field inside
// body:=json is validated and normalized element-wise.
func TestValidateEnumArgsBodyJSONArray(t *testing.T) {
	fields := []catalog.RequestField{
		{Name: "dimensions", Location: catalog.RequestFieldBody, Type: "array",
			ItemType: "string", Enum: []string{"date", "query", "page"}},
	}
	args := map[string]any{
		bodyArgKey: map[string]any{"dimensions": []any{"QUERY", "Page"}},
	}
	if err := validateEnumArgs(args, fields); err != nil {
		t.Fatalf("valid body-JSON array enum rejected: %v", err)
	}
	body := args[bodyArgKey].(map[string]any)
	if !reflect.DeepEqual(body["dimensions"], []any{"query", "page"}) {
		t.Errorf("dimensions not normalized: got %#v, want [query page]", body["dimensions"])
	}
}

// TestValidateEnumArgsBodyJSONInvalid pins that an invalid enum value inside
// body:=json produces a friendly CLI_ARG_INVALID error listing the valid
// choices, instead of silently forwarding to the upstream API for a 400.
func TestValidateEnumArgsBodyJSONInvalid(t *testing.T) {
	fields := []catalog.RequestField{
		{Name: "dimensions", Location: catalog.RequestFieldBody, Type: "array",
			ItemType: "string", Enum: []string{"date", "query", "page"}},
	}
	args := map[string]any{
		bodyArgKey: map[string]any{"dimensions": []any{"bogus"}},
	}
	err := validateEnumArgs(args, fields)
	if err == nil {
		t.Fatal("expected error for invalid body-JSON enum value, got nil")
	}
	if !strings.Contains(err.Error(), "date|query|page") || !strings.Contains(err.Error(), "bogus") {
		t.Errorf("error should name the bad value and list choices, got: %v", err)
	}
}

// TestValidateEnumArgsNoBodySideEffect pins that a flat enum field is processed
// without the body path firing or a body key being introduced.
func TestValidateEnumArgsNoBodySideEffect(t *testing.T) {
	fields := []catalog.RequestField{
		{Name: "aggregationType", Location: catalog.RequestFieldBody, Type: "string",
			Enum: []string{"AUTO", "MANUAL"}},
	}
	args := map[string]any{"aggregationType": "auto"}
	if err := validateEnumArgs(args, fields); err != nil {
		t.Fatalf("valid flat enum rejected: %v", err)
	}
	if args["aggregationType"] != "AUTO" {
		t.Errorf("aggregationType not normalized: got %v, want AUTO", args["aggregationType"])
	}
	if _, ok := args[bodyArgKey]; ok {
		t.Error("validateEnumArgs must not create a body key when none existed")
	}
}

// TestPromptMissingFields pins the wizard: required-and-missing fields are read
// from the reader (array split on commas), supplied fields are not re-prompted,
// and an empty answer for a required field errors.
func TestPromptMissingFields(t *testing.T) {
	fields := []catalog.RequestField{
		{Name: "siteUrl", Location: catalog.RequestFieldPath, Type: "string", Required: true},
		{Name: "startDate", Location: catalog.RequestFieldBody, Type: "string", Required: true},
		{Name: "dimensions", Location: catalog.RequestFieldBody, Type: "array", Required: true},
		{Name: "rowLimit", Location: catalog.RequestFieldBody, Type: "integer"}, // optional → not prompted
	}
	// siteUrl already supplied; wizard prompts startDate then dimensions.
	args := map[string]any{"siteUrl": "sc-domain:turek.co"}
	in := strings.NewReader("2026-04-28\nquery, page\n")
	var errBuf bytes.Buffer
	if err := promptMissingFields(in, &errBuf, args, fields); err != nil {
		t.Fatalf("promptMissingFields: %v", err)
	}
	if args["startDate"] != "2026-04-28" {
		t.Errorf("startDate = %v, want 2026-04-28", args["startDate"])
	}
	if !reflect.DeepEqual(args["dimensions"], []any{"query", "page"}) {
		t.Errorf("dimensions = %#v, want [query page]", args["dimensions"])
	}
	if _, prompted := args["rowLimit"]; prompted {
		t.Error("optional rowLimit should not be prompted")
	}

	// Empty answer for a required field is an error.
	err := promptMissingFields(strings.NewReader("\n"), &bytes.Buffer{},
		map[string]any{}, []catalog.RequestField{{Name: "siteUrl", Type: "string", Required: true}})
	if err == nil {
		t.Error("empty required answer should error")
	}
}

// TestValidateFieldTypes pins local rejection of unparseable typed scalars
// (better than a confusing upstream 400), while trusting already-typed values.
func TestValidateFieldTypes(t *testing.T) {
	fields := []catalog.RequestField{
		{Name: "rowLimit", Location: catalog.RequestFieldBody, Type: "integer"},
		{Name: "flag", Location: catalog.RequestFieldBody, Type: "boolean"},
	}
	if err := validateFieldTypes(map[string]any{"rowLimit": "abc"}, fields); err == nil || !strings.Contains(err.Error(), "expected an integer") {
		t.Errorf("rowLimit=abc should be rejected, got %v", err)
	}
	if err := validateFieldTypes(map[string]any{"rowLimit": "10"}, fields); err != nil {
		t.Errorf("rowLimit=10 should pass, got %v", err)
	}
	// Already-typed (from key:=json) value is trusted, not re-validated as string.
	if err := validateFieldTypes(map[string]any{"rowLimit": int64(10)}, fields); err != nil {
		t.Errorf("typed rowLimit should pass, got %v", err)
	}
	if err := validateFieldTypes(map[string]any{"flag": "nope"}, fields); err == nil {
		t.Error("flag=nope should be rejected")
	}
}

func TestValidateFieldTypesRejectsIntegerOverflow(t *testing.T) {
	fields := []catalog.RequestField{
		{Name: "rowLimit", Location: catalog.RequestFieldBody, Type: "integer"},
		{Name: "ids", Location: catalog.RequestFieldBody, Type: "array", ItemType: "integer"},
	}

	tooLarge := "9999999999999999999999"
	if err := validateFieldTypes(map[string]any{"rowLimit": tooLarge}, fields); err == nil || !strings.Contains(err.Error(), "expected an integer") {
		t.Errorf("rowLimit overflow should be rejected locally, got %v", err)
	}
	if err := validateFieldTypes(map[string]any{"ids": []any{json.Number(tooLarge)}}, fields); err == nil || !strings.Contains(err.Error(), "expected an integer") {
		t.Errorf("json.Number array overflow should be rejected locally, got %v", err)
	}
}

// TestWizardSkipsBodyJSONFields pins the medium fix: a required field supplied
// inside an explicit body:=json object is not re-prompted by the wizard.
func TestWizardSkipsBodyJSONFields(t *testing.T) {
	fields := []catalog.RequestField{
		{Name: "siteUrl", Location: catalog.RequestFieldPath, Type: "string", Required: true},
		{Name: "startDate", Location: catalog.RequestFieldBody, Type: "string", Required: true},
	}
	args := map[string]any{
		"siteUrl":  "sc-domain:turek.co",                      // flat
		bodyArgKey: map[string]any{"startDate": "2026-04-28"}, // via body:=json
	}
	// Empty reader: if the wizard wrongly prompted, ReadString would yield "" and error.
	if err := promptMissingFields(strings.NewReader(""), &bytes.Buffer{}, args, fields); err != nil {
		t.Errorf("wizard should not prompt for body:=json-supplied field, got %v", err)
	}
}

// TestKebabCase pins camelCase->kebab flag-name conversion.
func TestKebabCase(t *testing.T) {
	cases := map[string]string{
		"siteUrl": "site-url", "startDate": "start-date", "rowLimit": "row-limit",
		"userId": "user-id", "labelIds": "label-ids", "q": "q",
		"dimensionFilterGroups": "dimension-filter-groups",
		// Acronym / consecutive-caps cases (must not become per-letter hyphens).
		"URLPath": "url-path", "ID": "id", "htmlContent": "html-content",
		"adUnitId": "ad-unit-id",
	}
	for in, want := range cases {
		if got := kebabCase(in); got != want {
			t.Errorf("kebabCase(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestRegisterAndApplyKebabFlags pins the two-pass --kebab flag surface: for a
// `gum call <op>` invocation the op's fields register as typed --flags, and
// applyKebabFlags merges set values back under the canonical (camelCase) names.
func TestRegisterAndApplyKebabFlags(t *testing.T) {
	root := newRootCmd()
	const opID = "searchconsole.searchanalytics.query"
	registerDynamicCallFlags(root, []string{"call", opID})

	callCmd, _, err := root.Find([]string{"call"})
	if err != nil || callCmd == nil {
		t.Fatalf("find call: %v", err)
	}
	for _, kebab := range []string{"site-url", "start-date", "end-date", "dimensions", "row-limit"} {
		if callCmd.Flags().Lookup(kebab) == nil {
			t.Errorf("flag --%s was not registered for %s", kebab, opID)
		}
	}

	// Simulate parsed flags.
	_ = callCmd.Flags().Set("site-url", "sc-domain:turek.co")
	_ = callCmd.Flags().Set("start-date", "2026-05-01")
	_ = callCmd.Flags().Set("dimensions", "query")
	_ = callCmd.Flags().Set("dimensions", "page") // StringArray appends
	_ = callCmd.Flags().Set("row-limit", "10")

	args := map[string]any{}
	applyKebabFlags(callCmd, args, lookupRequestFields(opID))

	if args["siteUrl"] != "sc-domain:turek.co" {
		t.Errorf("siteUrl = %v", args["siteUrl"])
	}
	if args["startDate"] != "2026-05-01" {
		t.Errorf("startDate = %v", args["startDate"])
	}
	if args["rowLimit"] != "10" { // string here; coerced to int downstream
		t.Errorf("rowLimit = %v, want \"10\"", args["rowLimit"])
	}
	if !reflect.DeepEqual(args["dimensions"], []any{"query", "page"}) {
		t.Errorf("dimensions = %#v, want [query page]", args["dimensions"])
	}
	// A field whose flag was not set is absent.
	if _, ok := args["aggregationType"]; ok {
		t.Error("unset flag aggregationType should not appear in args")
	}
}

// TestSkeletonRendersForEveryOp pins that --skeleton renders without error for
// EVERY catalog op (a CLI-layer per-op check on top of the catalog invariants).
func TestSkeletonRendersForEveryOp(t *testing.T) {
	snap := loadCatalog()
	if snap == nil || len(snap.Ops) == 0 {
		t.Fatal("catalog empty")
	}
	for i := range snap.Ops {
		op := &snap.Ops[i]
		t.Run(op.OpID, func(t *testing.T) {
			var b bytes.Buffer
			if err := renderSkeleton(&b, op.OpID, op.RequestFields); err != nil {
				t.Errorf("renderSkeleton(%s): %v", op.OpID, err)
			}
			if b.Len() == 0 {
				t.Errorf("renderSkeleton(%s): empty output", op.OpID)
			}
		})
	}

	// Content assertion (not just non-error): a known op's skeleton must show
	// its fields, a path param, an enum, and the required marker.
	var b bytes.Buffer
	if err := renderSkeleton(&b, "searchconsole.searchanalytics.query", lookupRequestFields("searchconsole.searchanalytics.query")); err != nil {
		t.Fatalf("renderSkeleton: %v", err)
	}
	out := b.String()
	for _, want := range []string{"siteUrl=<string>", "required", "startDate=<string>", "dimensions=<string>", "choices: date|query|page"} {
		if !strings.Contains(out, want) {
			t.Errorf("searchanalytics.query skeleton missing %q in:\n%s", want, out)
		}
	}
}

// TestMetaToolFormat pins meta-tool (read/write/destructive) format resolution:
// --output wins; legacy --format is the fallback; empty stays empty (kernel
// default); an unknown --output errors.
func TestMetaToolFormat(t *testing.T) {
	if got, _ := metaToolFormat("table", ""); got != "table" {
		t.Errorf("--output table = %q", got)
	}
	if got, _ := metaToolFormat("", "toon"); got != "toon" {
		t.Errorf("--format toon fallback = %q", got)
	}
	if got, _ := metaToolFormat("", ""); got != "" {
		t.Errorf("empty = %q, want empty", got)
	}
	if _, err := metaToolFormat("yaml", ""); err == nil {
		t.Error("--output yaml should error")
	}
}

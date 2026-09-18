package dispatch

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/catalog"
)

// gum-puum: an ArgDefaulter fills args the caller omitted from sources outside
// the catalog (environment, profile config). These tests pin the kernel side of
// the contract: caller values always win, defaults run before validation and
// hashing, and a still-missing required arg carries the defaulter's hint.

type stubArgDefaulter struct {
	defaults map[string]any
	err      error
	hint     string
	sawArgs  map[string]any
}

func (s *stubArgDefaulter) ArgDefaults(_ *catalog.Op, args map[string]any) (map[string]any, error) {
	s.sawArgs = make(map[string]any, len(args))
	for k, v := range args {
		s.sawArgs[k] = v
	}
	return s.defaults, s.err
}

func (s *stubArgDefaulter) MissingArgHint(_ *catalog.Op, missing []string) string {
	if s.hint == "" {
		return ""
	}
	return s.hint + " [" + strings.Join(missing, ",") + "]"
}

func accountOpDispatcher(defaulter ArgDefaulter) *dispatcher {
	op := catalog.Op{
		OpID:             "test.account",
		OpSchemaVersion:  1,
		Title:            "Test",
		Summary:          "Test",
		DefaultVariantID: "test.v1",
		Variants:         []catalog.Variant{{VariantID: "test.v1", VariantSchemaVersion: 1}},
		RequestFields: []catalog.RequestField{
			{Name: "customerId", Location: catalog.RequestFieldPath, Type: "string", Required: true},
			{Name: "loginCustomerId", Location: catalog.RequestFieldArg, Type: "string"},
			{Name: "network", Location: catalog.RequestFieldArg, Type: "string", Default: "GOOGLE_SEARCH"},
		},
	}
	return &dispatcher{
		snapshot:     &catalog.Catalog{Ops: []catalog.Op{op}},
		argDefaulter: defaulter,
	}
}

func TestParseAndValidateAppliesArgDefaulter(t *testing.T) {
	d := accountOpDispatcher(&stubArgDefaulter{defaults: map[string]any{
		"customerId":      "1234567890",
		"loginCustomerId": "1111111111",
	}})

	omitted := &Invocation{OpID: "test.account", Args: map[string]any{}}
	got, serr := d.parseAndValidate(context.Background(), omitted)
	if serr != nil {
		t.Fatalf("parseAndValidate: %v", serr)
	}
	if v := omitted.Args["customerId"]; v != "1234567890" {
		t.Errorf("customerId = %#v; want the configured default", v)
	}
	if v := omitted.Args["loginCustomerId"]; v != "1111111111" {
		t.Errorf("loginCustomerId = %#v; want the configured default", v)
	}

	explicit := &Invocation{OpID: "test.account", Args: map[string]any{
		"customerId":      "1234567890",
		"loginCustomerId": "1111111111",
	}}
	want, serr := d.parseAndValidate(context.Background(), explicit)
	if serr != nil {
		t.Fatalf("parseAndValidate (explicit): %v", serr)
	}
	if got.ArgsHash != want.ArgsHash {
		t.Errorf("ArgsHash differs between defaulted (%s) and explicit (%s) args; "+
			"the cache key and audit record must see the args that go on the wire",
			got.ArgsHash, want.ArgsHash)
	}
}

// TestParseAndValidateArgDefaulterCallerWins covers the override rule,
// including an explicit empty string, which is how a caller suppresses a
// configured loginCustomerId for one call.
func TestParseAndValidateArgDefaulterCallerWins(t *testing.T) {
	d := accountOpDispatcher(&stubArgDefaulter{defaults: map[string]any{
		"customerId":      "1234567890",
		"loginCustomerId": "1111111111",
	}})
	inv := &Invocation{OpID: "test.account", Args: map[string]any{
		"customerId":      "9876543210",
		"loginCustomerId": "",
	}}
	if _, serr := d.parseAndValidate(context.Background(), inv); serr != nil {
		t.Fatalf("parseAndValidate: %v", serr)
	}
	if v := inv.Args["customerId"]; v != "9876543210" {
		t.Errorf("customerId = %#v; the explicit arg must override the default", v)
	}
	if v, ok := inv.Args["loginCustomerId"]; !ok || v != "" {
		t.Errorf("loginCustomerId = %#v (present=%v); an explicit empty value must survive", v, ok)
	}
}

// TestParseAndValidateArgDefaulterBeatsCatalogDefault follows the spec
// precedence: env and profile config rank above a built-in default.
func TestParseAndValidateArgDefaulterBeatsCatalogDefault(t *testing.T) {
	d := accountOpDispatcher(&stubArgDefaulter{defaults: map[string]any{
		"customerId": "1234567890",
		"network":    "GOOGLE_SEARCH_AND_PARTNERS",
	}})
	inv := &Invocation{OpID: "test.account", Args: map[string]any{}}
	if _, serr := d.parseAndValidate(context.Background(), inv); serr != nil {
		t.Fatalf("parseAndValidate: %v", serr)
	}
	if v := inv.Args["network"]; v != "GOOGLE_SEARCH_AND_PARTNERS" {
		t.Errorf("network = %#v; a configured default must beat the catalog default", v)
	}
}

func TestParseAndValidateArgDefaulterSeesCallerArgs(t *testing.T) {
	stub := &stubArgDefaulter{}
	d := accountOpDispatcher(stub)
	inv := &Invocation{OpID: "test.account", Args: map[string]any{"customerId": "1234567890"}}
	if _, serr := d.parseAndValidate(context.Background(), inv); serr != nil {
		t.Fatalf("parseAndValidate: %v", serr)
	}
	if _, ok := stub.sawArgs["customerId"]; !ok {
		t.Errorf("defaulter saw args %#v; want the caller's customerId so it can skip validating an unused default", stub.sawArgs)
	}
	if _, ok := stub.sawArgs["network"]; ok {
		t.Error("defaulter saw the catalog default; it must run before catalog defaults are applied")
	}
}

func TestParseAndValidateArgDefaulterError(t *testing.T) {
	d := accountOpDispatcher(&stubArgDefaulter{err: errors.New("GUM_TEST_ID: not a 10-digit id")})
	inv := &Invocation{OpID: "test.account", Args: map[string]any{}}
	_, serr := d.parseAndValidate(context.Background(), inv)
	if serr == nil {
		t.Fatal("parseAndValidate succeeded; a malformed configured default must fail the call")
	}
	if serr.ErrCode != ErrCodeInvalidArgs {
		t.Errorf("ErrCode = %s; want %s", serr.ErrCode, ErrCodeInvalidArgs)
	}
	if !strings.Contains(serr.Message, "GUM_TEST_ID") {
		t.Errorf("Message = %q; want the defaulter's error naming its source", serr.Message)
	}
	for _, key := range []string{"missing", "unknown", "type_errors"} {
		if _, ok := serr.Detail[key].([]string); !ok {
			t.Errorf("Detail[%q] = %#v; INVALID_ARGS must keep its array shape", key, serr.Detail[key])
		}
	}
	if hint, _ := serr.Detail["hint"].(string); hint == "" {
		t.Error("Detail[\"hint\"] is empty; want remediation for the bad default")
	}
}

func TestParseAndValidateMissingArgHint(t *testing.T) {
	d := accountOpDispatcher(&stubArgDefaulter{hint: "set a default"})
	inv := &Invocation{OpID: "test.account", Args: map[string]any{}}
	_, serr := d.parseAndValidate(context.Background(), inv)
	if serr == nil {
		t.Fatal("parseAndValidate succeeded with customerId missing and no default")
	}
	missing, _ := serr.Detail["missing"].([]string)
	if len(missing) != 1 || missing[0] != "customerId" {
		t.Errorf("missing = %#v; want [customerId]", serr.Detail["missing"])
	}
	if hint, _ := serr.Detail["hint"].(string); hint != "set a default [customerId]" {
		t.Errorf("hint = %q; want the defaulter's hint for the missing args", hint)
	}
}

func TestParseAndValidateNoHintWithoutDefaulterHint(t *testing.T) {
	for name, d := range map[string]*dispatcher{
		"nil defaulter":   accountOpDispatcher(nil),
		"empty hint text": accountOpDispatcher(&stubArgDefaulter{}),
	} {
		inv := &Invocation{OpID: "test.account", Args: map[string]any{}}
		_, serr := d.parseAndValidate(context.Background(), inv)
		if serr == nil {
			t.Fatalf("%s: parseAndValidate succeeded with customerId missing", name)
		}
		if _, ok := serr.Detail["hint"]; ok {
			t.Errorf("%s: hint = %#v; want no hint key", name, serr.Detail["hint"])
		}
	}
}

func TestNewDispatcherWithConfigWiresArgDefaults(t *testing.T) {
	stub := &stubArgDefaulter{}
	disp := NewDispatcherWithConfig(&catalog.Catalog{}, nil, DispatcherConfig{ArgDefaults: stub})
	d, ok := disp.(*dispatcher)
	if !ok {
		t.Fatalf("NewDispatcherWithConfig returned %T; want *dispatcher", disp)
	}
	if d.argDefaulter != stub {
		t.Error("DispatcherConfig.ArgDefaults was not wired into the dispatcher")
	}
}

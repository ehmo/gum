package catalog_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/catalog"
)

// TestCapabilityEnumMembership pins the three §5.8 lists and the closed set
// they compose. A stray atom in the wrong list would change what
// CapabilityExecutable answers, which decides whether dispatch refuses a
// variant or sends it upstream.
func TestCapabilityEnumMembership(t *testing.T) {
	wantGeneric := []string{
		"json_request", "json_response", "query_params",
		"path_params", "pagination", "field_mask", "lro_return",
	}
	if len(catalog.GenericCapabilities) != len(wantGeneric) {
		t.Fatalf("GenericCapabilities has %d atoms; want %d", len(catalog.GenericCapabilities), len(wantGeneric))
	}
	for _, atom := range wantGeneric {
		if !catalog.CapabilityExecutable(atom) {
			t.Errorf("CapabilityExecutable(%q)=false; §5.8 lists it as generic-executable", atom)
		}
	}

	if !catalog.CapabilityExecutable(catalog.CapabilityCodeExecution) {
		t.Error("CapabilityExecutable(code_execution)=false; the code.risor typed executor runs it")
	}

	for _, atom := range catalog.UnsupportedCapabilityClasses {
		if catalog.CapabilityExecutable(atom) {
			t.Errorf("CapabilityExecutable(%q)=true; §5.8 lists it as not executable through raw dispatch", atom)
		}
		if !catalog.CapabilityKnown(atom) {
			t.Errorf("CapabilityKnown(%q)=false; it is in the closed enum", atom)
		}
	}

	if got, want := len(catalog.KnownCapabilities), len(catalog.GenericCapabilities)+len(catalog.TypedExecutorCapabilities)+len(catalog.UnsupportedCapabilityClasses); got != want {
		t.Errorf("len(KnownCapabilities)=%d; want %d", got, want)
	}
	for _, atom := range []string{"", "x-", "json-request", "JSON_REQUEST", "media_upload"} {
		if catalog.CapabilityKnown(atom) {
			t.Errorf("CapabilityKnown(%q)=true; it is outside the closed enum", atom)
		}
		if catalog.CapabilityExecutable(atom) {
			t.Errorf("CapabilityExecutable(%q)=true; an unknown atom is never runnable", atom)
		}
	}
}

// TestOpValidateRejectsUnknownCapability pins the §5.8 UNKNOWN_CAPABILITY
// rule. Before this gate an atom outside the enum passed catalog validation
// and reached gum.describe_op, telling the caller about a capability class no
// executor implements.
func TestOpValidateRejectsUnknownCapability(t *testing.T) {
	cases := []struct {
		name    string
		atoms   []string
		support catalog.ExecutionSupport
		unsup   []string
		wantErr error
	}{
		{
			name:  "enum atom accepted",
			atoms: []string{"json_response", "query_params"},
		},
		{
			name:    "unsupported class accepted when declared unsupported",
			atoms:   []string{"json_response", "media_download"},
			support: catalog.ExecutionSupportPartial,
			unsup:   []string{"media_download"},
		},
		{
			name:    "typed-executor atom accepted",
			atoms:   []string{"code_execution"},
			wantErr: nil,
		},
		{
			name:    "experimental atom accepted on schema_only",
			atoms:   []string{"x-sovereign-endpoint"},
			support: catalog.ExecutionSupportSchemaOnly,
			unsup:   []string{"x-sovereign-endpoint"},
		},
		{
			name:    "experimental atom rejected off schema_only",
			atoms:   []string{"x-sovereign-endpoint"},
			wantErr: catalog.ErrExperimentalCapabilityNotSchemaOnly,
		},
		{
			name:    "experimental atom rejected on partial",
			atoms:   []string{"json_response", "x-streaming"},
			support: catalog.ExecutionSupportPartial,
			unsup:   []string{"x-streaming"},
			wantErr: catalog.ErrExperimentalCapabilityNotSchemaOnly,
		},
		{
			name:    "bare x- prefix rejected as unknown",
			atoms:   []string{"x-"},
			support: catalog.ExecutionSupportSchemaOnly,
			unsup:   []string{"x-"},
			wantErr: catalog.ErrUnknownCapability,
		},
		{
			name:    "empty atom rejected",
			atoms:   []string{""},
			wantErr: catalog.ErrUnknownCapability,
		},
		{
			name:    "misspelled atom rejected",
			atoms:   []string{"json_respones"},
			wantErr: catalog.ErrUnknownCapability,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := loadFixture(t, "sample-catalog.json")
			v := &c.Ops[0].Variants[0]
			v.Capabilities = tc.atoms
			v.ExecutionSupport = tc.support
			v.UnsupportedCapabilities = tc.unsup

			err := c.Validate()
			if tc.wantErr == nil {
				if err != nil {
					t.Fatalf("Validate(%v)=%v; want nil", tc.atoms, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("Validate(%v)=nil; want %v", tc.atoms, tc.wantErr)
			}
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("Validate(%v)=%v; want %v", tc.atoms, err, tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.atoms[len(tc.atoms)-1]) && tc.atoms[len(tc.atoms)-1] != "" {
				t.Errorf("error %q does not name the offending atom %q", err, tc.atoms[len(tc.atoms)-1])
			}
		})
	}
}

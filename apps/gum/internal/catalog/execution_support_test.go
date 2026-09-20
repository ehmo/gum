package catalog_test

import (
	"errors"
	"testing"

	"github.com/ehmo/gum/internal/catalog"
)

// TestOpValidateExecutionSupportBinding pins the spec §925 binding between a
// variant's `execution_support` and its `unsupported_capabilities`. Before
// gum-j6xl the catalog ABI declared no `unsupported_capabilities` field at
// all, and `gum.describe_op` substituted the whole `capabilities[]` list for
// it. The field is now the declaration site, so the catalog has to check it:
// an unvalidated variant becomes a wrong DescribeOpResult one hop later.
func TestOpValidateExecutionSupportBinding(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*catalog.Variant)
		want   error
	}{
		{
			name:   "unknown value",
			mutate: func(v *catalog.Variant) { v.ExecutionSupport = catalog.ExecutionSupport("mostly") },
			want:   catalog.ErrUnknownExecutionSupport,
		},
		{
			name: "full carries a list",
			mutate: func(v *catalog.Variant) {
				v.ExecutionSupport = catalog.ExecutionSupportFull
				v.UnsupportedCapabilities = []string{"pagination"}
			},
			want: catalog.ErrUnexpectedUnsupportedCapabilities,
		},
		{
			name: "omitted resolves to full and still forbids a list",
			mutate: func(v *catalog.Variant) {
				v.ExecutionSupport = ""
				v.UnsupportedCapabilities = []string{"pagination"}
			},
			want: catalog.ErrUnexpectedUnsupportedCapabilities,
		},
		{
			name: "partial without a list",
			mutate: func(v *catalog.Variant) {
				v.ExecutionSupport = catalog.ExecutionSupportPartial
			},
			want: catalog.ErrMissingUnsupportedCapabilities,
		},
		{
			name: "partial blocking every declared atom",
			mutate: func(v *catalog.Variant) {
				v.ExecutionSupport = catalog.ExecutionSupportPartial
				v.UnsupportedCapabilities = append([]string(nil), v.Capabilities...)
			},
			want: catalog.ErrPartialWithNoExecutableCapability,
		},
		{
			name: "listed atom is not declared",
			mutate: func(v *catalog.Variant) {
				v.ExecutionSupport = catalog.ExecutionSupportPartial
				v.UnsupportedCapabilities = []string{"media_upload_resumable"}
			},
			want: catalog.ErrUndeclaredUnsupportedCapability,
		},
		{
			name: "typed_executor_required leaves a declared atom off the list",
			mutate: func(v *catalog.Variant) {
				v.ExecutionSupport = catalog.ExecutionSupportTypedExecutorRequired
				v.UnsupportedCapabilities = []string{v.Capabilities[0]}
			},
			want: catalog.ErrMissingUnsupportedCapabilities,
		},
		{
			name: "schema_only leaves a declared atom off the list",
			mutate: func(v *catalog.Variant) {
				v.ExecutionSupport = catalog.ExecutionSupportSchemaOnly
				v.UnsupportedCapabilities = []string{v.Capabilities[0]}
			},
			want: catalog.ErrMissingUnsupportedCapabilities,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := loadFixture(t, "sample-catalog.json")
			tc.mutate(&c.Ops[0].Variants[0])
			err := c.Validate()
			if err == nil {
				t.Fatalf("Validate()=nil; want %v", tc.want)
			}
			if !errors.Is(err, tc.want) {
				t.Fatalf("Validate()=%v; want %v", err, tc.want)
			}
		})
	}
}

// TestOpValidateAcceptsDeclaredExecutionSupport holds the other side: the
// three legal non-full shapes must load. A validator that rejected them would
// make the §918 states undeclarable, which is the gum-j6xl bug inverted.
func TestOpValidateAcceptsDeclaredExecutionSupport(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*catalog.Variant)
	}{
		{
			name: "partial lists one of six atoms",
			mutate: func(v *catalog.Variant) {
				v.ExecutionSupport = catalog.ExecutionSupportPartial
				v.UnsupportedCapabilities = []string{"field_mask"}
			},
		},
		{
			name: "typed_executor_required lists every atom",
			mutate: func(v *catalog.Variant) {
				v.ExecutionSupport = catalog.ExecutionSupportTypedExecutorRequired
				v.UnsupportedCapabilities = append([]string(nil), v.Capabilities...)
			},
		},
		{
			name: "schema_only declares no atoms and lists none",
			mutate: func(v *catalog.Variant) {
				v.ExecutionSupport = catalog.ExecutionSupportSchemaOnly
				v.Capabilities = nil
				v.UnsupportedCapabilities = nil
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := loadFixture(t, "sample-catalog.json")
			tc.mutate(&c.Ops[0].Variants[0])
			if err := c.Validate(); err != nil {
				t.Fatalf("Validate()=%v; want nil", err)
			}
		})
	}
}

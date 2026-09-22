package mcp

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/catalog"
)

// partialOp is an op whose default variant declares the §918
// `execution_support: "partial"` state: one declared atom executes, one does
// not.
func partialOp() *catalog.Op {
	return &catalog.Op{
		OpID:             "drive.v3.files.create",
		Title:            "Create file",
		Summary:          "Upload a file to Drive.",
		DefaultVariantID: "drive.v3.rest.files.create",
		Variants: []catalog.Variant{{
			VariantID:        "drive.v3.rest.files.create",
			Stability:        catalog.StabilityStable,
			InterfaceKind:    catalog.InterfaceKindDiscoveryREST,
			RiskClass:        catalog.RiskClassWrite,
			Scopes:           []string{"https://www.googleapis.com/auth/drive.file"},
			Capabilities:     []string{"json_request", "json_response", "media_upload_simple"},
			ExecutionSupport: catalog.ExecutionSupportPartial,
			// §918's worked example: the JSON request and response execute,
			// the media upload does not.
			UnsupportedCapabilities: []string{"media_upload_simple"},
		}},
	}
}

// TestDescribeOpResultAcceptsPartialExecutionSupport pins the gum-o293 fix.
// §918 closes `execution_support` over four values and defines "partial" with
// its own discriminator rule: `unsupported_capabilities` MUST list every
// non-executable atom. §13 omitted "partial" from both enums, so a variant that
// is legal per §918 produced structuredContent that failed the outputSchema
// §3175 binds it to.
func TestDescribeOpResultAcceptsPartialExecutionSupport(t *testing.T) {
	res := buildDescribeOpResult(partialOp(), defaultMaxVariants)

	if res.ExecutionSupport != "partial" {
		t.Fatalf("top-level execution_support = %q; want %q", res.ExecutionSupport, "partial")
	}
	if res.UnsupportedCapabilities == nil || len(*res.UnsupportedCapabilities) == 0 {
		t.Fatalf("partial op carries no unsupported_capabilities; §918 requires the list")
	}
	if res.Variants[0].ExecutionSupport != "partial" {
		t.Fatalf("variants[0].execution_support = %q; want %q", res.Variants[0].ExecutionSupport, "partial")
	}

	rs := compileSpecSchema(t, string(metaToolOutputSchema("gum.describe_op")))
	if err := rs.Validate(asJSON(t, res)); err != nil {
		got, _ := json.MarshalIndent(res, "", "  ")
		t.Fatalf("partial describe_op payload fails the registered outputSchema: %v\npayload:\n%s", err, got)
	}
}

// TestDescribeOpResultPartialRequiresUnsupportedCapabilities holds the other
// half of the §918 discriminator: a "partial" result without the list is
// invalid, exactly as "schema_only" and "typed_executor_required" are.
func TestDescribeOpResultPartialRequiresUnsupportedCapabilities(t *testing.T) {
	res := buildDescribeOpResult(partialOp(), defaultMaxVariants)
	res.UnsupportedCapabilities = nil

	rs := compileSpecSchema(t, string(metaToolOutputSchema("gum.describe_op")))
	if err := rs.Validate(asJSON(t, res)); err == nil {
		t.Fatal("partial payload without unsupported_capabilities validated; §918 requires the list")
	}
}

// TestDescribeOpResultUsesDeclaredUnsupportedCapabilities pins the gum-j6xl
// fix. describe_op used to substitute the variant's whole `capabilities[]` for
// the unsupported list, because the catalog ABI declared no field to read.
// That inference is wrong for "partial": §925 requires only the non-executable
// atoms, and the worked example in §918 has two of the three atoms executing.
func TestDescribeOpResultUsesDeclaredUnsupportedCapabilities(t *testing.T) {
	res := buildDescribeOpResult(partialOp(), defaultMaxVariants)

	if res.UnsupportedCapabilities == nil {
		t.Fatal("partial op carries no unsupported_capabilities; §925 requires the list")
	}
	got := *res.UnsupportedCapabilities
	want := []string{"media_upload_simple"}
	if !slices.Equal(got, want) {
		t.Fatalf("unsupported_capabilities = %v; want %v (the declared list, not capabilities[])", got, want)
	}
}

// TestDescribeOpResultWarnsOnBlockedCapabilityClasses pins §5.8 checklist item
// 5: a new non-executable atom must reach describe_op as prose, not only as
// the `execution_support` discriminator. The registered outputSchema has
// carried `capability_class_warnings` since v0.1 with no producer, so the
// field was always absent and a caller who read the answer learned nothing.
func TestDescribeOpResultWarnsOnBlockedCapabilityClasses(t *testing.T) {
	res := buildDescribeOpResult(partialOp(), defaultMaxVariants)

	want := []string{"media_upload_simple is cataloged but not executable in this release"}
	if !slices.Equal(res.CapabilityClassWarnings, want) {
		t.Fatalf("capability_class_warnings = %v; want %v", res.CapabilityClassWarnings, want)
	}

	rs := compileSpecSchema(t, string(metaToolOutputSchema("gum.describe_op")))
	if err := rs.Validate(asJSON(t, res)); err != nil {
		got, _ := json.MarshalIndent(res, "", "  ")
		t.Fatalf("warned describe_op payload fails the registered outputSchema: %v\npayload:\n%s", err, got)
	}
}

// TestDescribeOpResultOmitsWarningsWhenNothingIsBlocked holds the other half:
// the field is a warning, so an op that executes every declared atom must not
// carry an empty array that a client would render as a heading with no rows.
func TestDescribeOpResultOmitsWarningsWhenNothingIsBlocked(t *testing.T) {
	op := partialOp()
	op.Variants[0].ExecutionSupport = ""
	op.Variants[0].UnsupportedCapabilities = nil

	res := buildDescribeOpResult(op, defaultMaxVariants)

	if res.CapabilityClassWarnings != nil {
		t.Fatalf("capability_class_warnings = %v on a full variant; want absent", res.CapabilityClassWarnings)
	}
	payload, err := json.Marshal(res)
	if err != nil {
		t.Fatalf("marshal describe_op result: %v", err)
	}
	if strings.Contains(string(payload), "capability_class_warnings") {
		t.Fatalf("full-variant payload carries the warning key:\n%s", payload)
	}
}

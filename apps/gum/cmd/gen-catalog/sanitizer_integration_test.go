package main_test

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"go.uber.org/goleak"

	gencatalog "github.com/ehmo/gum/cmd/gen-catalog"
	"github.com/ehmo/gum/internal/sanitize"
)

// TestSanitizerEnforcement is the release-blocker gate: it runs every op
// description in the generated catalog (built from the Gmail + Calendar fixtures)
// through sanitize.Sanitize and fails if any violation is present.
//
// toolKind is derived from the op's service_family:
//   - "meta" when service_family ∈ {"meta"}
//   - "convenience" otherwise
//
// riskClass is taken from the op's default variant.
func TestSanitizerEnforcement(t *testing.T) {
	defer goleak.VerifyNone(t)

	// Use the phase-4 fixture which includes send+trash (required by GenerateFromDiscoveries).
	gmailFixture, err := os.Open("testdata/gmail-discovery-phase4.json")
	if err != nil {
		t.Fatalf("open gmail phase4 fixture: %v", err)
	}
	defer func() { _ = gmailFixture.Close() }()

	calendarFixture, err := os.Open("testdata/calendar-discovery.json")
	if err != nil {
		t.Fatalf("open calendar fixture: %v", err)
	}
	defer func() { _ = calendarFixture.Close() }()

	cat, err := gencatalog.GenerateFromDiscoveries(gmailFixture, calendarFixture)
	if err != nil {
		t.Fatalf("GenerateFromDiscoveries: %v", err)
	}

	var failures []string

	for _, op := range cat.Ops {
		// Determine toolKind from service_family.
		toolKind := "convenience"
		if op.ServiceFamily == "meta" {
			toolKind = "meta"
		}

		// Find the default variant's risk_class.
		riskClass := "read"
		for _, v := range op.Variants {
			if v.VariantID == op.DefaultVariantID {
				riskClass = string(v.RiskClass)
				break
			}
		}

		// Run the sanitizer on the op's title and summary.
		for _, field := range []struct {
			name  string
			value string
		}{
			{"title", op.Title},
			{"summary", op.Summary},
		} {
			_, vs, err := sanitize.Sanitize(field.value, toolKind, riskClass)
			if err != nil {
				failures = append(failures, fmt.Sprintf(
					"op=%s field=%s: sanitizer internal error: %v",
					op.OpID, field.name, err,
				))
				continue
			}
			for _, v := range vs {
				failures = append(failures, fmt.Sprintf(
					"op=%s field=%s rule=%d offending=%q reason=%s",
					op.OpID, field.name, v.Rule, v.Offending, v.Reason,
				))
			}
		}
	}

	if len(failures) > 0 {
		t.Errorf("sanitizer violations in generated catalog (%d):\n  %s",
			len(failures), strings.Join(failures, "\n  "))
	}
}

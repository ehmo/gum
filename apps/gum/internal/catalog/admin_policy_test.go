package catalog_test

import (
	"errors"
	"testing"

	"github.com/ehmo/gum/internal/catalog"
)

func TestAdminPolicyValidation(t *testing.T) {
	policy := &catalog.AdminPolicy{
		BlastRadius:              catalog.AdminBlastRadiusFixtureWrite,
		FixtureOwnershipRequired: true,
		FixtureMarkerPrefix:      catalog.AdminFixtureMarkerPrefix,
		FixtureResourceKeys:      []string{"groupKey"},
	}
	if err := policy.Validate(); err != nil {
		t.Fatalf("fixture policy Validate() = %v, want nil", err)
	}

	bad := &catalog.AdminPolicy{BlastRadius: "admin_unknown"}
	if err := bad.Validate(); !errors.Is(err, catalog.ErrUnknownAdminBlastRadius) {
		t.Fatalf("unknown blast radius err = %v, want ErrUnknownAdminBlastRadius", err)
	}

	incomplete := &catalog.AdminPolicy{BlastRadius: catalog.AdminBlastRadiusFixtureWrite}
	if err := incomplete.Validate(); !errors.Is(err, catalog.ErrMissingAdminPolicy) {
		t.Fatalf("incomplete fixture policy err = %v, want ErrMissingAdminPolicy", err)
	}
}

func TestAdminWriteVariantsRequirePolicy(t *testing.T) {
	cat := loadFixture(t, "sample-catalog.json")
	op := cat.Ops[0]
	op.Service = "admin"
	op.Variants[0].RiskClass = catalog.RiskClassWrite
	op.Variants[0].AdminPolicy = nil
	cat.Ops = []catalog.Op{op}

	if err := cat.Validate(); !errors.Is(err, catalog.ErrMissingAdminPolicy) {
		t.Fatalf("admin write without policy err = %v, want ErrMissingAdminPolicy", err)
	}

	cat.Ops[0].Variants[0].AdminPolicy = &catalog.AdminPolicy{
		BlastRadius:              catalog.AdminBlastRadiusFixtureWrite,
		FixtureOwnershipRequired: true,
		FixtureMarkerPrefix:      catalog.AdminFixtureMarkerPrefix,
		FixtureResourceKeys:      []string{"userKey"},
	}
	if err := cat.Validate(); err != nil {
		t.Fatalf("admin write with fixture policy Validate() = %v, want nil", err)
	}
}

func TestValidateAdminFixtureOwnership(t *testing.T) {
	policy := &catalog.AdminPolicy{
		BlastRadius:              catalog.AdminBlastRadiusFixtureWrite,
		FixtureOwnershipRequired: true,
		FixtureMarkerPrefix:      catalog.AdminFixtureMarkerPrefix,
		FixtureResourceKeys:      []string{"userKey", "groupKey", "memberKey"},
	}

	good := map[string]any{
		"userKey":   "gum-fixture-user@example.com",
		"groupKey":  "groups/gum-fixture-group",
		"memberKey": "gum-fixture-member",
	}
	if err := catalog.ValidateAdminFixtureOwnership(good, policy); err != nil {
		t.Fatalf("ValidateAdminFixtureOwnership(good) = %v, want nil", err)
	}

	bad := map[string]any{
		"userKey":   "alice@example.com",
		"groupKey":  "gum-fixture-group@example.com",
		"memberKey": "gum-fixture-member",
	}
	if err := catalog.ValidateAdminFixtureOwnership(bad, policy); !errors.Is(err, catalog.ErrAdminFixtureOwnership) {
		t.Fatalf("ValidateAdminFixtureOwnership(non-fixture user) = %v, want ErrAdminFixtureOwnership", err)
	}

	domainSpoof := map[string]any{
		"userKey":   "alice@gum-fixture-example.com",
		"groupKey":  "gum-fixture-group@example.com",
		"memberKey": "gum-fixture-member",
	}
	if err := catalog.ValidateAdminFixtureOwnership(domainSpoof, policy); !errors.Is(err, catalog.ErrAdminFixtureOwnership) {
		t.Fatalf("ValidateAdminFixtureOwnership(domain spoof) = %v, want ErrAdminFixtureOwnership", err)
	}

	bodyGood := map[string]any{
		"body": map[string]any{
			"userKey":   "gum-fixture-user@example.com",
			"groupKey":  "groups/gum-fixture-group",
			"memberKey": "gum-fixture-member",
		},
	}
	if err := catalog.ValidateAdminFixtureOwnership(bodyGood, policy); err != nil {
		t.Fatalf("ValidateAdminFixtureOwnership(body good) = %v, want nil", err)
	}

	bodyBad := map[string]any{
		"body": map[string]any{
			"userKey":   "alice@example.com",
			"groupKey":  "groups/gum-fixture-group",
			"memberKey": "gum-fixture-member",
		},
	}
	if err := catalog.ValidateAdminFixtureOwnership(bodyBad, policy); !errors.Is(err, catalog.ErrAdminFixtureOwnership) {
		t.Fatalf("ValidateAdminFixtureOwnership(body non-fixture user) = %v, want ErrAdminFixtureOwnership", err)
	}
}

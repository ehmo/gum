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

// TestIsAdminFixtureResourceRejectsPlainValues verifies the marker check is a
// prefix test on the bare value, the email local part, or the last path
// segment, and nothing else.
func TestIsAdminFixtureResourceRejectsPlainValues(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"", false},
		{"   ", false},
		{"alice", false},
		{"groups/engineering", false},
		{"@gum-fixture-user", false},
		{"GUM-FIXTURE-USER", true},
		{"  gum-fixture-user  ", true},
		{"gum-fixture-user@example.com", true},
		{"groups/gum-fixture-group", true},
	}
	for _, c := range cases {
		if got := catalog.IsAdminFixtureResource(c.in); got != c.want {
			t.Errorf("IsAdminFixtureResource(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

// TestValidateAdminFixtureOwnershipNoPolicy verifies the guard is inert when no
// policy demands fixture ownership, so read ops pay nothing for it.
func TestValidateAdminFixtureOwnershipNoPolicy(t *testing.T) {
	if err := catalog.ValidateAdminFixtureOwnership(map[string]any{"userKey": "alice"}, nil); err != nil {
		t.Fatalf("nil policy err = %v, want nil", err)
	}

	off := &catalog.AdminPolicy{
		BlastRadius:         catalog.AdminBlastRadiusFixtureWrite,
		FixtureResourceKeys: []string{"userKey"},
	}
	if err := catalog.ValidateAdminFixtureOwnership(map[string]any{"userKey": "alice"}, off); err != nil {
		t.Fatalf("ownership-not-required err = %v, want nil", err)
	}
}

// TestValidateAdminFixtureOwnershipMissingKey verifies an absent key fails
// instead of passing as an unnamed resource.
func TestValidateAdminFixtureOwnershipMissingKey(t *testing.T) {
	policy := &catalog.AdminPolicy{
		BlastRadius:              catalog.AdminBlastRadiusFixtureWrite,
		FixtureOwnershipRequired: true,
		FixtureMarkerPrefix:      catalog.AdminFixtureMarkerPrefix,
		FixtureResourceKeys:      []string{"userKey"},
	}

	if err := catalog.ValidateAdminFixtureOwnership(map[string]any{}, policy); !errors.Is(err, catalog.ErrAdminFixtureOwnership) {
		t.Fatalf("no args err = %v, want ErrAdminFixtureOwnership", err)
	}

	otherKey := map[string]any{"body": map[string]any{"groupKey": "gum-fixture-group"}}
	if err := catalog.ValidateAdminFixtureOwnership(otherKey, policy); !errors.Is(err, catalog.ErrAdminFixtureOwnership) {
		t.Fatalf("body without key err = %v, want ErrAdminFixtureOwnership", err)
	}

	nonString := map[string]any{"userKey": 42}
	if err := catalog.ValidateAdminFixtureOwnership(nonString, policy); !errors.Is(err, catalog.ErrAdminFixtureOwnership) {
		t.Fatalf("non-string key err = %v, want ErrAdminFixtureOwnership", err)
	}
}

// TestValidateAdminFixtureOwnershipBodyShapes verifies the key lookup reads a
// map[string]string body as well as map[string]any, and refuses any other body
// type rather than treating it as absent-but-fine.
func TestValidateAdminFixtureOwnershipBodyShapes(t *testing.T) {
	policy := &catalog.AdminPolicy{
		BlastRadius:              catalog.AdminBlastRadiusFixtureWrite,
		FixtureOwnershipRequired: true,
		FixtureMarkerPrefix:      catalog.AdminFixtureMarkerPrefix,
		FixtureResourceKeys:      []string{"userKey"},
	}

	stringBody := map[string]any{"body": map[string]string{"userKey": "gum-fixture-user@example.com"}}
	if err := catalog.ValidateAdminFixtureOwnership(stringBody, policy); err != nil {
		t.Fatalf("map[string]string body err = %v, want nil", err)
	}

	scalarBody := map[string]any{"body": "gum-fixture-user"}
	if err := catalog.ValidateAdminFixtureOwnership(scalarBody, policy); !errors.Is(err, catalog.ErrAdminFixtureOwnership) {
		t.Fatalf("scalar body err = %v, want ErrAdminFixtureOwnership", err)
	}
}

// TestAdminPolicyValidateFixtureFieldsIndividually verifies each required
// fixture field is checked on its own, so a policy that sets only one of them
// still fails.
func TestAdminPolicyValidateFixtureFieldsIndividually(t *testing.T) {
	noPrefix := &catalog.AdminPolicy{
		BlastRadius:              catalog.AdminBlastRadiusFixtureWrite,
		FixtureOwnershipRequired: true,
		FixtureResourceKeys:      []string{"userKey"},
	}
	if err := noPrefix.Validate(); !errors.Is(err, catalog.ErrMissingAdminPolicy) {
		t.Fatalf("missing marker prefix err = %v, want ErrMissingAdminPolicy", err)
	}

	noKeys := &catalog.AdminPolicy{
		BlastRadius:              catalog.AdminBlastRadiusFixtureWrite,
		FixtureOwnershipRequired: true,
		FixtureMarkerPrefix:      catalog.AdminFixtureMarkerPrefix,
	}
	if err := noKeys.Validate(); !errors.Is(err, catalog.ErrMissingAdminPolicy) {
		t.Fatalf("missing resource keys err = %v, want ErrMissingAdminPolicy", err)
	}
}

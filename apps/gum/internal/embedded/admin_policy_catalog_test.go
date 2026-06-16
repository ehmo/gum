package embedded_test

import (
	"testing"

	"github.com/ehmo/gum/internal/catalog"
)

func TestEmbeddedAdminWritesCarryFixturePolicy(t *testing.T) {
	cat := loadEmbeddedCatalog(t)
	for i := range cat.Ops {
		op := &cat.Ops[i]
		if op.Service != "admin" {
			continue
		}
		v := defaultVariant(op)
		if v == nil || v.RiskClass == catalog.RiskClassRead {
			continue
		}
		if v.AdminPolicy == nil {
			t.Fatalf("%s: Admin write/destructive variant missing admin_policy", op.OpID)
		}
		if v.AdminPolicy.BlastRadius != catalog.AdminBlastRadiusFixtureWrite {
			t.Errorf("%s: blast_radius=%q want %q", op.OpID, v.AdminPolicy.BlastRadius, catalog.AdminBlastRadiusFixtureWrite)
		}
		if !v.AdminPolicy.FixtureOwnershipRequired {
			t.Errorf("%s: fixture ownership is not required", op.OpID)
		}
		if len(v.AdminPolicy.FixtureResourceKeys) == 0 {
			t.Errorf("%s: fixture_resource_keys empty", op.OpID)
		}
	}
}

func TestEmbeddedHighBlastRadiusAdminOpsAbsent(t *testing.T) {
	cat := loadEmbeddedCatalog(t)
	ops := make(map[string]bool, len(cat.Ops))
	for _, op := range cat.Ops {
		ops[op.OpID] = true
	}

	absent := []string{
		"admin.directory.orgunits.insert",
		"admin.directory.orgunits.update",
		"admin.directory.roles.insert",
		"admin.directory.roleAssignments.insert",
		"admin.directory.domains.insert",
		"admin.directory.domainAliases.insert",
		"admin.directory.verificationCodes.generate",
		"admin.directory.chromeosdevices.action.batch",
	}
	for _, opID := range absent {
		if ops[opID] {
			t.Fatalf("high-blast Admin op %s is present in embedded catalog", opID)
		}
	}
}

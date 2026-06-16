package main_test

import (
	"os"
	"testing"

	gencatalog "github.com/ehmo/gum/cmd/gen-catalog"
	"github.com/ehmo/gum/internal/catalog"
)

// TestGenerateEmitsGmailSendOp verifies that GenerateFromDiscovery emits
// gmail.users.messages.send with risk_class=write when the discovery doc
// contains the send method. (Phase 4 scope item 9)
func TestGenerateEmitsGmailSendOp(t *testing.T) {
	f, err := os.Open("testdata/gmail-discovery-phase4.json")
	if err != nil {
		t.Fatalf("open phase4 gmail fixture: %v (create testdata/gmail-discovery-phase4.json with send+trash methods)", err)
	}
	defer func() { _ = f.Close() }()

	cat, err := gencatalog.GenerateFromDiscovery(f)
	if err != nil {
		t.Fatalf("GenerateFromDiscovery(phase4-gmail): %v", err)
	}

	if err := cat.Validate(); err != nil {
		t.Fatalf("Validate() after phase4 gmail generation: %v", err)
	}

	// Assert gmail.users.messages.send is emitted.
	var sendOp *catalog.Op
	for i := range cat.Ops {
		if cat.Ops[i].OpID == "gmail.users.messages.send" {
			sendOp = &cat.Ops[i]
			break
		}
	}
	if sendOp == nil {
		t.Fatal("generator did not emit gmail.users.messages.send op")
	}
	if len(sendOp.Variants) == 0 {
		t.Fatal("gmail.users.messages.send: no variants emitted")
	}
	for _, v := range sendOp.Variants {
		if v.RiskClass != catalog.RiskClassWrite {
			t.Errorf("gmail.users.messages.send variant %q: risk_class=%q, want %q",
				v.VariantID, v.RiskClass, catalog.RiskClassWrite)
		}
		if v.AuthStrategy == catalog.AuthStrategyGUMOAuth {
			t.Errorf("gmail.users.messages.send: must not use gum_oauth")
		}
	}

	// Assert gmail.users.messages.trash is emitted.
	var trashOp *catalog.Op
	for i := range cat.Ops {
		if cat.Ops[i].OpID == "gmail.users.messages.trash" {
			trashOp = &cat.Ops[i]
			break
		}
	}
	if trashOp == nil {
		t.Fatal("generator did not emit gmail.users.messages.trash op")
	}
	for _, v := range trashOp.Variants {
		if v.RiskClass != catalog.RiskClassDestructive {
			t.Errorf("gmail.users.messages.trash variant %q: risk_class=%q, want %q",
				v.VariantID, v.RiskClass, catalog.RiskClassDestructive)
		}
	}
}

// TestGenerateFromDiscoveriesEmitsPhase4Ops verifies that GenerateFromDiscoveries
// emits the Phase 4 write and destructive ops.
func TestGenerateFromDiscoveriesEmitsPhase4Ops(t *testing.T) {
	gmailF, err := os.Open("testdata/gmail-discovery-phase4.json")
	if err != nil {
		t.Fatalf("open phase4 gmail fixture: %v", err)
	}
	defer func() { _ = gmailF.Close() }()

	calF, err := os.Open("testdata/calendar-discovery.json")
	if err != nil {
		t.Fatalf("open calendar fixture: %v", err)
	}
	defer func() { _ = calF.Close() }()

	cat, err := gencatalog.GenerateFromDiscoveries(gmailF, calF)
	if err != nil {
		t.Fatalf("GenerateFromDiscoveries: %v", err)
	}

	if err := cat.Validate(); err != nil {
		t.Fatalf("Validate(): %v", err)
	}

	opMap := make(map[string]*catalog.Op, len(cat.Ops))
	for i := range cat.Ops {
		opMap[cat.Ops[i].OpID] = &cat.Ops[i]
	}

	wantOps := []struct {
		opID      string
		riskClass catalog.RiskClass
	}{
		{"gmail.users.messages.list", catalog.RiskClassRead},
		{"gmail.users.messages.get", catalog.RiskClassRead},
		{"gmail.users.labels.list", catalog.RiskClassRead},
		{"gmail.users.messages.send", catalog.RiskClassWrite},
		{"gmail.users.messages.trash", catalog.RiskClassDestructive},
		{"calendar.events.list", catalog.RiskClassRead},
		{"calendar.calendarList.list", catalog.RiskClassRead},
	}

	for _, want := range wantOps {
		op, ok := opMap[want.opID]
		if !ok {
			t.Errorf("op %q not found in generated catalog", want.opID)
			continue
		}
		for _, v := range op.Variants {
			if v.RiskClass != want.riskClass {
				t.Errorf("op %q variant %q: risk_class=%q, want %q",
					want.opID, v.VariantID, v.RiskClass, want.riskClass)
			}
		}
	}
}

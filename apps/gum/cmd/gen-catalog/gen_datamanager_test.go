package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/catalog"
)

func TestBuildDataManagerOps(t *testing.T) {
	ops := BuildDataManagerOps()
	if len(ops) != 2 {
		t.Fatalf("BuildDataManagerOps len = %d; want 2", len(ops))
	}

	type want struct {
		method string
		path   string
		risk   catalog.RiskClass
	}
	wantIDs := map[string]want{
		"datamanager.events.ingest":          {"POST", dataManagerBase + "/events:ingest", catalog.RiskClassWrite},
		"datamanager.requestStatus.retrieve": {"GET", dataManagerBase + "/requestStatus:retrieve", catalog.RiskClassRead},
	}

	seen := map[string]bool{}
	for _, op := range ops {
		w, ok := wantIDs[op.OpID]
		if !ok {
			t.Errorf("unexpected op_id %q", op.OpID)
			continue
		}
		seen[op.OpID] = true

		if op.Service != "datamanager" {
			t.Errorf("%s: service = %q; want datamanager", op.OpID, op.Service)
		}
		// The ops write to a Google Ads account, so they stay in the googleads
		// family. That also keeps them out of the workspace-only dispatch-stub
		// generator.
		if op.ServiceFamily != "googleads" {
			t.Errorf("%s: family = %q; want googleads", op.OpID, op.ServiceFamily)
		}
		if len(op.Variants) != 1 {
			t.Fatalf("%s: variants = %d; want 1", op.OpID, len(op.Variants))
		}
		v := op.Variants[0]
		if v.VariantID != op.DefaultVariantID {
			t.Errorf("%s: default_variant_id %q is not the only variant %q", op.OpID, op.DefaultVariantID, v.VariantID)
		}
		if v.AuthStrategy != catalog.AuthStrategyBYOOAuth {
			t.Errorf("%s: auth = %q; want byo_oauth", op.OpID, v.AuthStrategy)
		}
		// Unlike the googleads ops this is a plain OAuth REST API: no developer
		// token, so the generic typed-rest-sdk adapter carries it.
		if v.BackendKind != catalog.BackendKindTypedRestSDK {
			t.Errorf("%s: backend = %q; want typed-rest-sdk", op.OpID, v.BackendKind)
		}
		if len(v.Scopes) != 1 || v.Scopes[0] != scopeDataManager {
			t.Errorf("%s: scopes = %v; want [%s]", op.OpID, v.Scopes, scopeDataManager)
		}
		if v.RiskClass != w.risk {
			t.Errorf("%s: risk = %q; want %q", op.OpID, v.RiskClass, w.risk)
		}
		if v.Binding == nil || v.Binding.AdapterKey != "rest.typed-rest-sdk" {
			t.Errorf("%s: adapter_key = %v; want rest.typed-rest-sdk", op.OpID, v.Binding)
			continue
		}
		if v.Binding.OperationKey != op.OpID {
			t.Errorf("%s: operation_key = %q; want the op id", op.OpID, v.Binding.OperationKey)
		}
		if v.Binding.HTTP == nil || v.Binding.HTTP.Method != w.method || v.Binding.HTTP.Path != w.path {
			t.Errorf("%s: http = %+v; want %s %s", op.OpID, v.Binding.HTTP, w.method, w.path)
		}
		// The adapter substitutes {placeholders} from path fields; these two
		// endpoints are custom methods with no path parameters at all.
		if strings.Contains(v.Binding.HTTP.Path, "{") {
			t.Errorf("%s: path %q must carry no placeholders", op.OpID, v.Binding.HTTP.Path)
		}
	}

	for id := range wantIDs {
		if !seen[id] {
			t.Errorf("missing op %q", id)
		}
	}

	// The ingest body is what the offline-conversion rail actually posts, so the
	// two required members are load-bearing. They must be body-located: the
	// typed-rest-sdk adapter sends every non-path arg as a query parameter.
	ingest := opByID(t, ops, "datamanager.events.ingest")
	wantBody := map[string]bool{"destinations": true, "events": true} // name -> required
	for _, name := range []string{"consent", "validateOnly", "encoding", "encryptionInfo"} {
		wantBody[name] = false
	}
	for _, f := range ingest.RequestFields {
		req, ok := wantBody[f.Name]
		if !ok {
			t.Errorf("ingest: unexpected request field %q", f.Name)
			continue
		}
		delete(wantBody, f.Name)
		if f.Location != catalog.RequestFieldBody {
			t.Errorf("ingest: field %q location = %q; want body", f.Name, f.Location)
		}
		if f.Required != req {
			t.Errorf("ingest: field %q required = %v; want %v", f.Name, f.Required, req)
		}
	}
	for name := range wantBody {
		t.Errorf("ingest: missing request field %q", name)
	}

	// requestId is a real query parameter on the retrieve endpoint, and the only
	// way to read back an asynchronous ingest.
	status := opByID(t, ops, "datamanager.requestStatus.retrieve")
	if len(status.RequestFields) != 1 {
		t.Fatalf("retrieve: request fields = %d; want 1", len(status.RequestFields))
	}
	rf := status.RequestFields[0]
	if rf.Name != "requestId" || rf.Location != catalog.RequestFieldQuery || !rf.Required {
		t.Errorf("retrieve: field = %+v; want required query requestId", rf)
	}

	// The ops must pass catalog validation as part of a catalog.
	cat := &catalog.Catalog{
		CatalogSchemaVersion: 1,
		GeneratedAt:          "2026-09-07T00:00:00Z",
		GeneratorVersion:     "test",
		Ops:                  ops,
	}
	if err := cat.Validate(); err != nil {
		t.Fatalf("catalog.Validate with datamanager ops: %v", err)
	}
}

func opByID(t *testing.T, ops []catalog.Op, id string) catalog.Op {
	t.Helper()
	for _, op := range ops {
		if op.OpID == id {
			return op
		}
	}
	t.Fatalf("op %q not built", id)
	return catalog.Op{}
}

func TestInjectDataManagerOffline(t *testing.T) {
	path := filepath.Join(t.TempDir(), "catalog.json")
	cat := catalog.Catalog{CatalogSchemaVersion: 1, GeneratedAt: "2026-09-07T00:00:00Z", GeneratorVersion: "test", Ops: BuildGoogleAdsOps()}
	data, err := json.Marshal(cat)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err = injectDataManager(path); err != nil {
			t.Fatal(err)
		}
		data, err = os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err = json.Unmarshal(data, &cat); err != nil {
			t.Fatal(err)
		}
		if len(cat.Ops) != len(BuildGoogleAdsOps())+2 {
			t.Fatalf("duplicate or lost ops: %d", len(cat.Ops))
		}
		if cat.GeneratedAt != "2026-09-07T00:00:00Z" {
			t.Fatal("generation date changed")
		}
		want := sha256.Sum256(data)
		actual, err := os.ReadFile(path + ".sha256")
		if err != nil {
			t.Fatal(err)
		}
		if string(actual) != fmt.Sprintf("%x  catalog.json\n", want) {
			t.Fatal("checksum mismatch")
		}
	}
}

func TestInjectOpsOfflineRejectsBrokenCatalog(t *testing.T) {
	path := filepath.Join(t.TempDir(), "catalog.json")
	if err := injectDataManager(path); err == nil {
		t.Fatal("missing catalog accepted")
	}
	for _, data := range []string{"{", `{"catalog_schema_version":99}`} {
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		if err := injectDataManager(path); err == nil {
			t.Fatal("broken catalog accepted")
		}
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != data {
			t.Fatal("failed injection changed catalog")
		}
	}
}

package routing_test

import (
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/lro/routing"
)

// TestLookupGoogleLongrunningGRPC pins that bare "operations/<id>" routes to
// the canonical google.longrunning gRPC endpoint.
func TestLookupGoogleLongrunningGRPC(t *testing.T) {
	ep, key, ok := routing.Lookup("operations/abc-123")
	if !ok {
		t.Fatal("Lookup(operations/abc-123) returned no match")
	}
	if key != "operations/" {
		t.Errorf("matched prefix=%q want operations/", key)
	}
	if ep.Transport != routing.TransportGRPC {
		t.Errorf("transport=%q want grpc", ep.Transport)
	}
	if ep.Pkg != "cloud.google.com/go/longrunning" {
		t.Errorf("pkg=%q want cloud.google.com/go/longrunning", ep.Pkg)
	}
}

// TestLookupComputeZoneOperations pins that a zoned Compute operation routes
// to the ZoneOperations REST endpoint with proper path templating.
func TestLookupComputeZoneOperations(t *testing.T) {
	name := "projects/my-proj/zones/us-central1-a/operations/op-555"
	ep, _, ok := routing.Lookup(name)
	if !ok {
		t.Fatal("Lookup zoned compute operation returned no match")
	}
	if ep.Resource != "ZoneOperations" {
		t.Errorf("resource=%q want ZoneOperations", ep.Resource)
	}
	if ep.Host != "compute.googleapis.com" {
		t.Errorf("host=%q want compute.googleapis.com", ep.Host)
	}
	got := routing.SubstitutePath(ep, name)
	want := "/compute/v1/projects/my-proj/zones/us-central1-a/operations/op-555"
	if got != want {
		t.Errorf("SubstitutePath got %q want %q", got, want)
	}
}

// TestLookupComputeRegionOperations pins that a regional Compute operation
// routes to RegionOperations.
func TestLookupComputeRegionOperations(t *testing.T) {
	name := "projects/p/regions/us-east1/operations/op-1"
	ep, _, ok := routing.Lookup(name)
	if !ok {
		t.Fatal("Lookup returned no match")
	}
	if ep.Resource != "RegionOperations" {
		t.Errorf("resource=%q want RegionOperations", ep.Resource)
	}
	got := routing.SubstitutePath(ep, name)
	if !strings.Contains(got, "/regions/us-east1/") {
		t.Errorf("substituted path %q missing /regions/us-east1/", got)
	}
}

// TestLookupCloudRunRoute pins that projects/*/locations/*/operations/ routes
// to Cloud Run's REST endpoint.
func TestLookupCloudRunRoute(t *testing.T) {
	name := "projects/p/locations/europe-west1/operations/cr-op"
	ep, _, ok := routing.Lookup(name)
	if !ok {
		t.Fatal("Lookup returned no match")
	}
	if ep.Host != "run.googleapis.com" {
		t.Errorf("host=%q want run.googleapis.com", ep.Host)
	}
	got := routing.SubstitutePath(ep, name)
	if !strings.HasPrefix(got, "/v2/") || !strings.HasSuffix(got, "cr-op") {
		t.Errorf("substituted path %q does not look right", got)
	}
}

// TestLookupSpecificityWins pins that a more-specific Compute pattern beats
// the generic projects/*/locations/*/operations/ Cloud Run pattern when both
// could syntactically match. (They don't actually conflict because Compute
// uses zones/regions/global instead of locations, but the lookup must still
// pick the highest-score match.)
func TestLookupSpecificityWins(t *testing.T) {
	// projects/*/global/operations/ should win over the generic catchall
	// even though both have wildcards.
	ep, key, ok := routing.Lookup("projects/p/global/operations/op-x")
	if !ok {
		t.Fatal("Lookup returned no match")
	}
	if key != "projects/*/global/operations/" {
		t.Errorf("matched key=%q want projects/*/global/operations/", key)
	}
	if ep.Resource != "GlobalOperations" {
		t.Errorf("resource=%q want GlobalOperations", ep.Resource)
	}
}

// TestLookupUnknownOperation pins that an opaque service name with no entry
// returns ok=false so the caller runs the fallback templates.
func TestLookupUnknownOperation(t *testing.T) {
	_, _, ok := routing.Lookup("totallyunknownservice/foo/bar/baz")
	if ok {
		t.Error("Lookup returned a match for an unrecognised name; expected fallback signal")
	}
}

// TestSubstitutePathOperationNameTail covers the {operation_name_tail}
// placeholder against the global-operations entry.
func TestSubstitutePathOperationNameTail(t *testing.T) {
	ep := routing.ServiceByPrefix["projects/*/global/operations/"]
	got := routing.SubstitutePath(ep, "projects/foo/global/operations/op-7")
	want := "/compute/v1/projects/foo/global/operations/op-7"
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

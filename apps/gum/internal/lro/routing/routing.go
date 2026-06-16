// Package routing carries the §5.7 LRO routing table used by gum.poll to
// resolve a Google long-running-operation name to the upstream Operations
// endpoint that knows how to fetch its state.
//
// Two artifacts live here:
//
//  1. ServiceByPrefix — the generated map illustrated in spec.md §5.7
//     ("var lroServiceByPrefix = map[string]lroEndpoint{ … }"). For v0.1.0 the
//     entries are hand-curated against the public Google API surface; the
//     daily catalog regen workflow (gum-7ht) will rebuild this from the
//     resources.operations.methods.get walk of each discovery doc in a future
//     release.
//  2. Lookup() — pattern matcher with prefix specificity (most specific
//     literal pattern wins; wildcard segments are scored last).
//
// The fallback templates (spec §5.7 lines 861-868) are NOT in this package —
// they live next to the HTTP fetcher in internal/lro where the actual HTTP
// requests get built.
package routing

import (
	"strings"
)

// Transport is the closed enum of upstream protocols for an Operations endpoint.
type Transport string

const (
	TransportREST Transport = "rest"
	TransportGRPC Transport = "grpc"
)

// Endpoint identifies one upstream Operations endpoint capable of serving
// GET status for a matching operation-name prefix.
//
// For REST endpoints, Host + Path together describe the URL template — Path
// MUST contain {operation_name} or {operation_name_tail} so the caller can
// substitute. For gRPC endpoints, Pkg + Resource together pick the SDK
// package and the *Service / *Client type to call .GetOperation on.
type Endpoint struct {
	Pkg       string    // e.g. "compute/v1" or "cloud.google.com/go/longrunning"
	Resource  string    // e.g. "ZoneOperations" or empty for grpc
	Transport Transport // rest | grpc
	// Host is the upstream URL host used by REST endpoints. Empty for grpc.
	// Example: "compute.googleapis.com".
	Host string
	// Path is the URL template path. Substitutions:
	//   {operation_name}      — full operation name verbatim
	//   {operation_name_tail} — last path segment after the final "/"
	//   {project} {zone} {region} — extracted from the operation-name prefix
	// Empty for grpc endpoints.
	Path string
}

// ServiceByPrefix maps operation-name prefix patterns to upstream endpoints.
// Per spec §5.7: per-service entries take precedence over pattern fallbacks.
// Pattern matching rules:
//
//   - "foo/" matches any name beginning with literal "foo/"
//   - "projects/*/foo/" matches "projects/<anything>/foo/..."
//   - "*" inside a segment matches the entire segment (no slashes)
//
// Most-specific match wins (literal segments outscore wildcards). The
// fallback "locations/*/operations/" entry intentionally sits at the bottom
// so concrete service prefixes claim their names first.
var ServiceByPrefix = map[string]Endpoint{
	// google.longrunning.Operations gRPC route (covers AI Platform, Cloud
	// Build, Dataflow, Workflows, and ~30 other services that route through
	// the canonical google.longrunning.Operations service).
	"operations/": {
		Pkg:       "cloud.google.com/go/longrunning",
		Transport: TransportGRPC,
	},

	// Compute Engine (REST). Compute has three operation-namespace shapes:
	// global, regional, and zonal — each lives at its own endpoint.
	"projects/*/global/operations/": {
		Pkg:       "compute/v1",
		Resource:  "GlobalOperations",
		Transport: TransportREST,
		Host:      "compute.googleapis.com",
		Path:      "/compute/v1/projects/{project}/global/operations/{operation_name_tail}",
	},
	"projects/*/regions/*/operations/": {
		Pkg:       "compute/v1",
		Resource:  "RegionOperations",
		Transport: TransportREST,
		Host:      "compute.googleapis.com",
		Path:      "/compute/v1/projects/{project}/regions/{region}/operations/{operation_name_tail}",
	},
	"projects/*/zones/*/operations/": {
		Pkg:       "compute/v1",
		Resource:  "ZoneOperations",
		Transport: TransportREST,
		Host:      "compute.googleapis.com",
		Path:      "/compute/v1/projects/{project}/zones/{zone}/operations/{operation_name_tail}",
	},

	// Cloud Run (REST). Operation names look like
	// "projects/<p>/locations/<l>/operations/<op>".
	"projects/*/locations/*/operations/": {
		Pkg:       "run/v2",
		Resource:  "Operations",
		Transport: TransportREST,
		Host:      "run.googleapis.com",
		Path:      "/v2/{operation_name}",
	},

	// Pub/Sub historically returned grpc operations; the SDK package wraps it.
	// Pub/Sub topic-level operations route through the standard
	// google.longrunning.Operations entry above; no per-service override
	// needed beyond it.

	// Artifact Registry (REST). Same project/location/operations shape, but
	// served by artifactregistry.googleapis.com — bind to a different host
	// than Cloud Run despite the matching prefix. Disambiguation in v0.1.0
	// is by upstream host fallback (the projects/*/locations/*/operations/
	// entry above is the default; a session that just hit
	// artifactregistry.googleapis.com falls through to the host fallback).

	// Plain "global/operations/" (Compute legacy — no project prefix). Some
	// older Compute LROs return un-projected names; route to the same handler.
	"global/operations/": {
		Pkg:       "compute/v1",
		Resource:  "GlobalOperations",
		Transport: TransportREST,
		Host:      "compute.googleapis.com",
		Path:      "/compute/v1/{operation_name}",
	},
}

// Lookup returns the most specific matching endpoint for operationName and
// the prefix key that won the match. ok=false signals "no entry; caller
// should run the §5.7 fallback templates".
func Lookup(operationName string) (Endpoint, string, bool) {
	bestKey := ""
	bestScore := -1
	for prefix := range ServiceByPrefix {
		if matched, score := matchPrefix(prefix, operationName); matched && score > bestScore {
			bestScore = score
			bestKey = prefix
		}
	}
	if bestKey == "" {
		return Endpoint{}, "", false
	}
	return ServiceByPrefix[bestKey], bestKey, true
}

// matchPrefix returns (matched, score). Score is the number of literal
// non-wildcard segments matched, biasing the lookup toward the most-specific
// pattern when multiple match.
func matchPrefix(prefix, name string) (bool, int) {
	psegs := splitPath(prefix)
	nsegs := splitPath(name)
	// prefix must have <= len(name) leading segments to match
	if len(psegs) > len(nsegs) {
		return false, 0
	}
	score := 0
	for i, ps := range psegs {
		if ps == "*" {
			continue
		}
		// empty trailing segment (caused by prefix ending in "/") matches
		// anything — that's how "operations/" matches "operations/foo".
		if ps == "" {
			continue
		}
		if nsegs[i] != ps {
			return false, 0
		}
		score++
	}
	return true, score
}

func splitPath(s string) []string {
	return strings.Split(s, "/")
}

// SubstitutePath renders ep.Path against operationName, expanding the
// templated placeholders. Returns the substituted path. If the path is
// empty (gRPC endpoint), returns "".
func SubstitutePath(ep Endpoint, operationName string) string {
	if ep.Path == "" {
		return ""
	}
	path := ep.Path
	segs := splitPath(operationName)

	// {operation_name} — full
	path = strings.ReplaceAll(path, "{operation_name}", operationName)
	// {operation_name_tail} — last segment
	if len(segs) > 0 {
		path = strings.ReplaceAll(path, "{operation_name_tail}", segs[len(segs)-1])
	}
	// Position-based variable extraction: scan operation name for known
	// keywords (projects, zones, regions, locations) and pick the next segment.
	for i := 0; i+1 < len(segs); i++ {
		switch segs[i] {
		case "projects":
			path = strings.ReplaceAll(path, "{project}", segs[i+1])
		case "zones":
			path = strings.ReplaceAll(path, "{zone}", segs[i+1])
		case "regions":
			path = strings.ReplaceAll(path, "{region}", segs[i+1])
		case "locations":
			path = strings.ReplaceAll(path, "{location}", segs[i+1])
		}
	}
	return path
}

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/ehmo/gum/internal/catalog"
)

// Spec §5.8 makes `capabilities[]` a required per-variant declaration. This
// pass writes it from data the catalog already carries, so it needs no
// Discovery refetch and a rerun on an unchanged catalog changes nothing.
//
// Derivation covers the six atoms the request record proves. `lro_return` is
// stamped by the Discovery enrichment pass and is carried through untouched,
// as is any atom a curated entry below owns.

// derivedCapabilities are the atoms applyCapabilities computes. An atom outside
// this set that a variant already declares survives the pass.
var derivedCapabilities = []string{
	catalog.CapabilityPathParams,
	catalog.CapabilityQueryParams,
	catalog.CapabilityJSONRequest,
	catalog.CapabilityJSONResponse,
	catalog.CapabilityPagination,
	catalog.CapabilityFieldMask,
}

// curatedCapability replaces derivation for one variant. It exists for the
// variants whose real capability set contradicts the request record: a
// response the generic dispatcher cannot shape, or an executor outside generic
// dispatch entirely.
type curatedCapability struct {
	capabilities     []string
	unsupported      []string
	executionSupport catalog.ExecutionSupport
	why              string
}

// curatedVariantCapabilities is keyed by variant_id.
var curatedVariantCapabilities = map[string]curatedCapability{
	"drive.v3.rest.files.export": {
		capabilities:     []string{catalog.CapabilityPathParams, catalog.CapabilityQueryParams, catalog.CapabilityMediaDownload},
		unsupported:      []string{catalog.CapabilityMediaDownload},
		executionSupport: catalog.ExecutionSupportPartial,
		why: "Files.Export returns the exported bytes, never a JSON resource. " +
			"The typed-rest-sdk adapter stamps Format \"json\" on every response, " +
			"so the body reaches shaping mislabelled. The request half executes.",
	},
	"drive.v3.rest.files.get": {
		capabilities: []string{
			catalog.CapabilityPathParams,
			catalog.CapabilityQueryParams,
			catalog.CapabilityJSONResponse,
			catalog.CapabilityFieldMask,
			catalog.CapabilityMediaDownload,
		},
		unsupported:      []string{catalog.CapabilityMediaDownload},
		executionSupport: catalog.ExecutionSupportPartial,
		why: "Files.Get returns JSON metadata by default and file bytes under " +
			"alt=media, which the catalog exposes as a request field. The " +
			"metadata half executes; the download half does not.",
	},
	"gum.code.v1.risor": {
		capabilities: []string{catalog.CapabilityCodeExecution},
		why:          "The code.risor typed executor runs the script in-process. No generic atom applies.",
	},
}

// deriveCapabilities computes a variant's atoms from the op's request record
// and the variant's HTTP binding.
func deriveCapabilities(op *catalog.Op, v *catalog.Variant) []string {
	var atoms []string
	add := func(atom string) {
		if !slices.Contains(atoms, atom) {
			atoms = append(atoms, atom)
		}
	}

	http := (*catalog.HTTPBinding)(nil)
	if v.Binding != nil {
		http = v.Binding.HTTP
	}

	if http != nil && strings.Contains(http.Path, "{") {
		add(catalog.CapabilityPathParams)
	}

	for _, f := range op.RequestFields {
		switch f.Location {
		case catalog.RequestFieldQuery:
			add(catalog.CapabilityQueryParams)
		case catalog.RequestFieldBody, catalog.RequestFieldArg:
			// An `arg` field is the plugin and Ads path: the executor receives
			// a JSON arguments object, the same shape a body carries.
			add(catalog.CapabilityJSONRequest)
		}
	}

	// A DELETE answers 204 with an empty body. Every other method in the
	// catalog answers with a JSON resource, and a variant with no HTTP binding
	// is a plugin or meta op whose executor returns JSON.
	if http == nil || !strings.EqualFold(http.Method, "DELETE") {
		add(catalog.CapabilityJSONResponse)
	}

	for _, f := range op.RequestFields {
		if f.Name == "pageToken" {
			add(catalog.CapabilityPagination)
		}
		if f.Name == "fields" {
			add(catalog.CapabilityFieldMask)
		}
	}
	if v.DefaultFields != "" {
		add(catalog.CapabilityFieldMask)
	}

	// Carry through atoms this pass cannot infer, such as the `lro_return`
	// stamp the Discovery enrichment writes.
	for _, atom := range v.Capabilities {
		if !slices.Contains(derivedCapabilities, atom) {
			add(atom)
		}
	}

	slices.Sort(atoms)
	return atoms
}

// applyCapabilities writes capabilities[], and the execution_support binding
// where a curated entry declares one, onto every variant, then rewrites
// catalog.json and its .sha256 in lockstep.
func applyCapabilities(catalogPath string) error {
	data, err := os.ReadFile(catalogPath)
	if err != nil {
		return fmt.Errorf("apply-capabilities: read %s: %w", catalogPath, err)
	}
	var cat catalog.Catalog
	if err := json.Unmarshal(data, &cat); err != nil {
		return fmt.Errorf("apply-capabilities: parse %s: %w", catalogPath, err)
	}

	seen := map[string]bool{}
	variants, curated := 0, 0
	for i := range cat.Ops {
		op := &cat.Ops[i]
		for j := range op.Variants {
			v := &op.Variants[j]
			if entry, ok := curatedVariantCapabilities[v.VariantID]; ok {
				seen[v.VariantID] = true
				curated++
				v.Capabilities = slices.Clone(entry.capabilities)
				v.UnsupportedCapabilities = slices.Clone(entry.unsupported)
				v.ExecutionSupport = entry.executionSupport
				continue
			}
			v.Capabilities = deriveCapabilities(op, v)
			variants++
		}
	}

	var missing []string
	for id := range curatedVariantCapabilities {
		if !seen[id] {
			missing = append(missing, id)
		}
	}
	slices.Sort(missing)
	if len(missing) > 0 {
		return fmt.Errorf("apply-capabilities: curated variant(s) not in %s: %v", catalogPath, missing)
	}

	if err := validateGeneratedCatalog(&cat); err != nil {
		return fmt.Errorf("apply-capabilities: validate catalog: %w", err)
	}
	if err := writeCatalogWithChecksum(catalogPath, &cat); err != nil {
		return fmt.Errorf("apply-capabilities: %w", err)
	}

	fmt.Fprintf(os.Stderr, "gen-catalog: derived capabilities for %d variant(s), applied %d curated entr(ies)\n",
		variants, curated)
	return nil
}

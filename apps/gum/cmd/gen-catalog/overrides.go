package main

// The overrides manifest is the curator-editable source-metadata file spec
// §5.2 names. It is embedded so every `go run ./cmd/gen-catalog` sees the
// file as committed, independent of the working directory, and validated
// against overrides.schema.json before any generation path runs.

import (
	"encoding/json"
	"fmt"
	"sync"

	_ "embed"

	"github.com/google/jsonschema-go/jsonschema"
	toml "github.com/pelletier/go-toml/v2"
)

//go:embed overrides.toml
var overridesTOML []byte

//go:embed overrides.schema.json
var overridesSchemaJSON []byte

// overridesManifest is the decoded, schema-valid overrides.toml.
type overridesManifest struct {
	APIs          map[string]apiOverride     `json:"apis"`
	SDKOnly       map[string]sdkOverride     `json:"sdk_only"`
	GRPCPreferred map[string]bindingOverride `json:"grpc_preferred"`
	OpIDOverrides map[string]string          `json:"op_id_overrides"`
	RiskOverrides map[string]string          `json:"risk_overrides"`
}

type apiOverride struct {
	DiscoveryURL string `json:"discovery_url"`
}

type sdkOverride struct {
	GoPkg      string `json:"go_pkg"`
	GoCall     string `json:"go_call"`
	AdapterKey string `json:"adapter_key"`
}

type bindingOverride struct {
	GoPkg  string `json:"go_pkg"`
	GoCall string `json:"go_call"`
}

// validateOverrides decodes a TOML manifest, validates it against the
// embedded JSON Schema, and returns the JSON re-encoding of the manifest.
// Every failure carries the spec §5.2 code OVERRIDES_SCHEMA_INVALID.
func validateOverrides(manifest, schemaJSON []byte) ([]byte, error) {
	var tree map[string]any
	if err := toml.Unmarshal(manifest, &tree); err != nil {
		return nil, fmt.Errorf("OVERRIDES_SCHEMA_INVALID: overrides.toml does not parse: %w", err)
	}

	encoded, err := json.Marshal(tree)
	if err != nil {
		return nil, fmt.Errorf("OVERRIDES_SCHEMA_INVALID: overrides.toml does not encode as JSON: %w", err)
	}

	// Round-trip through encoding/json so the validator sees JSON-native
	// values (float64 numbers), not go-toml's int64/time.Time decodings.
	var doc any
	if err := json.Unmarshal(encoded, &doc); err != nil {
		return nil, fmt.Errorf("OVERRIDES_SCHEMA_INVALID: re-decode manifest JSON: %w", err)
	}

	var schema jsonschema.Schema
	if err := json.Unmarshal(schemaJSON, &schema); err != nil {
		return nil, fmt.Errorf("OVERRIDES_SCHEMA_INVALID: overrides.schema.json does not parse: %w", err)
	}
	resolved, err := schema.Resolve(nil)
	if err != nil {
		return nil, fmt.Errorf("OVERRIDES_SCHEMA_INVALID: overrides.schema.json does not compile: %w", err)
	}

	if err := resolved.Validate(doc); err != nil {
		return nil, fmt.Errorf("OVERRIDES_SCHEMA_INVALID: overrides.toml violates overrides.schema.json: %w", err)
	}
	return encoded, nil
}

var (
	overridesOnce   sync.Once
	overridesLoaded *overridesManifest
	overridesErr    error
)

// loadOverrides validates and decodes the embedded manifest once per process.
// run() calls it before any generation mode, so a broken manifest stops the
// generator before a single variant is evaluated.
func loadOverrides() (*overridesManifest, error) {
	overridesOnce.Do(func() {
		encoded, err := validateOverrides(overridesTOML, overridesSchemaJSON)
		if err != nil {
			overridesErr = err
			return
		}
		var m overridesManifest
		if err := json.Unmarshal(encoded, &m); err != nil {
			overridesErr = fmt.Errorf("OVERRIDES_SCHEMA_INVALID: decode manifest into Go: %w", err)
			return
		}
		overridesLoaded = &m
	})
	return overridesLoaded, overridesErr
}

// discoveryURLFor maps a catalog op's service to its Discovery document, as
// declared under [apis.<service>] in overrides.toml.
func discoveryURLFor(service string) (string, bool) {
	m, err := loadOverrides()
	if err != nil {
		// run() surfaces the validation error before any caller gets here;
		// reporting "no URL" keeps the enrichment paths total regardless.
		return "", false
	}
	api, ok := m.APIs[service]
	return api.DiscoveryURL, ok
}

package profile

import (
	"encoding/json"
	"fmt"
	"sync"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/ehmo/gum/internal/embedded"
)

var (
	dslSchemaOnce     sync.Once
	dslSchemaResolved *jsonschema.Resolved
	dslSchemaErr      error
)

// compileDSLSchema lazily compiles the embedded expression-profile DSL schema.
func compileDSLSchema() (*jsonschema.Resolved, error) {
	dslSchemaOnce.Do(func() {
		var s jsonschema.Schema
		if err := json.Unmarshal(embedded.ExpressionProfileDSLJSON, &s); err != nil {
			dslSchemaErr = fmt.Errorf("profile: parse embedded DSL schema: %w", err)
			return
		}
		resolved, err := s.Resolve(nil)
		if err != nil {
			dslSchemaErr = fmt.Errorf("profile: compile embedded DSL schema: %w", err)
			return
		}
		dslSchemaResolved = resolved
	})
	return dslSchemaResolved, dslSchemaErr
}

// ValidateRawProfileFile validates a serialized expression-profile file
// against docs/expression-profile-dsl.json. The schema is embedded into the
// binary via go:embed so the function works without a filesystem path.
//
// Returns nil for a valid profile, a descriptive error otherwise.
func ValidateRawProfileFile(raw []byte) error {
	var doc any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return fmt.Errorf("profile: invalid JSON: %w", err)
	}
	rs, err := compileDSLSchema()
	if err != nil {
		return err
	}
	if err := rs.Validate(doc); err != nil {
		return fmt.Errorf("profile: schema validation failed: %w", err)
	}
	return nil
}

package plugins

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"sort"
	"strings"
	"sync"

	"github.com/ehmo/gum/internal/embedded"
	"github.com/ehmo/gum/internal/output/jcs"
)

// FirstPartySchemaOwner is the OwnerPlugin recorded on an embedded schema.
// It is not a plugin id: no plugin can own a first-party ref, so a collision
// message naming it tells the operator the conflict is with the gum binary
// itself and cannot be resolved by uninstalling something.
const FirstPartySchemaOwner = "gum (first-party)"

// firstPartySchemaSuffix is the store's file extension.
const firstPartySchemaSuffix = ".json"

var (
	firstPartyOnce sync.Once
	firstPartyRefs []SchemaRef
)

// FirstPartySchemaRefs returns the embedded first-party schema store as a
// SchemaRef inventory, hashed the same way plugin schemas are: JCS-canonical
// SHA-256, hex-encoded.
//
// The store is baked into the binary, so the walk runs once per process.
// A file that does not parse as JSON is skipped rather than fatal: it cannot
// be served either, and refusing every plugin install over one bad embedded
// file would be a worse failure than letting the ref go unclaimed.
func FirstPartySchemaRefs() []SchemaRef {
	firstPartyOnce.Do(func() {
		_ = fs.WalkDir(embedded.SchemaFS, ".", func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			name := d.Name()
			if !strings.HasSuffix(name, firstPartySchemaSuffix) {
				return nil
			}

			raw, readErr := embedded.SchemaFS.ReadFile(path)
			if readErr != nil {
				return nil
			}
			var tree any
			if json.Unmarshal(raw, &tree) != nil {
				return nil
			}
			body, jcsErr := jcs.Marshal(tree)
			if jcsErr != nil {
				return nil
			}

			sum := sha256.Sum256(body)
			firstPartyRefs = append(firstPartyRefs, SchemaRef{
				Ref:         strings.TrimSuffix(name, firstPartySchemaSuffix),
				Hash:        hex.EncodeToString(sum[:]),
				OwnerPlugin: FirstPartySchemaOwner,
			})
			return nil
		})

		sort.Slice(firstPartyRefs, func(i, j int) bool {
			return firstPartyRefs[i].Ref < firstPartyRefs[j].Ref
		})
	})

	out := make([]SchemaRef, len(firstPartyRefs))
	copy(out, firstPartyRefs)
	return out
}

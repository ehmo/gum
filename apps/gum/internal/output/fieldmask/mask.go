package fieldmask

import (
	"sort"
	"strings"
)

// node is a single field in the parsed mask tree.
type node struct {
	name     string  // field name or "*"
	wildcard bool    // true iff name == "*"
	children []*node // non-nil means this field has a sub-selection
}

// Mask is the parsed, opaque representation of a Google-style field mask.
type Mask struct {
	roots []*node
}

// Has reports whether the mask selects the field at the given path.
// A wildcard node (*) matches any single segment and also selects all
// deeper paths beneath it.
func (m *Mask) Has(path ...string) bool {
	if len(path) == 0 {
		return false
	}
	return hasInNodes(m.roots, path)
}

func hasInNodes(nodes []*node, path []string) bool {
	seg, rest := path[0], path[1:]
	for _, n := range nodes {
		if !n.wildcard && n.name != seg {
			continue
		}
		// This node matches the current segment (either by name or wildcard).
		if len(rest) == 0 || n.wildcard {
			return true
		}
		if len(n.children) > 0 && hasInNodes(n.children, rest) {
			return true
		}
	}
	return false
}

// Project applies the mask to src and returns a new map containing only the
// selected fields. Array values are projected element-wise. Missing source
// fields are silently omitted.
func (m *Mask) Project(src map[string]any) map[string]any {
	return projectMap(m.roots, src)
}

// projectMap copies only the node-selected keys from src into a new map.
func projectMap(nodes []*node, src map[string]any) map[string]any {
	out := make(map[string]any)
	for _, n := range nodes {
		if n.wildcard {
			// Wildcard: include all keys from the source unchanged.
			for k, v := range src {
				out[k] = v
			}
			return out
		}
		val, ok := src[n.name]
		if !ok {
			continue
		}
		if len(n.children) == 0 {
			out[n.name] = val // leaf: include as-is
		} else {
			out[n.name] = projectValue(n.children, val)
		}
	}
	return out
}

// projectArray applies children to each element of arr, silently dropping
// elements that are not map[string]any.
func projectArray(children []*node, arr []any) []any {
	out := make([]any, 0, len(arr))
	for _, elem := range arr {
		if m, ok := elem.(map[string]any); ok {
			out = append(out, projectMap(children, m))
		}
		// Non-map elements under a nested mask are dropped silently.
	}
	return out
}

// projectValue dispatches to projectMap or projectArray based on val's type.
// A scalar where the mask expects nesting is dropped (returns nil).
func projectValue(children []*node, val any) any {
	switch v := val.(type) {
	case map[string]any:
		return projectMap(children, v)
	case []any:
		return projectArray(children, v)
	default:
		return nil
	}
}

// String returns the canonical serialization of the mask.
// Children are sorted alphabetically with wildcards last — this ordering
// is intentional so that round-trip Parse(m.String()) always produces the
// same byte sequence regardless of the original parse order.
func (m *Mask) String() string {
	return serializeNodes(m.roots)
}

func serializeNodes(nodes []*node) string {
	sorted := make([]*node, len(nodes))
	copy(sorted, nodes)
	sort.Slice(sorted, func(i, j int) bool {
		a, b := sorted[i], sorted[j]
		if a.wildcard != b.wildcard {
			return b.wildcard // wildcards sort last
		}
		return a.name < b.name
	})

	parts := make([]string, len(sorted))
	for i, n := range sorted {
		if n.wildcard {
			parts[i] = "*"
		} else if len(n.children) == 0 {
			parts[i] = n.name
		} else {
			parts[i] = n.name + "(" + serializeNodes(n.children) + ")"
		}
	}
	return strings.Join(parts, ",")
}

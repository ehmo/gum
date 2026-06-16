package profile

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/ehmo/gum/internal/output/toon"
)

// ApplyInput carries the raw executor response body and the user-requested output
// format (from Invocation.Format).
type ApplyInput struct {
	// Body is the raw JSON response body from the executor.
	Body []byte

	// UserFormat is the format requested by the caller: "toon", "json", "raw", or "".
	// When non-empty it overrides Profile.DefaultFormat.
	UserFormat string
}

// ApplyOutput is the result of applying an expression profile to a response body.
type ApplyOutput struct {
	// Body is the shaped response in the chosen format.
	Body []byte

	// Format is the effective format used ("toon", "json", or "raw").
	Format string

	// ProfileApplied is true when at least one profile rule transformed the body.
	ProfileApplied bool

	// BytesIn is len(ApplyInput.Body).
	BytesIn int

	// BytesOut is len(Body).
	BytesOut int
}

// Apply applies profile p to in and returns the shaped output.
// The effective format is determined by: UserFormat > Profile.DefaultFormat > "toon".
// An error is returned if Body is not valid JSON or if profile rules cannot be applied.
func Apply(p *Profile, in ApplyInput) (ApplyOutput, error) {
	bytesIn := len(in.Body)

	// Raw bypass: return as-is.
	if in.UserFormat == "raw" {
		return ApplyOutput{
			Body:           in.Body,
			Format:         "raw",
			ProfileApplied: false,
			BytesIn:        bytesIn,
			BytesOut:       bytesIn,
		}, nil
	}

	// A 204 No Content / empty body — common for successful delete and some
	// write/update ops — is not a JSON document. Treat it as an empty success
	// ({}) rather than erroring with "parse JSON", so a successful destructive op
	// doesn't surface a spurious error to the caller.
	if len(strings.TrimSpace(string(in.Body))) == 0 {
		return ApplyOutput{
			Body:           []byte("{}"),
			Format:         "json",
			ProfileApplied: false,
			BytesIn:        bytesIn,
			BytesOut:       2,
		}, nil
	}

	// Parse body as JSON.
	var v any
	if err := json.Unmarshal(in.Body, &v); err != nil {
		return ApplyOutput{}, fmt.Errorf("profile apply: parse JSON: %w", err)
	}

	// $defs / definitions preservation: capture before transforms so projection
	// or strip passes cannot drop schema reference fragments that other parts
	// of the document point at via $ref.
	preservedDefs, preservedKey := captureDefs(v)

	// Apply transforms in order.
	// 1. Projection.
	if len(p.Projection) > 0 {
		v = applyProjection(v, p.Projection)
	}

	// 2a. KeepFields.
	if len(p.KeepFields) > 0 {
		v = applyKeepFields(v, p.KeepFields)
	}

	// 2b. DropFields (runs after KeepFields).
	if len(p.DropFields) > 0 {
		v = applyDropFields(v, p.DropFields)
	}

	// 3. StripNulls.
	if p.StripNulls {
		v = applyStripNulls(v)
	}

	// 4. Flatten (envelope unwrapping).
	if p.Flatten {
		v = applyFlatten(v)
	}

	// 4b. FlattenSingletons (sub-step of 4).
	if p.FlattenSingletons {
		if arr, ok := v.([]any); ok && len(arr) == 1 {
			v = arr[0]
		}
	}

	// 5. CollapseArrays.
	if p.CollapseArrays != nil {
		v = applyCollapseArrays(v, p.CollapseArrays)
	}

	// 6. TruncateStrings.
	if p.TruncateStrings != nil {
		v = applyTruncateStrings(v, p.TruncateStrings, "")
	}

	// 7. Dedupe.
	if p.Dedupe != nil {
		if arr, ok := v.([]any); ok {
			v = applyDedupe(arr, p.Dedupe)
		}
	}

	// SortBy.
	if p.SortBy != "" {
		if arr, ok := v.([]any); ok {
			v = applySortBy(arr, p.SortBy)
		}
	}

	// Limit.
	if p.Limit > 0 {
		if arr, ok := v.([]any); ok && len(arr) > p.Limit {
			v = arr[:p.Limit]
		}
	}

	// Restore $defs / definitions if they were present in the input.
	if preservedDefs != nil {
		if m, ok := v.(map[string]any); ok {
			m[preservedKey] = preservedDefs
			v = m
		}
	}

	// OnEmpty sentinel (post-pipeline): fire when stages 2–7 reduce a non-empty
	// upstream response to an empty body. Spec §9.1 "Empty-output handling".
	if p.OnEmpty != "" {
		switch vt := v.(type) {
		case []any:
			if len(vt) == 0 {
				v = p.OnEmpty
			}
		case map[string]any:
			if len(vt) == 0 {
				v = p.OnEmpty
			}
		case nil:
			v = p.OnEmpty
		}
	}

	// Determine output format.
	format := in.UserFormat
	if format == "" {
		format = p.DefaultFormat
	}
	if format == "" {
		format = "toon"
	}

	// Encode output.
	var outBytes []byte
	var err error
	switch format {
	case "json":
		outBytes, err = json.Marshal(v)
		if err != nil {
			return ApplyOutput{}, fmt.Errorf("profile apply: marshal JSON: %w", err)
		}
	case "toon":
		outBytes, err = toon.EncodeWithOptions(v, toon.EncoderOptions{OmitZeroCounts: p.OmitZeroCounts})
		if err != nil {
			return ApplyOutput{}, fmt.Errorf("profile apply: encode TOON: %w", err)
		}
	default:
		outBytes, err = toon.EncodeWithOptions(v, toon.EncoderOptions{OmitZeroCounts: p.OmitZeroCounts})
		if err != nil {
			return ApplyOutput{}, fmt.Errorf("profile apply: encode TOON: %w", err)
		}
	}

	return ApplyOutput{
		Body:           outBytes,
		Format:         format,
		ProfileApplied: true,
		BytesIn:        bytesIn,
		BytesOut:       len(outBytes),
	}, nil
}

// captureDefs returns the top-level $defs (preferred) or definitions section
// from v if v is a JSON object, plus the key it was stored under. Returns
// (nil, "") when neither key is present.
func captureDefs(v any) (any, string) {
	m, ok := v.(map[string]any)
	if !ok {
		return nil, ""
	}
	for _, key := range []string{"$defs", "definitions"} {
		if defs, ok := m[key]; ok {
			// Deep-copy the fragment: later transforms mutate maps IN PLACE
			// (applyStripNulls deletes null-like keys via delete(vt, key)). Without
			// a copy, stripping would corrupt the very $defs we must preserve
			// verbatim for $ref resolution before the restore writes it back.
			return deepCopyJSON(defs), key
		}
	}
	return nil, ""
}

// deepCopyJSON returns a structural copy of a json.Unmarshal-shaped value
// (map[string]any / []any / scalars) so the caller holds a snapshot immune to
// later in-place mutation. Scalars are immutable and returned as-is.
func deepCopyJSON(v any) any {
	switch vt := v.(type) {
	case map[string]any:
		cp := make(map[string]any, len(vt))
		for k, val := range vt {
			cp[k] = deepCopyJSON(val)
		}
		return cp
	case []any:
		cp := make([]any, len(vt))
		for i, val := range vt {
			cp[i] = deepCopyJSON(val)
		}
		return cp
	default:
		return v
	}
}

// applyProjection keeps only specified keys in maps. Works on map[string]any and []any of maps.
func applyProjection(v any, fields []string) any {
	fieldSet := make(map[string]bool, len(fields))
	for _, f := range fields {
		fieldSet[f] = true
	}

	switch vt := v.(type) {
	case map[string]any:
		result := make(map[string]any, len(fields))
		for _, f := range fields {
			if val, ok := vt[f]; ok {
				result[f] = val
			}
		}
		return result
	case []any:
		result := make([]any, len(vt))
		for i, elem := range vt {
			if m, ok := elem.(map[string]any); ok {
				projected := make(map[string]any, len(fields))
				for _, f := range fields {
					if val, ok := m[f]; ok {
						projected[f] = val
					}
				}
				result[i] = projected
			} else {
				result[i] = elem
			}
		}
		return result
	default:
		return v
	}
}

// applySortBy sorts an array of maps by the given field key (ascending).
func applySortBy(arr []any, key string) []any {
	result := make([]any, len(arr))
	copy(result, arr)
	sort.SliceStable(result, func(i, j int) bool {
		mi, oki := result[i].(map[string]any)
		mj, okj := result[j].(map[string]any)
		if !oki || !okj {
			return false
		}
		vi := mi[key]
		vj := mj[key]
		return compareValues(vi, vj) < 0
	})
	return result
}

// compareValues compares two values for sorting. Returns -1, 0, or 1.
func compareValues(a, b any) int {
	// Handle nil.
	if a == nil && b == nil {
		return 0
	}
	if a == nil {
		return -1
	}
	if b == nil {
		return 1
	}

	// Numeric comparison.
	fa, aIsNum := toFloat(a)
	fb, bIsNum := toFloat(b)
	if aIsNum && bIsNum {
		if fa < fb {
			return -1
		}
		if fa > fb {
			return 1
		}
		return 0
	}

	// String comparison.
	sa, aIsStr := a.(string)
	sb, bIsStr := b.(string)
	if aIsStr && bIsStr {
		if sa < sb {
			return -1
		}
		if sa > sb {
			return 1
		}
		return 0
	}

	// Fallback: marshal to JSON and compare strings.
	ja, _ := json.Marshal(a)
	jb, _ := json.Marshal(b)
	sa = string(ja)
	sb = string(jb)
	if sa < sb {
		return -1
	}
	if sa > sb {
		return 1
	}
	return 0
}

// toFloat converts a numeric value to float64.
func toFloat(v any) (float64, bool) {
	switch vt := v.(type) {
	case float64:
		return vt, true
	case int:
		return float64(vt), true
	case int64:
		return float64(vt), true
	case int32:
		return float64(vt), true
	default:
		return 0, false
	}
}

// classifyDotPaths examines dot-path paths against key (one map key segment)
// and returns:
//   - directMatch=true when an entry in paths equals key exactly.
//   - subPaths: the tail segments of all paths whose head segment is key
//     (e.g., path "messages.id" against key "messages" → subPath "id").
//
// directMatch takes priority: when true, subPaths is always nil.
// Spec §9.1 step 2.
func classifyDotPaths(key string, paths []string) (directMatch bool, subPaths []string) {
	prefix := key + "."
	for _, path := range paths {
		if path == key {
			return true, nil
		}
		if strings.HasPrefix(path, prefix) {
			subPaths = append(subPaths, path[len(prefix):])
		}
	}
	return false, subPaths
}

// applyKeepFields recursively retains only keys that are in the allowlist or
// are prefix ancestors of dot-paths in the allowlist.
// paths is the set of allowed dot-paths at the current level.
// Spec §9.1 step 2 (keep_fields).
func applyKeepFields(v any, paths []string) any {
	switch vt := v.(type) {
	case map[string]any:
		result := make(map[string]any)
		for key, val := range vt {
			directMatch, subPaths := classifyDotPaths(key, paths)
			if directMatch {
				result[key] = val
			} else if len(subPaths) > 0 {
				result[key] = applyKeepFields(val, subPaths)
			}
			// Otherwise: key not in any path → drop.
		}
		return result
	case []any:
		result := make([]any, len(vt))
		for i, elem := range vt {
			result[i] = applyKeepFields(elem, paths)
		}
		return result
	default:
		return v
	}
}

// applyDropFields recursively removes keys whose dot-path is in the denylist.
// Applied after applyKeepFields. Spec §9.1 step 2 (drop_fields).
func applyDropFields(v any, paths []string) any {
	switch vt := v.(type) {
	case map[string]any:
		result := make(map[string]any)
		for key, val := range vt {
			directMatch, subPaths := classifyDotPaths(key, paths)
			if directMatch {
				continue // drop this key entirely
			}
			if len(subPaths) > 0 {
				result[key] = applyDropFields(val, subPaths)
			} else {
				result[key] = val
			}
		}
		return result
	case []any:
		result := make([]any, len(vt))
		for i, elem := range vt {
			result[i] = applyDropFields(elem, paths)
		}
		return result
	default:
		return v
	}
}

// applyStripNulls recursively removes nil, empty-string, empty-object, and
// empty-array values from maps. Spec §9.1 step 3.
//
// Children are recursed first so that an object that becomes empty after its
// own children are stripped is also removed (post-recursion null-like check).
func applyStripNulls(v any) any {
	switch vt := v.(type) {
	case map[string]any:
		for key, val := range vt {
			if isNullLike(val) {
				delete(vt, key)
			} else {
				stripped := applyStripNulls(val)
				if isNullLike(stripped) {
					delete(vt, key)
				} else {
					vt[key] = stripped
				}
			}
		}
		return vt
	case []any:
		result := make([]any, len(vt))
		for i, elem := range vt {
			result[i] = applyStripNulls(elem)
		}
		return result
	default:
		return v
	}
}

// isNullLike reports whether v is a null-like value: nil, empty string,
// empty object, or empty array. These are the four kinds removed by strip_nulls.
// Spec §9.1 step 3.
func isNullLike(v any) bool {
	if v == nil {
		return true
	}
	switch vt := v.(type) {
	case string:
		return vt == ""
	case map[string]any:
		return len(vt) == 0
	case []any:
		return len(vt) == 0
	}
	return false
}

// envelopeKeys is the ordered list of well-known single-key envelope field names
// that applyFlatten unwraps. Spec §9.1 step 4: "unwrap common envelopes such as
// {data:[...]}, {items:[...]}, or provider-specific configured wrappers."
//
// NOTE: provider-specific configured wrappers (e.g., per-profile override) are
// not yet implemented; this list covers the statically-known common REST shapes.
// When profile-level envelope configuration is added, this list becomes the fallback.
var envelopeKeys = []string{"items", "data", "results", "value"}

// applyFlatten unwraps a single-key envelope map whose value is an array.
// Only maps with exactly one key are candidates; multi-key envelopes are left as-is.
// Spec §9.1 step 4.
func applyFlatten(v any) any {
	m, ok := v.(map[string]any)
	if !ok || len(m) != 1 {
		return v
	}
	for _, key := range envelopeKeys {
		if inner, ok := m[key]; ok {
			if arr, ok := inner.([]any); ok {
				return arr
			}
		}
	}
	return v
}

// applyCollapseArrays truncates arrays to spec.MaxItems and records the number
// of omitted elements. When the top-level value is an array it is wrapped in
// {"items":[...],"omitted_count":N}; when it is a map, each array-valued field
// is truncated in-place and a <key>_omitted_count sibling field is added.
// Spec §9.1 step 5.
func applyCollapseArrays(v any, spec *CollapseArraysSpec) any {
	switch vt := v.(type) {
	case []any:
		if len(vt) > spec.MaxItems {
			original := len(vt)
			truncated := vt[:spec.MaxItems]
			return map[string]any{
				"items":         truncated,
				"omitted_count": original - spec.MaxItems,
			}
		}
		return v
	case map[string]any:
		for key, val := range vt {
			if arr, ok := val.([]any); ok && len(arr) > spec.MaxItems {
				original := len(arr)
				vt[key] = arr[:spec.MaxItems]
				vt[key+"_omitted_count"] = original - spec.MaxItems
			}
		}
		return vt
	default:
		return v
	}
}

// applyTruncateStrings recursively truncates string values to the limits in spec.
// fieldPath is the dot-path prefix for the current recursion level (empty at top level);
// it is used to match per-field overrides by absolute dot path. Spec §9.1 step 6.
func applyTruncateStrings(v any, spec *TruncateStringsSpec, fieldPath string) any {
	switch vt := v.(type) {
	case map[string]any:
		result := make(map[string]any, len(vt))
		for key, val := range vt {
			var childPath string
			if fieldPath == "" {
				childPath = key
			} else {
				childPath = fieldPath + "." + key
			}
			switch sv := val.(type) {
			case string:
				limit := spec.DefaultChars
				// Dot-path (more specific) wins over the bare field name. Checking
				// the bare name first made a "meta.note" override unreachable
				// whenever any same-named "note" key existed at another level.
				if l, ok := spec.Fields[childPath]; ok {
					limit = l
				} else if l, ok := spec.Fields[key]; ok {
					limit = l
				}
				result[key] = truncateString(sv, limit)
			default:
				result[key] = applyTruncateStrings(val, spec, childPath)
			}
		}
		return result
	case []any:
		result := make([]any, len(vt))
		for i, elem := range vt {
			switch sv := elem.(type) {
			case string:
				result[i] = truncateString(sv, spec.DefaultChars)
			default:
				result[i] = applyTruncateStrings(elem, spec, fieldPath)
			}
		}
		return result
	default:
		return v
	}
}

// truncateString truncates s to limit runes and appends "…" if truncated.
// If limit is 0, returns s unchanged.
func truncateString(s string, limit int) string {
	if limit <= 0 {
		return s
	}
	runes := []rune(s)
	if len(runes) <= limit {
		return s
	}
	return string(runes[:limit]) + "…"
}

// applyDedupe removes duplicate rows based on the concatenated key fields.
// First occurrence wins; subsequent rows with the same composite key are dropped.
// Non-map elements are passed through unchanged. Spec §9.1 step 7.
func applyDedupe(arr []any, spec *DedupeSpec) []any {
	seen := make(map[string]bool)
	result := make([]any, 0, len(arr))
	for _, elem := range arr {
		m, ok := elem.(map[string]any)
		if !ok {
			result = append(result, elem)
			continue
		}
		// Build the stable key. JSON-marshal each part so distinct values that
		// fmt.Sprintf("%v") would render identically stay distinct — e.g. a JSON
		// null ("null") vs the string "<nil>" ("\"<nil>\""), and so a value
		// containing the \x00 separator can't be confused (JSON escapes it).
		parts := make([]string, len(spec.By))
		for i, field := range spec.By {
			b, _ := json.Marshal(m[field])
			parts[i] = string(b)
		}
		key := strings.Join(parts, "\x00")
		if !seen[key] {
			seen[key] = true
			result = append(result, elem)
		}
	}
	return result
}

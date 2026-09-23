package profile

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/ehmo/gum/internal/output/render"
	"github.com/ehmo/gum/internal/output/toon"
)

// occurrenceCountKey is the field stage 7 writes on the surviving row of a
// collapsed duplicate group, carrying how many rows shared its key (spec §9.1
// stage 7). It sits beside collapse_arrays' "<field>_omitted_count" siblings:
// both put the count of what went missing in the body that lost it.
const occurrenceCountKey = "occurrence_count"

// ApplyInput carries the raw executor response body and the user-requested output
// format (from Invocation.Format).
type ApplyInput struct {
	// Body is the raw JSON response body from the executor.
	Body []byte

	// UserFormat is the format requested by the caller: "toon", "json", "raw", or "".
	// When non-empty it overrides Profile.DefaultFormat.
	UserFormat string

	// MaxItems, when set, replaces the profile's collapse_arrays.max_items for
	// this one call. The zero value leaves the profile's own rule in force.
	MaxItems MaxItemsOverride

	// Op and Variant fill the op: and variant: headers of the §9.0 TOON
	// document. They are the resolved ids for this invocation, so the caller
	// supplies them; the applier sees only a response body. A caller with no
	// catalog behind it (a meta tool, a fixture) leaves them empty, and the
	// header is then present with an empty value.
	Op      string
	Variant string
}

// MaxItemsMode selects where the collapse_arrays cap comes from for one
// invocation.
type MaxItemsMode int

const (
	// MaxItemsFromProfile leaves the active profile's collapse_arrays rule in
	// force. It is the zero value, so an invocation that says nothing about
	// the cap keeps the shipped behaviour.
	MaxItemsFromProfile MaxItemsMode = iota

	// MaxItemsLimit caps arrays at MaxItemsOverride.Value, whether or not the
	// profile declares a cap of its own.
	MaxItemsLimit

	// MaxItemsUnlimited removes the cap for this invocation.
	MaxItemsUnlimited
)

// MaxItemsOverride is a per-invocation replacement for the active profile's
// collapse_arrays.max_items.
//
// A profile cap keeps an unbounded upstream array out of an LLM context
// window. It is the wrong default for a batch op the caller already bounded:
// a 245-keyword generateKeywordHistoricalMetrics request capped at 100
// returned 100 results, and the only format that returned all 243 was raw,
// which carries no adapter annotations (gum-pmbp).
type MaxItemsOverride struct {
	Mode  MaxItemsMode
	Value int
}

// CollapsedArray records one array that collapse_arrays truncated, so a
// presentation layer can report how many records are missing and name the
// sibling key that holds the count.
type CollapsedArray struct {
	// Field is the response key whose array was truncated. Empty when the
	// whole body was an array and collapse wrapped it in {items, omitted_count}.
	Field string

	// CountKey is the key carrying the omitted count in the shaped body:
	// "<field>_omitted_count", or "omitted_count" for a bare array body.
	CountKey string

	// Kept and Omitted sum to the number of elements the upstream returned.
	Kept    int
	Omitted int
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

	// DroppedPaths lists the response dot-paths removed by the profile's field
	// filters — projection, keep_fields, and drop_fields — in lexical order,
	// deduped, and without array indices (so one entry covers a field dropped
	// from every element of an array).
	//
	// Only a field filter can remove a whole field, so only the three filters
	// report here. The lossy-but-marked stages are excluded on purpose:
	// collapse_arrays writes its own omitted_count sibling, truncate_strings
	// shortens values it keeps, and strip_nulls removes only null-like values.
	//
	// A profile whitelist that omits a field the operation advertises is
	// invisible to the caller otherwise: the response is valid JSON, just
	// smaller (gum-bpx0). Presentation layers name these paths so the caller
	// knows to re-run with --format raw or read the recovery artifact.
	DroppedPaths []string

	// CollapsedArrays lists the arrays collapse_arrays truncated, sorted by
	// field name. Empty when nothing was truncated.
	//
	// The count is already in the body as <field>_omitted_count, but a notice
	// that names only the removed fields points the reader at the smaller
	// problem: dropping 143 of 243 results deserves at least equal billing
	// (gum-pmbp).
	CollapsedArrays []CollapsedArray

	// Shaped is the shaped value as an in-memory tree, before encoding.
	//
	// A presentation layer that hands a caller structured data needs the
	// shaped tree, not the upstream one. MCP structuredContent was built from
	// the raw response body, so a client that read it saw every field the
	// profile had removed and none of the token saving (spec §9.1, §13).
	Shaped any

	// ResultCount is the number of records the shaped body carries.
	// OmittedCount totals the omitted counts the body reports. They feed
	// _expression.result_count and _expression.omitted_count (spec §9.1).
	ResultCount  int
	OmittedCount int

	// OnEmptyMessage is the profile's on_empty string, set only when the
	// shaped body carries an empty record set. Spec §9.1 rule 2 emits it as
	// _expression.on_empty_message.
	OnEmptyMessage string

	// Lossy is true when the profile ran at least one stage that can remove
	// data. It feeds _expression.lossy.
	Lossy bool

	// IntentionalZeroMaxItems is true when the collapse cap in force for this
	// call is 0, so the profile dropped every row on purpose. Spec §9.1
	// discriminator 5 needs it to tell that apart from an upstream empty
	// response.
	IntentionalZeroMaxItems bool

	// DedupedRows and LimitedRows count the rows stage 7 collapsed as
	// duplicates and the rows the profile's limit cut. Neither stage writes a
	// count into the body the way collapse_arrays does, so the notice is the
	// only place the caller learns of them (spec §9.1 shaping notice).
	DedupedRows int
	LimitedRows int
}

// Apply applies profile p to in and returns the shaped output.
// The effective format is determined by: UserFormat > Profile.DefaultFormat > "toon".
// An error is returned if Body is not valid JSON or if profile rules cannot be applied.
func Apply(p *Profile, in ApplyInput) (ApplyOutput, error) {
	bytesIn := len(in.Body)

	// Raw bypass: return as-is. The counts still come from the body, because
	// _expression.result_count is required on every result (spec §9.1 rule 3)
	// and a raw pass-through is the one shape whose record count is exactly
	// what upstream sent. A body that is not JSON leaves them at zero.
	if in.UserFormat == "raw" {
		var raw any
		dec := json.NewDecoder(bytes.NewReader(in.Body))
		dec.UseNumber()
		if err := dec.Decode(&raw); err != nil {
			raw = nil
		}
		count, _, omitted := shapeCounts(raw)
		return ApplyOutput{
			Body:           in.Body,
			Format:         "raw",
			ProfileApplied: false,
			BytesIn:        bytesIn,
			BytesOut:       bytesIn,
			Shaped:         raw,
			ResultCount:    count,
			OmittedCount:   omitted,
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
			Shaped:         map[string]any{},
		}, nil
	}

	// Parse body as JSON. UseNumber keeps every number as its original literal
	// instead of a float64: float64 holds 53 bits of integer, so a Google Ads
	// customer id or a YouTube view count above 2^53 came back out of the
	// pipeline as a different number, and two distinct ids could collide into
	// one dedupe key. json.Number marshals back to the same digits, and the
	// numeric comparators below read it through toFloat.
	dec := json.NewDecoder(bytes.NewReader(in.Body))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return ApplyOutput{}, fmt.Errorf("profile apply: parse JSON: %w", err)
	}

	// $defs / definitions preservation: capture before transforms so projection
	// or strip passes cannot drop schema reference fragments that other parts
	// of the document point at via $ref.
	preservedDefs, preservedKey := captureDefs(v)

	// The upstream record count, taken before any stage runs. Spec §9.1 rule 1
	// defines omitted_count against it, and no transform below leaves it
	// recoverable: each one replaces or mutates v.
	upstreamCount, _, _ := shapeCounts(v)

	// Apply transforms in order.
	dropped := &dropRecorder{}

	// 1. Projection.
	if len(p.Projection) > 0 {
		v = applyProjection(v, p.Projection, dropped, "")
	}

	// 2a. KeepFields.
	if len(p.KeepFields) > 0 {
		v = applyKeepFields(v, p.KeepFields, dropped, "")
	}

	// 2b. DropFields (runs after KeepFields).
	if len(p.DropFields) > 0 {
		v = applyDropFields(v, p.DropFields, dropped, "")
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

	// 5. CollapseArrays. The caller's --max-items / max_items override wins
	// over the profile's own cap (gum-pmbp).
	collapsed := &collapseRecorder{}
	if spec := effectiveCollapse(p.CollapseArrays, in.MaxItems); spec != nil {
		v = applyCollapseArrays(v, spec, collapsed)
	}

	// 6. TruncateStrings.
	if p.TruncateStrings != nil {
		v = applyTruncateStrings(v, p.TruncateStrings, "")
	}

	// 7. Dedupe. The removed-row counts feed the shaping notice: neither this
	// stage nor the limit below writes a count into the body the way
	// collapse_arrays does, so a caller who is not told reads a short result
	// as a complete one.
	dedupedRows := 0
	if p.Dedupe != nil {
		v = applyToRowArray(v, func(arr []any) []any {
			kept, removed := applyDedupe(arr, p.Dedupe)
			dedupedRows += removed
			return kept
		})
	}

	// SortBy.
	if p.SortBy != "" {
		v = applyToRowArray(v, func(arr []any) []any { return applySortBy(arr, p.SortBy) })
	}

	// Limit.
	limitedRows := 0
	if p.Limit > 0 {
		v = applyToRowArray(v, func(arr []any) []any {
			if len(arr) > p.Limit {
				limitedRows += len(arr) - p.Limit
				return arr[:p.Limit]
			}
			return arr
		})
	}

	// Restore $defs / definitions if they were present in the input.
	if preservedDefs != nil {
		if m, ok := v.(map[string]any); ok {
			m[preservedKey] = preservedDefs
			v = m
			// A filter may have dropped the schema section on the way through;
			// it is back in the output, so it is not missing data.
			dropped.forget(preservedKey)
		}
	}

	// Counts for the §9.1 envelope. They read the shaped tree, so they report
	// what the caller receives rather than what upstream sent.
	resultCount, hasRecords, omittedCount := shapeCounts(v)

	// Rows no count key accounts for. Only collapse_arrays writes an
	// omitted_count sibling, so keep_fields, drop_fields, strip_nulls, dedupe
	// and limit each removed rows that the envelope reported as omitted_count
	// 0 — the value §9.1 rule 3 defines as "the upstream API returned zero
	// results". The larger of the two sources wins rather than their sum: a
	// collapsed record array is counted by both, and a collapsed array nested
	// inside a surviving row is counted only by the sibling key.
	if removed := upstreamCount - resultCount; removed > omittedCount {
		omittedCount = removed
	}

	// OnEmpty (post-pipeline): spec §9.1 rule 2 emits the profile's string as
	// _expression.on_empty_message when shaping leaves an empty record set.
	//
	// It does not replace the body. Substituting the sentinel string for the
	// payload destroyed every other field the response carried and left the
	// caller unable to tell a shaped-empty result from a string-valued one.
	// The substitution also fired only when the whole shaped value was empty,
	// which a list response never is: zero rows shape to {"messages": []},
	// an object of length 1.
	//
	// The message is not gated on the upstream having been non-empty. Spec
	// §9.4 requires gum.search_apis to answer a zero-hit query with its
	// on_empty string, and §9.1 discriminator 5 needs the message beside an
	// empty upstream whenever the cap is 0. The pair (result_count,
	// omitted_count) is what tells the caller which of the two happened, which
	// is why omitted_count above counts every stage rather than stage 5 alone.
	onEmptyMessage := ""
	if p.OnEmpty != "" && resultCount == 0 && (hasRecords || isEmptyShape(v)) {
		onEmptyMessage = p.OnEmpty
	}

	// Discriminator 5: the cap in force is 0, so the rows are gone on purpose.
	// The flag is withheld without a message, because §13 makes
	// {intentional_zero_max_items: true, on_empty_message: null} a combination
	// the runtime must never emit.
	zeroMaxItems := false
	if spec := effectiveCollapse(p.CollapseArrays, in.MaxItems); spec != nil && spec.MaxItems == 0 && onEmptyMessage != "" {
		zeroMaxItems = true
	}

	// Determine output format.
	format := in.UserFormat
	if format == "" {
		format = p.DefaultFormat
	}
	if format == "" {
		format = "toon"
	}

	// Encode output (spec §9.1 stage 8).
	//
	// The reported format must be the format the bytes are in. An
	// unimplemented name falls back to TOON and is renamed, because a consumer
	// that trusts the label and parses accordingly gets a parse error on bytes
	// that are valid, just not what the label promised.
	var outBytes []byte
	var err error
	switch format {
	case "json":
		outBytes, err = json.Marshal(v)
		if err != nil {
			return ApplyOutput{}, fmt.Errorf("profile apply: marshal JSON: %w", err)
		}
	case "csv", "markdown":
		var buf bytes.Buffer
		if err = render.Structured(&buf, format, v); err != nil {
			return ApplyOutput{}, fmt.Errorf("profile apply: encode %s: %w", format, err)
		}
		outBytes = buf.Bytes()
	default:
		format = "toon"
		outBytes, err = toon.EncodeDocument(in.Op, in.Variant, v, p.OmitZeroCounts)
		if err != nil {
			return ApplyOutput{}, fmt.Errorf("profile apply: encode TOON: %w", err)
		}
		if outBytes == nil {
			// §9.0: TOON covers uniform arrays of records and "falls back to
			// JSON for nested/non-uniform". The label follows the bytes, so
			// the caller never parses a header block that is not there.
			format = "json"
			outBytes, err = json.Marshal(v)
			if err != nil {
				return ApplyOutput{}, fmt.Errorf("profile apply: marshal JSON: %w", err)
			}
		}
	}

	return ApplyOutput{
		Body:                    outBytes,
		Format:                  format,
		ProfileApplied:          true,
		BytesIn:                 bytesIn,
		BytesOut:                len(outBytes),
		DroppedPaths:            dropped.paths(),
		CollapsedArrays:         collapsed.arrays,
		Shaped:                  v,
		ResultCount:             resultCount,
		OmittedCount:            omittedCount,
		OnEmptyMessage:          onEmptyMessage,
		Lossy:                   isLossy(p, effectiveCollapse(p.CollapseArrays, in.MaxItems)),
		DedupedRows:             dedupedRows,
		LimitedRows:             limitedRows,
		IntentionalZeroMaxItems: zeroMaxItems,
	}, nil
}

// dropRecorder collects the dot-paths a field filter removed from a response.
// A nil recorder is a no-op, so the filter helpers stay callable where the
// accounting is not wanted.
//
// Paths carry no array indices: recursing into a []any keeps the parent prefix,
// so a field dropped from 500 array elements is reported once.
type dropRecorder struct {
	seen map[string]struct{}
}

// record adds prefix+key to the set of dropped paths.
func (r *dropRecorder) record(prefix, key string) {
	if r == nil {
		return
	}
	if r.seen == nil {
		r.seen = make(map[string]struct{})
	}
	r.seen[childPath(prefix, key)] = struct{}{}
}

// forget removes path from the set. Used when a later stage puts a dropped
// section back (the $defs restore).
func (r *dropRecorder) forget(path string) {
	if r == nil || r.seen == nil {
		return
	}
	delete(r.seen, path)
}

// paths returns the recorded dot-paths in lexical order, or nil when nothing
// was dropped. Sorting makes the notice text stable across runs, which matters
// because Go map iteration order is random.
func (r *dropRecorder) paths() []string {
	if r == nil || len(r.seen) == 0 {
		return nil
	}
	out := make([]string, 0, len(r.seen))
	for p := range r.seen {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// childPath joins a dot-path prefix and a map key.
func childPath(prefix, key string) string {
	if prefix == "" {
		return key
	}
	return prefix + "." + key
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
// rec (may be nil) records the keys projection removed, under the dot-path prefix.
func applyProjection(v any, fields []string, rec *dropRecorder, prefix string) any {
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
		recordUnprojected(vt, fieldSet, rec, prefix)
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
				recordUnprojected(m, fieldSet, rec, prefix)
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

// recordUnprojected reports every key of m that the projection field set omits.
func recordUnprojected(m map[string]any, fieldSet map[string]bool, rec *dropRecorder, prefix string) {
	if rec == nil {
		return
	}
	for key := range m {
		if !fieldSet[key] {
			rec.record(prefix, key)
		}
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
	case json.Number:
		// Apply decodes with UseNumber, so every number from an upstream body
		// arrives here. A literal too large for float64 saturates rather than
		// failing, which keeps the sort total.
		f, err := vt.Float64()
		if err != nil {
			return 0, false
		}
		return f, true
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
// rec (may be nil) records each dropped key under the dot-path prefix.
// Spec §9.1 step 2 (keep_fields).
func applyKeepFields(v any, paths []string, rec *dropRecorder, prefix string) any {
	switch vt := v.(type) {
	case map[string]any:
		result := make(map[string]any)
		for key, val := range vt {
			directMatch, subPaths := classifyDotPaths(key, paths)
			if directMatch {
				result[key] = val
			} else if len(subPaths) > 0 {
				result[key] = applyKeepFields(val, subPaths, rec, childPath(prefix, key))
			} else {
				// Key not in any path → drop it, and say so.
				rec.record(prefix, key)
			}
		}
		return result
	case []any:
		// Array elements share their parent's prefix: the report names fields,
		// not positions.
		result := make([]any, len(vt))
		for i, elem := range vt {
			result[i] = applyKeepFields(elem, paths, rec, prefix)
		}
		return result
	default:
		return v
	}
}

// applyDropFields recursively removes keys whose dot-path is in the denylist.
// rec (may be nil) records each removed key under the dot-path prefix.
// Applied after applyKeepFields. Spec §9.1 step 2 (drop_fields).
func applyDropFields(v any, paths []string, rec *dropRecorder, prefix string) any {
	switch vt := v.(type) {
	case map[string]any:
		result := make(map[string]any)
		for key, val := range vt {
			directMatch, subPaths := classifyDotPaths(key, paths)
			if directMatch {
				rec.record(prefix, key)
				continue // drop this key entirely
			}
			if len(subPaths) > 0 {
				result[key] = applyDropFields(val, subPaths, rec, childPath(prefix, key))
			} else {
				result[key] = val
			}
		}
		return result
	case []any:
		result := make([]any, len(vt))
		for i, elem := range vt {
			result[i] = applyDropFields(elem, paths, rec, prefix)
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
//
// rec collects what was truncated so the caller can tell the user how many
// records are missing; a nil recorder is a no-op.
func applyCollapseArrays(v any, spec *CollapseArraysSpec, rec *collapseRecorder) any {
	if spec == nil || spec.MaxItems < 0 {
		// A negative cap has no meaning and used to reach arr[:spec.MaxItems],
		// which panics. Callers validate their own bounds; this is the
		// defence-in-depth arm so a bad profile cannot crash the process.
		return v
	}
	switch vt := v.(type) {
	case []any:
		if len(vt) > spec.MaxItems {
			original := len(vt)
			truncated := vt[:spec.MaxItems]
			rec.record("", "omitted_count", spec.MaxItems, original-spec.MaxItems)
			return map[string]any{
				"items":         truncated,
				"omitted_count": original - spec.MaxItems,
			}
		}
		return v
	case map[string]any:
		// Collect before writing. Adding keys to a map while ranging over it
		// leaves it unspecified whether the new keys are visited, and the
		// sibling counts are keys.
		type collapse struct {
			key      string
			kept     []any
			original int
		}
		var pending []collapse
		for key, val := range vt {
			if arr, ok := val.([]any); ok && len(arr) > spec.MaxItems {
				pending = append(pending, collapse{key: key, kept: arr[:spec.MaxItems], original: len(arr)})
			}
		}
		sort.Slice(pending, func(i, j int) bool { return pending[i].key < pending[j].key })

		for _, c := range pending {
			countKey := c.key + "_omitted_count"
			vt[c.key] = c.kept
			vt[countKey] = c.original - spec.MaxItems
			rec.record(c.key, countKey, spec.MaxItems, c.original-spec.MaxItems)
		}
		return vt
	default:
		return v
	}
}

// effectiveCollapse picks the collapse_arrays rule for one invocation. The
// caller's override wins over the profile's own cap: a profile author bounds
// the default response, but the caller bounds their own request, and a batch
// op whose input names 245 keywords must be able to return 245 results
// (gum-pmbp). Returns nil when no cap applies.
func effectiveCollapse(fromProfile *CollapseArraysSpec, override MaxItemsOverride) *CollapseArraysSpec {
	switch override.Mode {
	case MaxItemsUnlimited:
		return nil
	case MaxItemsLimit:
		return &CollapseArraysSpec{MaxItems: override.Value}
	default:
		return fromProfile
	}
}

// shapeCounts reports the record count of a shaped value, whether it carries a
// record array at all, and the omitted total its count keys declare.
//
// hasRecords separates "the profile returned an empty list" from "the operation
// returned one object". A single-object GET has no record array, so an empty
// result set is not a thing it can report, and on_empty must stay silent for it.
func shapeCounts(v any) (count int, hasRecords bool, omitted int) {
	switch t := v.(type) {
	case []any:
		return len(t), true, 0
	case map[string]any:
		arr := recordArray(t)
		return len(arr), arr != nil, sumOmittedCounts(t)
	}
	return 0, false, 0
}

// isEmptyShape reports whether shaping left nothing at all: a null body, or an
// empty object or array. Spec §9.1 counts "all fields dropped/stripped" as an
// empty output alongside "zero rows after collapse_arrays".
func isEmptyShape(v any) bool {
	switch t := v.(type) {
	case nil:
		return true
	case []any:
		return len(t) == 0
	case map[string]any:
		return len(t) == 0
	}
	return false
}

// collapseRecorder collects the arrays collapse_arrays truncated, in the order
// they were truncated. A nil recorder is a no-op.
type collapseRecorder struct {
	arrays []CollapsedArray
}

func (r *collapseRecorder) record(field, countKey string, kept, omitted int) {
	if r == nil {
		return
	}
	r.arrays = append(r.arrays, CollapsedArray{
		Field:    field,
		CountKey: countKey,
		Kept:     kept,
		Omitted:  omitted,
	})
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
				clamped, cut := truncateString(sv, limit)
				result[key] = clamped
				if cut {
					// docs/profile-dsl-reference.md §2.8: a truncated value
					// is followed by sibling metadata <field>_truncated. An
					// upstream field of that name is overwritten only when
					// this stage actually clamped its sibling, because a
					// stale false next to a clamped value is the one outcome
					// the contract cannot allow.
					result[key+truncatedSuffix] = true
				}
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
				// An array element has no field name, so it gets no sibling
				// flag; the ellipsis is the only signal available here.
				result[i], _ = truncateString(sv, spec.DefaultChars)
			default:
				result[i] = applyTruncateStrings(elem, spec, fieldPath)
			}
		}
		return result
	default:
		return v
	}
}

// truncatedSuffix names the sibling key that marks a clamped string.
const truncatedSuffix = "_truncated"

// truncateString clamps s to limit runes, counting the "…" it appends, and
// reports whether it clamped. A limit of 0 or less is a no-op.
//
// The ellipsis is inside the limit because the limit describes what the
// consumer receives: a profile asking for 180 characters used to get 181, which
// broke any downstream that sized a column or a budget from the same number.
func truncateString(s string, limit int) (string, bool) {
	if limit <= 0 {
		return s, false
	}
	runes := []rune(s)
	if len(runes) <= limit {
		return s, false
	}
	return string(runes[:limit-1]) + "…", true
}

// applyToRowArray runs fn over the record array in v and returns v with that
// array replaced. The record array is the top-level value when it is an array,
// otherwise the field toon.RecordArrayKey names in a top-level object.
//
// The row stages (dedupe, sort_by, limit) used to require a top-level array,
// which made all three dead for real bodies: a Google list response is an
// object ({"messages":[...],"nextPageToken":"..."}) and stage 5 rewrites a
// top-level array into {"items":[...],"omitted_count":N}.
//
// An object with two or more unnamed array-valued fields has no single record
// array, so fn does not run: guessing which array is the records would silently
// reorder or drop rows from the wrong one. The counts in shapeCounts read the
// same key, so the notice and the envelope describe one array, not two.
func applyToRowArray(v any, fn func([]any) []any) any {
	switch vt := v.(type) {
	case []any:
		return fn(vt)
	case map[string]any:
		rowKey := toon.RecordArrayKey(vt)
		if rowKey == "" {
			return v
		}
		vt[rowKey] = fn(vt[rowKey].([]any))
		return vt
	default:
		return v
	}
}

// applyDedupe removes duplicate rows based on the concatenated key fields.
// First occurrence wins; subsequent rows with the same composite key are dropped.
// Non-map elements are passed through unchanged. Spec §9.1 step 7.
//
// The surviving row of a collapsed group carries occurrenceCountKey, the number
// of rows that shared its key. Spec §9.1 stage 7 collapses repeated rows "with
// occurrence counts": without it the caller reads one row and cannot tell that
// forty identical ones stood behind it. A row that already carries the key
// keeps the upstream value, because an annotation that overwrites a field is
// the loss the annotation exists to report. Returns the shaped rows and the
// number of rows removed.
func applyDedupe(arr []any, spec *DedupeSpec) ([]any, int) {
	firstAt := make(map[string]int)
	occurrences := make(map[string]int)
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
		//
		// A row that carries none of the key fields is not keyable. Keying it on
		// all-absent fields gave every such row the same null key, so a profile
		// naming a field the body does not have collapsed the whole result set
		// to one row. Pass it through unkeyed instead.
		parts := make([]string, len(spec.By))
		keyable := false
		for i, field := range spec.By {
			val, present := m[field]
			if present {
				keyable = true
			}
			b, _ := json.Marshal(val)
			parts[i] = string(b)
		}
		if !keyable {
			result = append(result, elem)
			continue
		}
		key := strings.Join(parts, "\x00")
		occurrences[key]++
		if _, seen := firstAt[key]; !seen {
			firstAt[key] = len(result)
			result = append(result, elem)
		}
	}

	for key, n := range occurrences {
		if n < 2 {
			continue
		}
		row, ok := result[firstAt[key]].(map[string]any)
		if !ok {
			continue
		}
		if _, present := row[occurrenceCountKey]; present {
			continue
		}
		row[occurrenceCountKey] = n
	}

	return result, len(arr) - len(result)
}

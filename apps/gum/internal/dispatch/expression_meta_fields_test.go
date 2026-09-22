package dispatch

// Drift guard for ExpressionMeta.Fields (bead gum-3dhe).
//
// gum_parallel serializes per-result metadata through Fields, not through the
// struct's own json.Marshal, because §9.0.1 hoists individual fields out of
// each result. A field added to ExpressionMeta and not added to Fields would
// silently vanish from every batch result, so the projection is checked
// against the struct's JSON tags by reflection rather than by eye.

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// fieldsExcluded lists tags Fields deliberately omits. §13 places
// _code_output_truncated on the enclosing ParallelResultItem, as a sibling of
// _expression, so it is not part of the delta.
var fieldsExcluded = map[string]bool{"_code_output_truncated": true}

// populatedMeta sets every field, so no key is missing merely because its
// zero value is omitted.
func populatedMeta() *ExpressionMeta {
	variant := "gmail.v1.rest.users.messages.list"
	onEmpty := "No messages matched the filter."
	expires := "2026-01-01T00:00:00Z"
	root := "file:///work"
	warning := "implicit_project_root"
	zero := true
	truncated := true
	return &ExpressionMeta{
		Profile:                  "gmail.messages.list.v1",
		OpID:                     "gmail.users.messages.list",
		VariantID:                &variant,
		Lossy:                    true,
		ResultCount:              7,
		OmittedCount:             236,
		OnEmptyMessage:           &onEmpty,
		FullResultPath:           "/tmp/gum/full.json",
		FullResultResource:       "gum://results/abc",
		ArtifactExpiresAt:        &expires,
		IntentionalZeroMaxItems:  &zero,
		ProjectRootURI:           &root,
		ProfileResolutionWarning: &warning,
		CodeOutputTruncated:      &truncated,
		UnsupportedCapabilities:  []string{"media_download"},
	}
}

// TestExpressionMetaFieldsCoversEveryTag fails when a new ExpressionMeta
// field is not projected, which is how the field would disappear from
// gum_parallel results without any test noticing.
func TestExpressionMetaFieldsCoversEveryTag(t *testing.T) {
	got := populatedMeta().Fields()

	rt := reflect.TypeOf(ExpressionMeta{})
	for i := 0; i < rt.NumField(); i++ {
		tag := strings.Split(rt.Field(i).Tag.Get("json"), ",")[0]
		if tag == "" || tag == "-" {
			continue
		}
		if fieldsExcluded[tag] {
			if _, ok := got[tag]; ok {
				t.Errorf("Fields emits %q; §13 puts it on the enclosing ParallelResultItem", tag)
			}
			continue
		}
		if _, ok := got[tag]; !ok {
			t.Errorf("Fields drops %q; add it to ExpressionMeta.Fields", tag)
		}
	}

	for key := range got {
		if fieldsExcluded[key] {
			continue
		}
		if !tagExists(rt, key) {
			t.Errorf("Fields emits %q, which is not an ExpressionMeta json tag", key)
		}
	}
}

// TestExpressionMetaFieldsMatchesJSONMarshal pins the projection to the
// struct's own wire form: Fields must agree with json.Marshal on every key it
// emits, so the two encodings cannot disagree about a value or an omission.
func TestExpressionMetaFieldsMatchesJSONMarshal(t *testing.T) {
	for name, meta := range map[string]*ExpressionMeta{
		"populated": populatedMeta(),
		"zero":      {},
		"nulls":     {Profile: "p", OpID: "op", ResultCount: 3},
	} {
		t.Run(name, func(t *testing.T) {
			raw, err := json.Marshal(meta)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			var want map[string]any
			if err := json.Unmarshal(raw, &want); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}

			got := map[string]any{}
			fieldsRaw, err := json.Marshal(meta.Fields())
			if err != nil {
				t.Fatalf("marshal Fields: %v", err)
			}
			if err := json.Unmarshal(fieldsRaw, &got); err != nil {
				t.Fatalf("unmarshal Fields: %v", err)
			}

			for key, wantVal := range want {
				if fieldsExcluded[key] {
					continue
				}
				gotVal, ok := got[key]
				if !ok {
					t.Errorf("Fields omits %q that json.Marshal emits", key)
					continue
				}
				if !reflect.DeepEqual(gotVal, wantVal) {
					t.Errorf("Fields[%q] = %v; json.Marshal has %v", key, gotVal, wantVal)
				}
			}
			for key := range got {
				if _, ok := want[key]; !ok {
					t.Errorf("Fields emits %q that json.Marshal omits", key)
				}
			}
		})
	}
}

// TestExpressionMetaFieldsNilReceiver pins the degraded path: a caller that
// never shaped a response may call Fields unguarded.
func TestExpressionMetaFieldsNilReceiver(t *testing.T) {
	var m *ExpressionMeta
	if got := m.Fields(); got != nil {
		t.Errorf("(*ExpressionMeta)(nil).Fields() = %v; want nil", got)
	}
}

// tagExists reports whether name is a json tag on ExpressionMeta.
func tagExists(rt reflect.Type, name string) bool {
	for i := 0; i < rt.NumField(); i++ {
		if strings.Split(rt.Field(i).Tag.Get("json"), ",")[0] == name {
			return true
		}
	}
	return false
}

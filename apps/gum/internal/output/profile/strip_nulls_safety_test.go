package profile_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/output/profile"
)

// TestProfileStripNullsSafety covers the seven scenarios from spec §9 +
// docs/catalog-abi.md §55 + docs/expression-profile-dsl.md validation rule §2.
func TestProfileStripNullsSafety(t *testing.T) {
	type tc struct {
		name        string
		stripNulls  bool
		keepFields  []string
		safe        []string
		wantErr     bool
		wantSentinel bool
		wantSubstr  []string
	}

	cases := []tc{
		{
			name:       "strip_nulls_false_passes",
			stripNulls: false,
			keepFields: nil,
			safe:       nil,
			wantErr:    false,
		},
		{
			name:       "safe_star_passes",
			stripNulls: true,
			keepFields: nil,
			safe:       []string{"*"},
			wantErr:    false,
		},
		{
			name:         "nil_safe_list_rejects",
			stripNulls:   true,
			keepFields:   nil,
			safe:         nil,
			wantErr:      true,
			wantSentinel: true,
			wantSubstr:   []string{"PROFILE_STRIP_NULLS_UNSAFE"},
		},
		{
			name:         "empty_safe_list_rejects",
			stripNulls:   true,
			keepFields:   nil,
			safe:         []string{},
			wantErr:      true,
			wantSentinel: true,
			wantSubstr:   []string{"PROFILE_STRIP_NULLS_UNSAFE"},
		},
		{
			name:       "keep_fields_subset_passes",
			stripNulls: true,
			keepFields: []string{"id", "subject"},
			safe:       []string{"id", "subject", "threadId"},
			wantErr:    false,
		},
		{
			name:         "keep_fields_exceeds_safe_rejects",
			stripNulls:   true,
			keepFields:   []string{"id", "payload"},
			safe:         []string{"id"},
			wantErr:      true,
			wantSentinel: true,
			wantSubstr:   []string{"PROFILE_STRIP_NULLS_UNSAFE", "payload"},
		},
		{
			name:         "unbounded_no_keep_fields_rejects",
			stripNulls:   true,
			keepFields:   nil,
			safe:         []string{"id"},
			wantErr:      true,
			wantSentinel: true,
			wantSubstr:   []string{"PROFILE_STRIP_NULLS_UNSAFE"},
		},
	}

	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			p := &profile.Profile{
				StripNulls: c.stripNulls,
				KeepFields: c.keepFields,
			}
			err := profile.ValidateStripNullsSafety(p, c.safe)

			if c.wantErr && err == nil {
				t.Fatalf("expected an error but got nil")
			}
			if !c.wantErr && err != nil {
				t.Fatalf("expected nil error but got: %v", err)
			}
			if c.wantSentinel && !errors.Is(err, profile.ErrProfileStripNullsUnsafe) {
				t.Errorf("expected errors.Is(err, ErrProfileStripNullsUnsafe) to be true; got: %v", err)
			}
			for _, sub := range c.wantSubstr {
				if !strings.Contains(err.Error(), sub) {
					t.Errorf("expected error to contain %q; got: %v", sub, err)
				}
			}
		})
	}
}

// TestCatalogVariantHasNullElisionSafeFields is a compile-pin test that
// verifies the NullElisionSafeFields field exists on catalog.Variant with the
// correct JSON tag (null_elision_safe_fields, omitempty).
func TestCatalogVariantHasNullElisionSafeFields(t *testing.T) {
	// Compile-time check: field must exist and be assignable.
	v := catalog.Variant{
		NullElisionSafeFields: []string{"*"},
	}

	// Runtime check: JSON tag must be "null_elision_safe_fields".
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}
	got := string(data)
	if !strings.Contains(got, `"null_elision_safe_fields":["*"]`) {
		t.Errorf("expected JSON to contain %q; got: %s", `"null_elision_safe_fields":["*"]`, got)
	}

	// omitempty check: unset field must not appear in JSON.
	empty := catalog.Variant{}
	emptyData, err := json.Marshal(empty)
	if err != nil {
		t.Fatalf("json.Marshal of empty Variant failed: %v", err)
	}
	emptyStr := string(emptyData)
	if strings.Contains(emptyStr, "null_elision_safe_fields") {
		t.Errorf("expected omitempty to suppress null_elision_safe_fields when unset; got: %s", emptyStr)
	}
}

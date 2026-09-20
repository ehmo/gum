package profile_test

import (
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/output/profile"
)

// TestFileLookupOnNilFile keeps the nil guard honest. ResolveProfile calls
// Lookup on a file it may not have loaded, so a nil receiver has to answer
// "no such profile" instead of panicking.
func TestFileLookupOnNilFile(t *testing.T) {
	var f *profile.File
	if got := f.Lookup("anything"); got != nil {
		t.Errorf("(*File)(nil).Lookup() = %v; want nil", got)
	}
}

// TestParseFileSurfacesPerTableErrors pins one error arm per table kind. Each
// table routes its keys to a different parser, so a swallowed error in any one
// of them turns a typo into a silently-default profile.
func TestParseFileSurfacesPerTableErrors(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "tests_table_key",
			src:  "[output_profiles.\"p\"]\nformat = \"json\"\n\n[[tests]]\nexpect_format = \"xml\"\n",
			want: "tests.expect_format",
		},
		{
			name: "override_bindings_value",
			src:  "[override_bindings]\n\"gmail.messages.list\" = nope\n",
			want: "override_bindings",
		},
		{
			name: "profile_table_key",
			src:  "[output_profiles.\"p\"]\nformat = \"xml\"\n",
			want: "format",
		},
		{
			name: "collapse_on_empty_cross_field",
			src:  "[output_profiles.\"p\"]\ncollapse_arrays = { max_items = 0 }\n",
			want: "on_empty",
		},
		{
			name: "attach_tests_failure",
			src:  "[[tests]]\nname = \"orphan\"\n",
			want: "no profile to attach to",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := profile.ParseFile(tc.src)
			if err == nil {
				t.Fatalf("ParseFile(%q) err=nil; want a rejection", tc.src)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err=%v; want %q in the message", err, tc.want)
			}
		})
	}
}

// TestProfileSectionNameQuoteForms covers both TOML literal-key quote styles
// and the two empty-name rejections. An unquoted name is further table
// nesting, not a profile called "a.b", so it has to stay an error.
func TestProfileSectionNameQuoteForms(t *testing.T) {
	f, err := profile.ParseFile("[output_profiles.'gmail.messages.list.v1']\nformat = \"json\"\n")
	if err != nil {
		t.Fatalf("ParseFile(single-quoted name): %v", err)
	}
	if f.Lookup("gmail.messages.list.v1") == nil {
		t.Errorf("profiles = %v; want the single-quoted name parsed", f.Profiles)
	}

	rejections := map[string]string{
		"empty_single_quoted": "[output_profiles.'']\nformat = \"json\"\n",
		"empty_double_quoted": "[output_profiles.\"\"]\nformat = \"json\"\n",
		"unquoted":            "[output_profiles.plain]\nformat = \"json\"\n",
	}
	for name, src := range rejections {
		t.Run(name, func(t *testing.T) {
			if _, err := profile.ParseFile(src); err == nil {
				t.Fatalf("ParseFile(%q) err=nil; want a rejection", src)
			}
		})
	}
}

// TestOverrideBindingKeyRejections covers the key half of the binding table.
// The value half already has tests; a malformed or repeated key is the half
// that would otherwise let one target silently win over another.
func TestOverrideBindingKeyRejections(t *testing.T) {
	cases := map[string]struct {
		src  string
		want string
	}{
		"unterminated_key": {"[override_bindings]\n\"gmail = \"p\"\n", "key"},
		"blank_key":        {"[override_bindings]\n\"   \" = \"p\"\n", "empty op_id"},
		"blank_value":      {"[override_bindings]\n\"a.b\" = \"   \"\n", "empty profile name"},
		"declared_twice":   {"[override_bindings]\n\"a.b\" = \"p\"\n\"a.b\" = \"q\"\n", "declared twice"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := profile.ParseFile(tc.src)
			if err == nil {
				t.Fatalf("ParseFile(%q) err=nil; want a rejection", tc.src)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err=%v; want %q in the message", err, tc.want)
			}
		})
	}
}

// TestAttachTestsRoutesFixtures pins the three attach outcomes: a bare-key file
// takes every fixture, an envelope with two profiles demands `profile = ...`,
// and a name that no table defines is an error rather than a dropped fixture.
func TestAttachTestsRoutesFixtures(t *testing.T) {
	t.Run("bare_key_file_takes_every_fixture", func(t *testing.T) {
		f, err := profile.ParseFile("format = \"json\"\n\n[[tests]]\nname = \"one\"\n\n[[tests]]\nname = \"two\"\n")
		if err != nil {
			t.Fatalf("ParseFile: %v", err)
		}
		if len(f.Profiles) != 1 {
			t.Fatalf("profiles = %d; want 1", len(f.Profiles))
		}
		if got := len(f.Profiles[0].Tests); got != 2 {
			t.Errorf("tests attached = %d; want 2", got)
		}
	})

	t.Run("two_profiles_need_an_explicit_target", func(t *testing.T) {
		src := "[output_profiles.\"a\"]\nformat = \"json\"\n\n[output_profiles.\"b\"]\nformat = \"json\"\n\n[[tests]]\nname = \"amb\"\n"
		_, err := profile.ParseFile(src)
		if err == nil {
			t.Fatal("ParseFile err=nil; want the ambiguous-fixture rejection")
		}
		if !strings.Contains(err.Error(), "set profile") {
			t.Errorf("err=%v; want it to name the profile key as the fix", err)
		}
	})

	t.Run("one_profile_needs_no_explicit_target", func(t *testing.T) {
		src := "[output_profiles.\"only\"]\nformat = \"json\"\n\n[[tests]]\nname = \"implicit\"\n"
		f, err := profile.ParseFile(src)
		if err != nil {
			t.Fatalf("ParseFile: %v", err)
		}
		target := f.Lookup("only")
		if target == nil || len(target.Tests) != 1 {
			t.Fatalf("tests attached = %v; want the lone fixture on \"only\"", target)
		}
	})

	t.Run("unknown_target_is_an_error", func(t *testing.T) {
		src := "[output_profiles.\"a\"]\nformat = \"json\"\n\n[[tests]]\nname = \"x\"\nprofile = \"nope\"\n"
		_, err := profile.ParseFile(src)
		if err == nil {
			t.Fatal("ParseFile err=nil; want the undefined-profile rejection")
		}
		if !strings.Contains(err.Error(), "not defined in this file") {
			t.Errorf("err=%v; want it to say the profile is undefined", err)
		}
	})
}

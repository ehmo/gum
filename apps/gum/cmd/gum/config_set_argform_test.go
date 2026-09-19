package main

import (
	"strings"
	"testing"
)

// gum-ea9v: `gum config set <key> <value>` is the form git, npm, and gcloud
// take, so it is the first thing a caller tries. Cobra's own
// "accepts 1 arg(s), received 2" names neither the accepted form nor a way out
// of it.
func TestConfigSetTwoArgsNamesKeyValueForm(t *testing.T) {
	withTempConfigRootCLI(t)

	_, err := runCLI(t, "config", "set", "googleads.geo_target_constants", "2840")
	if err == nil {
		t.Fatal("config set with two args succeeded; want a rejection")
	}
	for _, want := range []string{"key=value", "gum config set googleads.geo_target_constants=2840"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q; want it to contain %q", err, want)
		}
	}
}

// TestConfigSetSpacedValueQuotesTheSuggestion covers the other way the arg
// count runs over: an unquoted value with spaces. The suggestion has to carry
// the quotes or it fails the same way when pasted.
func TestConfigSetSpacedValueQuotesTheSuggestion(t *testing.T) {
	withTempConfigRootCLI(t)

	_, err := runCLI(t, "config", "set", "output.note", "two", "words")
	if err == nil {
		t.Fatal("config set with three args succeeded; want a rejection")
	}
	if want := `gum config set output.note="two words"`; !strings.Contains(err.Error(), want) {
		t.Errorf("error %q; want it to contain %q", err, want)
	}
}

// TestConfigSetNoArgsNamesKeyValueForm: zero args must name the form too.
func TestConfigSetNoArgsNamesKeyValueForm(t *testing.T) {
	withTempConfigRootCLI(t)

	_, err := runCLI(t, "config", "set")
	if err == nil {
		t.Fatal("config set with no args succeeded; want a rejection")
	}
	if !strings.Contains(err.Error(), "key=value") {
		t.Errorf("error %q; want it to name the key=value form", err)
	}
}

// TestConfigSetMissingEqualsNamesTheForm: one arg without an `=` is the third
// way to miss the form, and it reaches RunE rather than the Args gate.
func TestConfigSetMissingEqualsNamesTheForm(t *testing.T) {
	withTempConfigRootCLI(t)

	_, err := runCLI(t, "config", "set", "output.default_format")
	if err == nil {
		t.Fatal("config set with no '=' succeeded; want a rejection")
	}
	if want := "gum config set output.default_format=<value>"; !strings.Contains(err.Error(), want) {
		t.Errorf("error %q; want it to contain %q", err, want)
	}
}

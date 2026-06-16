package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/ehmo/gum/internal/catalog"
)

// TestCLIRiskGate verifies spec §12.0: calling a read-class op with
// --risk=write must produce a RISK_TOOL_MISMATCH envelope before any
// upstream request. The fixture uses the embedded gmail.users.messages.list
// op (risk_class=read) and asserts the structured-error envelope.
func TestCLIRiskGate(t *testing.T) {
	root := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{
		"call", "gmail.users.messages.list",
		"--risk=write",
		"userId=me",
	})
	err := root.ExecuteContext(context.Background())
	if err == nil {
		t.Fatalf("expected risk-gate failure; got success.\noutput:\n%s", out.String())
	}
	// The CLI prints the envelope on stdout AND returns the structured error.
	body := out.String()
	if !strings.Contains(body, "RISK_TOOL_MISMATCH") {
		t.Errorf("envelope missing RISK_TOOL_MISMATCH:\n%s", body)
	}
	if !strings.Contains(body, `"required_risk_flag": "--risk=read"`) {
		t.Errorf("envelope missing required_risk_flag=--risk=read:\n%s", body)
	}
	if !strings.Contains(body, `"variant_id"`) {
		t.Errorf("envelope missing variant_id for the resolved variant:\n%s", body)
	}
}

// TestCLIVariantSelection verifies the --variant-id flag pins variant
// resolution. Calling gmail.users.messages.list with an explicit
// --variant-id that does not exist must surface VARIANT_NOT_FOUND
// (no upstream request, no silent fallback).
func TestCLIVariantSelection(t *testing.T) {
	t.Run("unknown variant_id → VARIANT_NOT_FOUND", func(t *testing.T) {
		root := newRootCmd()
		var out bytes.Buffer
		root.SetOut(&out)
		root.SetErr(&out)
		root.SetArgs([]string{
			"call", "gmail.users.messages.list",
			"--risk=read",
			"--variant-id=gmail.v999.nonexistent",
			"userId=me",
		})
		err := root.ExecuteContext(context.Background())
		if err == nil {
			t.Fatalf("expected VARIANT_NOT_FOUND; got success.\noutput:\n%s", out.String())
		}
		body := out.String()
		if !strings.Contains(body, "VARIANT_NOT_FOUND") {
			t.Errorf("envelope missing VARIANT_NOT_FOUND:\n%s", body)
		}
		var parsed map[string]any
		if jerr := json.Unmarshal([]byte(extractJSON(body)), &parsed); jerr == nil {
			if parsed["variant_id"] != "gmail.v999.nonexistent" {
				t.Errorf("envelope variant_id = %v; want gmail.v999.nonexistent", parsed["variant_id"])
			}
		}
	})

	// CLI surface separation: the parser reads --variant-id as a host control
	// flag, and a positional `variant_id=` would be treated as an operation
	// arg (not as the pin). This mirrors §12.0 line 2422 host-control rule.
	t.Run("positional variant_id= remains an op arg", func(t *testing.T) {
		// Sandbox auth so dispatch fails at AUTH_REQUIRED (fast, no network).
		t.Setenv("HOME", t.TempDir())
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())
		t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", "")
		// Tight context: even if auth resolution tries a network leg, abort
		// before the test wallclock budget blows up.
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		root := newRootCmd()
		var out bytes.Buffer
		root.SetOut(&out)
		root.SetErr(&out)
		root.SetArgs([]string{
			"call", "gmail.users.messages.list",
			"--risk=read",
			"variant_id=this-is-an-op-arg-not-a-pin",
			"userId=me",
		})
		// We don't care about the dispatch outcome (the embedded gmail op
		// fails at the executor / auth without live creds); we only care
		// that the CLI did NOT route variant_id= into RequestedVariantID.
		// If it had, the dispatcher would surface VARIANT_NOT_FOUND.
		_ = root.ExecuteContext(ctx)
		if strings.Contains(out.String(), "VARIANT_NOT_FOUND") {
			t.Errorf("positional variant_id= was incorrectly routed to variant pin:\n%s", out.String())
		}
	})
}

// extractJSON pulls the first {...} block from a CLI body that may contain
// other diagnostics. Returns the input as-is when no brace is found so tests
// can still grep for substrings.
func extractJSON(body string) string {
	i := strings.Index(body, "{")
	if i < 0 {
		return body
	}
	depth := 0
	for j := i; j < len(body); j++ {
		switch body[j] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return body[i : j+1]
			}
		}
	}
	return body[i:]
}

// TestCliArgInvalid locks the CLI_ARG_INVALID wrapper shape: a *callargs.Error
// whose Code matches the stable §1421 string and whose Reason round-trips
// through Error().
func TestCliArgInvalid(t *testing.T) {
	err := cliArgInvalid("--risk is required")
	if err == nil {
		t.Fatal("cliArgInvalid returned nil")
	}
	if got := err.Error(); !strings.Contains(got, "CLI_ARG_INVALID") {
		t.Errorf("Error() = %q, want CLI_ARG_INVALID prefix", got)
	}
	if got := err.Error(); !strings.Contains(got, "--risk is required") {
		t.Errorf("Error() = %q, want reason substring", got)
	}
}

// TestRequiresConfirmation is the destructive-confirmation companion error.
// Same code class as cliArgInvalid but the message MUST describe the signed
// retry shape so the LLM/user does not use an unsafe local --yes shortcut.
func TestRequiresConfirmation(t *testing.T) {
	err := requiresConfirmation("calendar.events.delete")
	if err == nil {
		t.Fatal("requiresConfirmation returned nil")
	}
	if got := err.Error(); !strings.Contains(got, "--confirmed --token") {
		t.Errorf("Error() = %q, want --confirmed --token mention", got)
	}
	if got := err.Error(); !strings.Contains(got, "calendar.events.delete") {
		t.Errorf("Error() = %q, want op_id mention", got)
	}
}

// TestNormalizeRisk locks the closed risk enum.
func TestNormalizeRisk(t *testing.T) {
	cases := []struct {
		in   string
		want string
		ok   bool
	}{
		{"read", "read", true},
		{"WRITE", "write", true},
		{" destructive ", "destructive", true},
		{"admin", "", false},
		{"", "", false},
	}
	for _, tc := range cases {
		got, ok := normalizeRisk(tc.in)
		if ok != tc.ok {
			t.Errorf("normalizeRisk(%q) ok = %v, want %v", tc.in, ok, tc.ok)
		}
		if ok && got != tc.want {
			t.Errorf("normalizeRisk(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestSelectFormat verifies the mutually-exclusive output-format flag selector.
func TestSelectFormat(t *testing.T) {
	cases := []struct {
		name       string
		j, t, c, m bool
		want       string
		wantErr    bool
	}{
		{name: "default_is_json", want: "json"},
		{name: "json_explicit", j: true, want: "json"},
		{name: "toon_explicit", t: true, want: "toon"},
		{name: "csv_explicit", c: true, want: "csv"},
		{name: "markdown_explicit", m: true, want: "markdown"},
		{name: "two_flags_rejected", j: true, t: true, wantErr: true},
		{name: "three_flags_rejected", j: true, c: true, m: true, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := selectFormat(tc.j, tc.t, tc.c, tc.m)
			if tc.wantErr {
				if err == nil {
					t.Errorf("selectFormat: want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("selectFormat: %v", err)
			}
			if got != tc.want {
				t.Errorf("selectFormat = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestAsString covers the variant-risk-class string coercion used inside
// printDispatchError's RISK_TOOL_MISMATCH hint.
func TestAsString(t *testing.T) {
	if got := asString("read"); got != "read" {
		t.Errorf("asString(string) = %q, want read", got)
	}
	if got := asString(catalog.RiskClass("write")); got != "write" {
		t.Errorf("asString(RiskClass) = %q, want write", got)
	}
	if got := asString(42); got != "" {
		t.Errorf("asString(int) = %q, want empty fallback", got)
	}
	if got := asString(nil); got != "" {
		t.Errorf("asString(nil) = %q, want empty fallback", got)
	}
}

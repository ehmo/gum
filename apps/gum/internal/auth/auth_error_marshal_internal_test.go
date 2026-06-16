package auth

import (
	"encoding/json"
	"testing"
)

// TestAuthErrorMarshalJSONBranches pins the spec §7 envelope shape:
// JSON tag is "error_code" not "code"; empty optional fields omit;
// UserMessage falls back to HumanRemediation when blank; explicit
// UserMessage wins over HumanRemediation. The MCP stdio surface
// forwards this verbatim, so a tag rename or fallback regression
// would break the host contract.
func TestAuthErrorMarshalJSONBranches(t *testing.T) {
	t.Run("uses_error_code_tag_and_omits_blanks", func(t *testing.T) {
		e := &AuthError{Code: "NO_ADC", Strategy: "adc", HumanRemediation: "run gum auth login"}
		buf, err := json.Marshal(e)
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}
		var got map[string]any
		if err := json.Unmarshal(buf, &got); err != nil {
			t.Fatalf("Unmarshal: %v", err)
		}
		if got["error_code"] != "NO_ADC" {
			t.Errorf("error_code=%v; want NO_ADC", got["error_code"])
		}
		if _, has := got["code"]; has {
			t.Errorf("unexpected `code` key (must be error_code): %s", buf)
		}
		// UserMessage absent → falls back to HumanRemediation.
		if got["user_message"] != "run gum auth login" {
			t.Errorf("user_message=%v; want fallback to HumanRemediation", got["user_message"])
		}
		// Omitted optional fields must not appear.
		for _, k := range []string{"missing_components", "required_scopes", "have_scopes", "setup_command", "op_id", "retryable"} {
			if _, has := got[k]; has {
				t.Errorf("expected key %q omitted (zero value); got envelope=%s", k, buf)
			}
		}
	})

	t.Run("explicit_user_message_wins", func(t *testing.T) {
		e := &AuthError{
			Code:             "SCOPE_MISSING",
			Strategy:         "byo_oauth",
			HumanRemediation: "fallback",
			UserMessage:      "explicit",
		}
		buf, err := json.Marshal(e)
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}
		var got map[string]any
		_ = json.Unmarshal(buf, &got)
		if got["user_message"] != "explicit" {
			t.Errorf("user_message=%v; want explicit", got["user_message"])
		}
	})

	t.Run("retryable_true_is_emitted", func(t *testing.T) {
		e := &AuthError{Code: "X", Retryable: true}
		buf, err := json.Marshal(e)
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}
		var got map[string]any
		_ = json.Unmarshal(buf, &got)
		if got["retryable"] != true {
			t.Errorf("retryable=%v; want true", got["retryable"])
		}
	})
}

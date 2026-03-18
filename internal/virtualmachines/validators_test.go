package virtualmachines

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestPasswordValidator(t *testing.T) {
	t.Parallel()
	v := PasswordValidator{}
	tests := []struct {
		name     string
		input    string
		wantSummary string
	}{
		{name: "too_short", input: "Short1!", wantSummary: "Invalid password length"},
		{name: "too_long", input: "GigaLongPassword123!ExtraChars", wantSummary: "Invalid password length"},
		{name: "missing_uppercase", input: "test1password!", wantSummary: "Password missing uppercase letter"},
		{name: "missing_lowercase", input: "TEST1PASSWORD!", wantSummary: "Password missing lowercase letter"},
		{name: "missing_digit", input: "TestPassword!!", wantSummary: "Password missing digit"},
		{name: "missing_symbol", input: "Test1Password1", wantSummary: "Password missing symbol"},
		{name: "invalid_chars", input: "Test1Pass$word", wantSummary: "Invalid password characters"}, // $ not in allowed set: ! @ # % - _ .
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var resp validator.StringResponse
			v.ValidateString(context.Background(), validator.StringRequest{
				ConfigValue: types.StringValue(tc.input),
			}, &resp)
			if !resp.Diagnostics.HasError() {
				t.Fatalf("expected error with summary %q, got none", tc.wantSummary)
			}
			found := false
			for _, d := range resp.Diagnostics {
				if d.Summary() == tc.wantSummary {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("expected diagnostic with summary %q, got: %v", tc.wantSummary, resp.Diagnostics)
			}
		})
	}
}

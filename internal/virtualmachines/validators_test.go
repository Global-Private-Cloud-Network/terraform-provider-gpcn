package virtualmachines

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func runStringValidator(v validator.String, value string) validator.StringResponse {
	var resp validator.StringResponse
	v.ValidateString(context.Background(), validator.StringRequest{
		Path:        path.Root("text"),
		ConfigValue: types.StringValue(value),
	}, &resp)
	return resp
}

func TestMaxBytesValidator(t *testing.T) {
	const limit = 8
	v := MaxBytesValidator{Max: limit}

	t.Run("exactly_max_bytes", func(t *testing.T) {
		resp := runStringValidator(v, strings.Repeat("a", limit))
		if resp.Diagnostics.HasError() {
			t.Fatalf("expected no error, got %v", resp.Diagnostics)
		}
	})

	t.Run("one_byte_over", func(t *testing.T) {
		resp := runStringValidator(v, strings.Repeat("a", limit+1))
		if resp.Diagnostics.ErrorsCount() != 1 {
			t.Fatalf("expected one error, got %v", resp.Diagnostics)
		}
		got := resp.Diagnostics.Errors()[0]
		if got.Summary() != ErrSummaryTextTooLong {
			t.Errorf("expected summary %q, got %q", ErrSummaryTextTooLong, got.Summary())
		}
		wantDetail := fmt.Sprintf(ErrDetailTextTooLong, limit, limit+1)
		if got.Detail() != wantDetail {
			t.Errorf("expected detail %q, got %q", wantDetail, got.Detail())
		}
	})

	// Five characters of three bytes each: under Max in characters, over Max in bytes.
	t.Run("multi_byte_over", func(t *testing.T) {
		value := strings.Repeat("€", 5)
		resp := runStringValidator(v, value)
		if resp.Diagnostics.ErrorsCount() != 1 {
			t.Fatalf("expected one error, got %v", resp.Diagnostics)
		}
		wantDetail := fmt.Sprintf(ErrDetailTextTooLong, limit, len(value))
		if got := resp.Diagnostics.Errors()[0].Detail(); got != wantDetail {
			t.Errorf("expected detail %q, got %q", wantDetail, got)
		}
	})
}

func TestBasicPlaneValidator(t *testing.T) {
	v := BasicPlaneValidator{}

	t.Run("u_ffff_passes", func(t *testing.T) {
		resp := runStringValidator(v, "a\uffffb")
		if resp.Diagnostics.HasError() {
			t.Fatalf("expected no error, got %v", resp.Diagnostics)
		}
	})

	t.Run("u_10000_fails", func(t *testing.T) {
		resp := runStringValidator(v, "a\U00010000b")
		if resp.Diagnostics.ErrorsCount() != 1 {
			t.Fatalf("expected one error, got %v", resp.Diagnostics)
		}
		got := resp.Diagnostics.Errors()[0]
		if got.Summary() != ErrSummaryUserDataCharacter {
			t.Errorf("expected summary %q, got %q", ErrSummaryUserDataCharacter, got.Summary())
		}
		if got.Detail() != ErrDetailUserDataCharacter {
			t.Errorf("expected detail %q, got %q", ErrDetailUserDataCharacter, got.Detail())
		}
	})
}

func TestPasswordValidator(t *testing.T) {
	v := PasswordValidator{}
	tests := []struct {
		name        string
		input       string
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

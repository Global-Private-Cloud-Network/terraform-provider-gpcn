package sshkeys_test

import (
	"context"
	"strings"
	"testing"

	"terraform-provider-gpcn/internal/sshkeys"

	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestSSHKeyNameValidator(t *testing.T) {
	t.Parallel()

	const invalidSummary = "Invalid SSH key name"
	const whitespaceDetail = "Name must not start or end with whitespace (GPCN trims it, which would make the stored name differ from the configuration)"
	const requiredDetail = "Name is required"
	const tooLongDetail = "Name must be at most 30 characters"
	const charactersDetail = "Name may contain only letters, numbers, spaces, periods, hyphens, and the symbols _ ( ) ' #, and must begin and end with a letter or number"

	tests := []struct {
		name       string
		input      string
		wantDetail string
	}{
		{name: "valid_plain", input: "terraform-demo-key", wantDetail: ""},
		{name: "valid_extra_symbols", input: "key_(one)'s #1", wantDetail: ""},
		{name: "valid_at_max_length", input: strings.Repeat("a", 30), wantDetail: ""},
		{name: "leading_hyphen", input: "-demo-key", wantDetail: charactersDetail},
		{name: "trailing_hyphen", input: "demo-key-", wantDetail: charactersDetail},
		{name: "over_max_length", input: strings.Repeat("a", 31), wantDetail: tooLongDetail},
		{name: "leading_space", input: " demo-key", wantDetail: whitespaceDetail},
		{name: "trailing_space", input: "demo-key ", wantDetail: whitespaceDetail},
		{name: "empty", input: "", wantDetail: requiredDetail},
		// GPCN trims with JavaScript, which removes a byte order mark. A mark
		// the provider keeps makes the stored name differ from the plan.
		{name: "leading_byte_order_mark", input: "\ufeffdemo-key", wantDetail: whitespaceDetail},
		// JavaScript keeps U+0085, and the shared trim set follows JavaScript, so
		// the whitespace refusal never fires. The name regex refuses the character.
		{name: "leading_next_line", input: "\u0085demo-key", wantDetail: charactersDetail},
		{name: "unicode_letter", input: "café", wantDetail: charactersDetail},
		{name: "interior_unicode_letter", input: "café-key", wantDetail: charactersDetail},
	}

	v := sshkeys.NameValidator{}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var resp validator.StringResponse
			v.ValidateString(context.Background(), validator.StringRequest{
				ConfigValue: types.StringValue(tc.input),
			}, &resp)

			if tc.wantDetail == "" {
				if resp.Diagnostics.HasError() {
					t.Fatalf("expected %q to be accepted, got: %v", tc.input, resp.Diagnostics)
				}
				return
			}
			if !resp.Diagnostics.HasError() {
				t.Fatalf("expected %q to be rejected with detail %q, got no error", tc.input, tc.wantDetail)
			}
			if len(resp.Diagnostics) != 1 {
				t.Fatalf("expected exactly one diagnostic for %q, got: %v", tc.input, resp.Diagnostics)
			}
			if got := resp.Diagnostics[0].Summary(); got != invalidSummary {
				t.Errorf("summary mismatch for %q:\n got %q\nwant %q", tc.input, got, invalidSummary)
			}
			if got := resp.Diagnostics[0].Detail(); got != tc.wantDetail {
				t.Errorf("detail mismatch for %q:\n got %q\nwant %q", tc.input, got, tc.wantDetail)
			}
		})
	}
}

func TestSSHKeyNameValidatorSkipsNullAndUnknown(t *testing.T) {
	t.Parallel()

	v := sshkeys.NameValidator{}
	for _, value := range []types.String{types.StringNull(), types.StringUnknown()} {
		var resp validator.StringResponse
		v.ValidateString(context.Background(), validator.StringRequest{ConfigValue: value}, &resp)
		if resp.Diagnostics.HasError() {
			t.Errorf("expected %v to be skipped, got: %v", value, resp.Diagnostics)
		}
	}
}

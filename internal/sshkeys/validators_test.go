package sshkeys_test

import (
	"context"
	"strings"
	"testing"

	"terraform-provider-gpcn/internal/provider"
	"terraform-provider-gpcn/internal/sshkeys"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
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
		{name: "unicode_letter", input: "café", wantDetail: charactersDetail},
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

// The validator changes nothing until the schema carries it. The attachment
// therefore needs a guard of its own.
func TestSSHKeyResourceSchemaAttachesNameValidator(t *testing.T) {
	t.Parallel()

	var resp resource.SchemaResponse
	provider.NewSSHKeyResource().Schema(context.Background(), resource.SchemaRequest{}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("schema returned diagnostics: %v", resp.Diagnostics)
	}

	attribute, ok := resp.Schema.Attributes["name"].(schema.StringAttribute)
	if !ok {
		t.Fatalf("name is %T, want schema.StringAttribute", resp.Schema.Attributes["name"])
	}
	for _, v := range attribute.Validators {
		if _, ok := v.(sshkeys.NameValidator); ok {
			return
		}
	}
	t.Errorf("name carries %d validators, none of them sshkeys.NameValidator", len(attribute.Validators))
}

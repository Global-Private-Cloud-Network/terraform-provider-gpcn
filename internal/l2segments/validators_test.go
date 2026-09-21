package l2segments_test

import (
	"context"
	"testing"

	"terraform-provider-gpcn/internal/l2segments"
	"terraform-provider-gpcn/internal/provider"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestL2SegmentNoOuterWhitespaceValidator(t *testing.T) {
	t.Parallel()

	const nameSummary = "Invalid L2 segment name"
	const nameDetail = "name must not start or end with whitespace (GPCN trims it, which would make the stored value differ from the configuration)"

	tests := []struct {
		name       string
		input      string
		wantDetail string
	}{
		{name: "valid_plain", input: "segment-a", wantDetail: ""},
		{name: "valid_inner_space", input: "segment a", wantDetail: ""},
		{name: "valid_empty", input: "", wantDetail: ""},
		{name: "leading_space", input: " segment-a", wantDetail: nameDetail},
		{name: "trailing_space", input: "segment-a ", wantDetail: nameDetail},
		{name: "leading_tab", input: "\tsegment-a", wantDetail: nameDetail},
		{name: "trailing_newline", input: "segment-a\n", wantDetail: nameDetail},
	}

	v := l2segments.NoOuterWhitespaceValidator{Attribute: "name"}
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
			if len(resp.Diagnostics) != 1 {
				t.Fatalf("expected exactly one diagnostic for %q, got: %v", tc.input, resp.Diagnostics)
			}
			if got := resp.Diagnostics[0].Summary(); got != nameSummary {
				t.Errorf("summary mismatch for %q:\n got %q\nwant %q", tc.input, got, nameSummary)
			}
			if got := resp.Diagnostics[0].Detail(); got != tc.wantDetail {
				t.Errorf("detail mismatch for %q:\n got %q\nwant %q", tc.input, got, tc.wantDetail)
			}
		})
	}
}

// The sentence names the attribute the user must fix, so the description carries
// its own bytes.
func TestL2SegmentNoOuterWhitespaceValidatorNamesTheDescription(t *testing.T) {
	t.Parallel()

	const summary = "Invalid L2 segment description"
	const detail = "description must not start or end with whitespace (GPCN trims it, which would make the stored value differ from the configuration)"

	var resp validator.StringResponse
	l2segments.NoOuterWhitespaceValidator{Attribute: "description"}.ValidateString(
		context.Background(),
		validator.StringRequest{ConfigValue: types.StringValue("padded ")},
		&resp,
	)

	if len(resp.Diagnostics) != 1 {
		t.Fatalf("expected exactly one diagnostic, got: %v", resp.Diagnostics)
	}
	if got := resp.Diagnostics[0].Summary(); got != summary {
		t.Errorf("summary = %q, want %q", got, summary)
	}
	if got := resp.Diagnostics[0].Detail(); got != detail {
		t.Errorf("detail = %q, want %q", got, detail)
	}
}

func TestL2SegmentNoOuterWhitespaceValidatorSkipsNullAndUnknown(t *testing.T) {
	t.Parallel()

	v := l2segments.NoOuterWhitespaceValidator{Attribute: "name"}
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
func TestL2SegmentResourceSchemaAttachesWhitespaceValidators(t *testing.T) {
	t.Parallel()

	var resp resource.SchemaResponse
	provider.NewL2SegmentResource().Schema(context.Background(), resource.SchemaRequest{}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("schema returned diagnostics: %v", resp.Diagnostics)
	}

	for _, attributeName := range []string{"name", "description"} {
		attribute, ok := resp.Schema.Attributes[attributeName].(schema.StringAttribute)
		if !ok {
			t.Fatalf("%s is %T, want schema.StringAttribute", attributeName, resp.Schema.Attributes[attributeName])
		}
		found := false
		for _, v := range attribute.Validators {
			whitespace, isWhitespace := v.(l2segments.NoOuterWhitespaceValidator)
			if isWhitespace && whitespace.Attribute == attributeName {
				found = true
			}
		}
		if !found {
			t.Errorf("%s carries %d validators, none of them a NoOuterWhitespaceValidator for %q", attributeName, len(attribute.Validators), attributeName)
		}
	}
}

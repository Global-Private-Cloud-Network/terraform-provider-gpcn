package helpers_test

import (
	"context"
	"testing"

	"terraform-provider-gpcn/internal/helpers"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// The four Release B resources share one validator. The rendered bytes are the
// contract, so the table asserts literals rather than the format constants.
func TestNoOuterWhitespaceValidatorUnit(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		summary     string
		attribute   string
		value       types.String
		wantSummary string
		wantDetail  string
	}{
		{name: "clean", summary: "Invalid VPC %s", attribute: "name", value: types.StringValue("vpc-a")},
		{name: "interior space", summary: "Invalid VPC %s", attribute: "name", value: types.StringValue("vpc a")},
		{name: "empty", summary: "Invalid VPC %s", attribute: "description", value: types.StringValue("")},
		{
			name: "leading space", summary: "Invalid L2 segment %s", attribute: "name",
			value:       types.StringValue(" segment-a"),
			wantSummary: "Invalid L2 segment name",
			wantDetail:  "name must not start or end with whitespace (GPCN trims it, which would make the stored value differ from the configuration)",
		},
		{
			name: "trailing space", summary: "Invalid VPC subnet %s", attribute: "name",
			value:       types.StringValue("subnet-a "),
			wantSummary: "Invalid VPC subnet name",
			wantDetail:  "name must not start or end with whitespace (GPCN trims it, which would make the stored value differ from the configuration)",
		},
		{
			name: "leading tab", summary: "Invalid security group %s", attribute: "description",
			value:       types.StringValue("\tweb tier"),
			wantSummary: "Invalid security group description",
			wantDetail:  "description must not start or end with whitespace (GPCN trims it, which would make the stored value differ from the configuration)",
		},
		{
			name: "trailing newline", summary: "Invalid security group rule %s", attribute: "description",
			value:       types.StringValue("ping\n"),
			wantSummary: "Invalid security group rule description",
			wantDetail:  "description must not start or end with whitespace (GPCN trims it, which would make the stored value differ from the configuration)",
		},
		{name: "null", summary: "Invalid VPC %s", attribute: "description", value: types.StringNull()},
		{name: "unknown", summary: "Invalid VPC %s", attribute: "name", value: types.StringUnknown()},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			response := &validator.StringResponse{}
			helpers.NoOuterWhitespaceValidator{Summary: testCase.summary, Attribute: testCase.attribute}.ValidateString(
				context.Background(),
				validator.StringRequest{Path: path.Root(testCase.attribute), ConfigValue: testCase.value},
				response,
			)

			if testCase.wantDetail == "" {
				if count := len(response.Diagnostics); count != 0 {
					t.Fatalf("Expected no diagnostics, got %d: %v", count, response.Diagnostics)
				}
				return
			}

			if count := len(response.Diagnostics); count != 1 {
				t.Fatalf("Expected 1 diagnostic, got %d: %v", count, response.Diagnostics)
			}
			if got := response.Diagnostics[0].Summary(); got != testCase.wantSummary {
				t.Errorf("Summary = %q, want %q", got, testCase.wantSummary)
			}
			if got := response.Diagnostics[0].Detail(); got != testCase.wantDetail {
				t.Errorf("Detail = %q, want %q", got, testCase.wantDetail)
			}
		})
	}
}

// The description reaches the operator through the generated documentation, so
// it names the attribute it governs.
func TestNoOuterWhitespaceValidatorDescriptionNamesTheAttributeUnit(t *testing.T) {
	t.Parallel()

	subject := helpers.NoOuterWhitespaceValidator{Summary: "Invalid VPC %s", Attribute: "description"}
	const want = "description must not start or end with whitespace"

	if got := subject.Description(context.Background()); got != want {
		t.Errorf("Description = %q, want %q", got, want)
	}
	if got := subject.MarkdownDescription(context.Background()); got != want {
		t.Errorf("MarkdownDescription = %q, want %q", got, want)
	}
}

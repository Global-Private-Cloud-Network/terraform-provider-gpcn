package vpcs

import (
	"context"
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// The three details are the backend's own refinement messages. A validator
// that disagrees with the API is worse than no validator at all.
func TestVpcSuperCidrValidator(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		cidr       string
		wantDetail string
	}{
		{name: "slash_16", cidr: "10.50.0.0/16"},
		{name: "slash_24", cidr: "10.50.1.0/24"},
		{name: "rfc1918_172", cidr: "172.16.0.0/16"},
		{name: "rfc1918_192", cidr: "192.168.0.0/24"},
		{name: "not_network_address", cidr: "10.50.0.1/16", wantDetail: ErrDetailVpcCidrShape},
		{name: "host_bits_set", cidr: "10.50.1.0/16", wantDetail: ErrDetailVpcCidrShape},
		{name: "leading_zero_octet", cidr: "010.50.0.0/16", wantDetail: ErrDetailVpcCidrShape},
		{name: "leading_zero_prefix", cidr: "10.50.0.0/016", wantDetail: ErrDetailVpcCidrShape},
		{name: "octet_over_255", cidr: "10.256.0.0/16", wantDetail: ErrDetailVpcCidrShape},
		{name: "no_prefix", cidr: "10.50.0.0", wantDetail: ErrDetailVpcCidrShape},
		{name: "not_a_cidr", cidr: "office-lan", wantDetail: ErrDetailVpcCidrShape},
		{name: "ipv6", cidr: "fd00::/16", wantDetail: ErrDetailVpcCidrShape},
		{name: "prefix_too_short", cidr: "10.0.0.0/8", wantDetail: ErrDetailVpcCidrPrefixBand},
		{name: "prefix_too_long", cidr: "10.50.1.0/25", wantDetail: ErrDetailVpcCidrPrefixBand},
		{name: "prefix_band_before_rfc1918", cidr: "8.0.0.0/8", wantDetail: ErrDetailVpcCidrPrefixBand},
		{name: "public_range", cidr: "8.8.8.0/24", wantDetail: ErrDetailVpcCidrNotRfc1918},
		{name: "outside_172_16_12", cidr: "172.32.0.0/16", wantDetail: ErrDetailVpcCidrNotRfc1918},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			response := &validator.StringResponse{}
			SuperCidrValidator{}.ValidateString(
				context.Background(),
				validator.StringRequest{Path: path.Root("cidr"), ConfigValue: types.StringValue(testCase.cidr)},
				response,
			)

			if testCase.wantDetail == "" {
				if response.Diagnostics.HasError() {
					t.Fatalf("Expected %q to validate, got %v", testCase.cidr, response.Diagnostics)
				}
				return
			}

			if count := len(response.Diagnostics); count != 1 {
				t.Fatalf("Expected 1 diagnostic for %q, got %d: %v", testCase.cidr, count, response.Diagnostics)
			}
			if got := response.Diagnostics[0].Summary(); got != ErrSummaryInvalidVpcCidr {
				t.Errorf("Summary = %q, want %q", got, ErrSummaryInvalidVpcCidr)
			}
			if got := response.Diagnostics[0].Detail(); got != testCase.wantDetail {
				t.Errorf("Detail = %q, want %q", got, testCase.wantDetail)
			}
		})
	}
}

// A null or unknown value belongs to the framework, not to the API rules.
func TestVpcSuperCidrValidatorSkipsNullAndUnknown(t *testing.T) {
	t.Parallel()

	for name, value := range map[string]types.String{"null": types.StringNull(), "unknown": types.StringUnknown()} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			response := &validator.StringResponse{}
			SuperCidrValidator{}.ValidateString(
				context.Background(),
				validator.StringRequest{Path: path.Root("cidr"), ConfigValue: value},
				response,
			)
			if count := len(response.Diagnostics); count != 0 {
				t.Fatalf("Expected no diagnostics, got %d: %v", count, response.Diagnostics)
			}
		})
	}
}

// GPCN trims a name and a description. The validator refuses what GPCN would
// trim, so the stored value always matches the configuration.
func TestVpcNoSurroundingWhitespaceValidator(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		attribute string
		value     string
		wantError bool
	}{
		{name: "clean_name", attribute: "name", value: "vpc-a"},
		{name: "interior_space", attribute: "name", value: "vpc a"},
		{name: "empty", attribute: "description", value: ""},
		{name: "leading_space", attribute: "name", value: " vpc-a", wantError: true},
		{name: "trailing_space", attribute: "name", value: "vpc-a ", wantError: true},
		{name: "leading_tab", attribute: "description", value: "\tshared", wantError: true},
		{name: "trailing_newline", attribute: "description", value: "shared\n", wantError: true},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			response := &validator.StringResponse{}
			NoSurroundingWhitespaceValidator{Attribute: testCase.attribute}.ValidateString(
				context.Background(),
				validator.StringRequest{Path: path.Root(testCase.attribute), ConfigValue: types.StringValue(testCase.value)},
				response,
			)

			if !testCase.wantError {
				if response.Diagnostics.HasError() {
					t.Fatalf("Expected %q to validate, got %v", testCase.value, response.Diagnostics)
				}
				return
			}

			if count := len(response.Diagnostics); count != 1 {
				t.Fatalf("Expected 1 diagnostic for %q, got %d: %v", testCase.value, count, response.Diagnostics)
			}
			wantSummary := fmt.Sprintf(ErrSummaryInvalidVpcAttribute, testCase.attribute)
			if got := response.Diagnostics[0].Summary(); got != wantSummary {
				t.Errorf("Summary = %q, want %q", got, wantSummary)
			}
			wantDetail := testCase.attribute + " must not start or end with whitespace (GPCN trims it, which would make the stored value differ from the configuration)"
			if got := response.Diagnostics[0].Detail(); got != wantDetail {
				t.Errorf("Detail = %q, want %q", got, wantDetail)
			}
		})
	}
}

// A null or unknown value belongs to the framework, not to the API rules.
func TestVpcNoSurroundingWhitespaceValidatorSkipsNullAndUnknown(t *testing.T) {
	t.Parallel()

	for name, value := range map[string]types.String{"null": types.StringNull(), "unknown": types.StringUnknown()} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			response := &validator.StringResponse{}
			NoSurroundingWhitespaceValidator{Attribute: "name"}.ValidateString(
				context.Background(),
				validator.StringRequest{Path: path.Root("name"), ConfigValue: value},
				response,
			)
			if count := len(response.Diagnostics); count != 0 {
				t.Fatalf("Expected no diagnostics, got %d: %v", count, response.Diagnostics)
			}
		})
	}
}

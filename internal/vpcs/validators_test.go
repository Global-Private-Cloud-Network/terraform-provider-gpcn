package vpcs

import (
	"context"
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
		{name: "not_network_address", cidr: "10.50.0.1/16", wantDetail: "must be a valid IPv4 CIDR whose address is the network address (e.g. 10.50.0.0/16)"},
		{name: "host_bits_set", cidr: "10.50.1.0/16", wantDetail: "must be a valid IPv4 CIDR whose address is the network address (e.g. 10.50.0.0/16)"},
		{name: "leading_zero_octet", cidr: "010.50.0.0/16", wantDetail: "must be a valid IPv4 CIDR whose address is the network address (e.g. 10.50.0.0/16)"},
		{name: "leading_zero_prefix", cidr: "10.50.0.0/016", wantDetail: "must be a valid IPv4 CIDR whose address is the network address (e.g. 10.50.0.0/16)"},
		{name: "octet_over_255", cidr: "10.256.0.0/16", wantDetail: "must be a valid IPv4 CIDR whose address is the network address (e.g. 10.50.0.0/16)"},
		{name: "no_prefix", cidr: "10.50.0.0", wantDetail: "must be a valid IPv4 CIDR whose address is the network address (e.g. 10.50.0.0/16)"},
		{name: "not_a_cidr", cidr: "office-lan", wantDetail: "must be a valid IPv4 CIDR whose address is the network address (e.g. 10.50.0.0/16)"},
		{name: "ipv6", cidr: "fd00::/16", wantDetail: "must be a valid IPv4 CIDR whose address is the network address (e.g. 10.50.0.0/16)"},
		{name: "prefix_too_short", cidr: "10.0.0.0/8", wantDetail: "prefix must be between /16 and /24"},
		{name: "prefix_too_long", cidr: "10.50.1.0/25", wantDetail: "prefix must be between /16 and /24"},
		{name: "prefix_band_before_rfc1918", cidr: "8.0.0.0/8", wantDetail: "prefix must be between /16 and /24"},
		{name: "public_range", cidr: "8.8.8.0/24", wantDetail: "must lie inside an RFC1918 private range (10/8, 172.16/12, 192.168/16)"},
		{name: "outside_172_16_12", cidr: "172.32.0.0/16", wantDetail: "must lie inside an RFC1918 private range (10/8, 172.16/12, 192.168/16)"},
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
			if got := response.Diagnostics[0].Summary(); got != "Invalid VPC CIDR" {
				t.Errorf("Summary = %q, want %q", got, "Invalid VPC CIDR")
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

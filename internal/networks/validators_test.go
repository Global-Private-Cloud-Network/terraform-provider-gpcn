package networks

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestCIDRValidator(t *testing.T) {
	t.Parallel()
	v := CIDRValidator{}
	tests := []struct {
		name        string
		input       string
		wantSummary string
	}{
		{name: "invalid_cidr_block", input: "10.0.0.0/245", wantSummary: ErrSummaryInvalidAttr},
		{name: "host_bits_set", input: "10.0.0.1/24", wantSummary: ErrSummaryInvalidAttr},
		{name: "ipv6", input: "2001:db8::/32", wantSummary: ErrSummaryInvalidAttr},
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
			if resp.Diagnostics[0].Summary() != tc.wantSummary {
				t.Errorf("expected summary %q, got %q", tc.wantSummary, resp.Diagnostics[0].Summary())
			}
		})
	}
}

func TestDNSServerIPValidator(t *testing.T) {
	t.Parallel()
	v := DNSServerIPValidator{}
	tests := []struct {
		name        string
		input       string
		wantSummary string
	}{
		{name: "invalid_ip", input: "123.123.123.1234", wantSummary: ErrSummaryInvalidAttr},
		{name: "empty_string", input: "", wantSummary: ErrSummaryInvalidAttr},
		{name: "ipv6", input: "2001:db8::1", wantSummary: ErrSummaryInvalidAttr},
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
			if resp.Diagnostics[0].Summary() != tc.wantSummary {
				t.Errorf("expected summary %q, got %q", tc.wantSummary, resp.Diagnostics[0].Summary())
			}
		})
	}
}

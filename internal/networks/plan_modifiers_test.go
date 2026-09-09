package networks

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

var testDefaultRouteSchema = schema.Schema{
	Attributes: map[string]schema.Attribute{
		"cidr_block": schema.StringAttribute{Optional: true, Computed: true},
		"gateway_ip": schema.StringAttribute{Optional: true, Computed: true},
	},
}

func planWithCIDR(cidr tftypes.Value) tfsdk.Plan {
	raw := tftypes.NewValue(tftypes.Object{
		AttributeTypes: map[string]tftypes.Type{
			"cidr_block": tftypes.String,
			"gateway_ip": tftypes.String,
		},
	}, map[string]tftypes.Value{
		"cidr_block": cidr,
		"gateway_ip": tftypes.NewValue(tftypes.String, tftypes.UnknownValue),
	})
	return tfsdk.Plan{Raw: raw, Schema: testDefaultRouteSchema}
}

func TestDefaultRouteFromCIDR(t *testing.T) {
	cidrValue := func(s string) tftypes.Value { return tftypes.NewValue(tftypes.String, s) }
	nullCIDR := tftypes.NewValue(tftypes.String, nil)
	unknownCIDR := tftypes.NewValue(tftypes.String, tftypes.UnknownValue)

	tests := []struct {
		name        string
		configValue types.String
		stateValue  types.String
		cidr        tftypes.Value
		wantUnknown bool
		want        string
	}{
		{"user value kept", types.StringValue("10.0.0.9"), types.StringNull(), cidrValue("10.0.0.0/24"), false, "10.0.0.9"},
		{"derived from cidr", types.StringNull(), types.StringNull(), cidrValue("192.168.5.0/24"), false, "192.168.5.1"},
		{"cidr unknown preserves state", types.StringNull(), types.StringValue("10.0.0.1"), unknownCIDR, false, "10.0.0.1"},
		{"cidr unknown no state stays unknown", types.StringNull(), types.StringNull(), unknownCIDR, true, ""},
		{"cidr null no state stays unknown", types.StringNull(), types.StringNull(), nullCIDR, true, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := planmodifier.StringRequest{
				ConfigValue: tc.configValue,
				StateValue:  tc.stateValue,
				PlanValue:   types.StringUnknown(),
				Path:        path.Root("gateway_ip"),
				Plan:        planWithCIDR(tc.cidr),
			}
			resp := &planmodifier.StringResponse{PlanValue: types.StringUnknown()}
			DefaultRouteFromCIDR{}.PlanModifyString(context.Background(), req, resp)

			if resp.Diagnostics.HasError() {
				t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
			}
			if tc.wantUnknown {
				if !resp.PlanValue.IsUnknown() {
					t.Errorf("expected unknown, got %v", resp.PlanValue)
				}
				return
			}
			if resp.PlanValue.ValueString() != tc.want {
				t.Errorf("expected %q, got %q", tc.want, resp.PlanValue.ValueString())
			}
		})
	}
}

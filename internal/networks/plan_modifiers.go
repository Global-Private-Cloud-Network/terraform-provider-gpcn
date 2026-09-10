package networks

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// DefaultRouteFromCIDR resolves gateway_ip at plan time. It keeps a user value,
// else derives the first usable host of cidr_block, else falls back to state.
type DefaultRouteFromCIDR struct{}

func (m DefaultRouteFromCIDR) Description(_ context.Context) string {
	return "Derives gateway_ip from cidr_block when the user omits it"
}

func (m DefaultRouteFromCIDR) MarkdownDescription(ctx context.Context) string {
	return m.Description(ctx)
}

func (m DefaultRouteFromCIDR) PlanModifyString(ctx context.Context, req planmodifier.StringRequest, resp *planmodifier.StringResponse) {
	// The user supplied a value. Keep it.
	if !req.ConfigValue.IsNull() {
		resp.PlanValue = req.ConfigValue
		return
	}

	var networkType, cidr types.String
	resp.Diagnostics.Append(req.Plan.GetAttribute(ctx, path.Root("network_type"), &networkType)...)
	resp.Diagnostics.Append(req.Plan.GetAttribute(ctx, path.Root("cidr_block"), &cidr)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Only a standard network gets a default route. A custom network keeps the
	// server value.
	if networkType.ValueString() == NETWORK_TYPE_STANDARD && !cidr.IsNull() && !cidr.IsUnknown() && cidr.ValueString() != "" {
		if host, err := firstUsableHostFromCIDR(cidr.ValueString()); err == nil {
			resp.PlanValue = types.StringValue(host)
			return
		}
	}

	// No value to derive. Preserve the state value like UseStateForUnknown.
	if !req.StateValue.IsNull() {
		resp.PlanValue = req.StateValue
	}
}

package virtualmachines

import (
	"context"

	"terraform-provider-gpcn/internal/networks"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// PublicIpPlanModifier marks public_ip as unknown when the machine is about to take on
// or give up an address. Either way of asking for one changes the address the birth
// interface carries. Only the apply learns what it becomes.
type PublicIpPlanModifier struct{}

const publicIpPlanModifierDescription = "Marks public_ip as unknown when allocate_public_ip or public_ip_id changes"

func (m PublicIpPlanModifier) Description(_ context.Context) string {
	return publicIpPlanModifierDescription
}

func (m PublicIpPlanModifier) MarkdownDescription(_ context.Context) string {
	return publicIpPlanModifierDescription
}

func (m PublicIpPlanModifier) PlanModifyString(ctx context.Context, req planmodifier.StringRequest, resp *planmodifier.StringResponse) {
	// If the resource is being created, leave it unknown
	if req.StateValue.IsNull() {
		return
	}

	// Get allocate_public_ip from both state and plan
	var stateAllocatePublicIp, planAllocatePublicIp types.Bool
	req.State.GetAttribute(ctx, path.Root("allocate_public_ip"), &stateAllocatePublicIp)
	req.Plan.GetAttribute(ctx, path.Root("allocate_public_ip"), &planAllocatePublicIp)

	var statePublicIpId, planPublicIpId types.String
	req.State.GetAttribute(ctx, path.Root("public_ip_id"), &statePublicIpId)
	req.Plan.GetAttribute(ctx, path.Root("public_ip_id"), &planPublicIpId)

	// If either way of asking for an address is changing, mark public_ip as unknown
	if !stateAllocatePublicIp.Equal(planAllocatePublicIp) || !statePublicIpId.Equal(planPublicIpId) {
		resp.PlanValue = types.StringUnknown()
		return
	}

	// Otherwise, preserve the state value (like UseStateForUnknown)
	resp.PlanValue = req.StateValue
}

// NetworkInterfacesPlanModifier marks network_interfaces as unknown when any attribute
// that shapes the interface set changes. Each one adds, removes or re-addresses an
// interface, so the value must refresh after apply.
type NetworkInterfacesPlanModifier struct{}

const networkInterfacesPlanModifierDescription = "Marks network_interfaces as unknown when subnet_id, l2_segment_ids, allocate_public_ip or public_ip_id changes"

func (m NetworkInterfacesPlanModifier) Description(_ context.Context) string {
	return networkInterfacesPlanModifierDescription
}

func (m NetworkInterfacesPlanModifier) MarkdownDescription(_ context.Context) string {
	return networkInterfacesPlanModifierDescription
}

func (m NetworkInterfacesPlanModifier) PlanModifyList(ctx context.Context, req planmodifier.ListRequest, resp *planmodifier.ListResponse) {
	// If the resource is being created, leave it unknown
	if req.StateValue.IsNull() {
		return
	}

	changed := false
	for _, name := range []string{"subnet_id", "public_ip_id"} {
		var stateValue, planValue types.String
		req.State.GetAttribute(ctx, path.Root(name), &stateValue)
		req.Plan.GetAttribute(ctx, path.Root(name), &planValue)
		if !stateValue.Equal(planValue) {
			changed = true
		}
	}

	var stateSegmentIds, planSegmentIds types.List
	req.State.GetAttribute(ctx, path.Root("l2_segment_ids"), &stateSegmentIds)
	req.Plan.GetAttribute(ctx, path.Root("l2_segment_ids"), &planSegmentIds)

	var stateAllocatePublicIp, planAllocatePublicIp types.Bool
	req.State.GetAttribute(ctx, path.Root("allocate_public_ip"), &stateAllocatePublicIp)
	req.Plan.GetAttribute(ctx, path.Root("allocate_public_ip"), &planAllocatePublicIp)

	if changed || !stateSegmentIds.Equal(planSegmentIds) || !stateAllocatePublicIp.Equal(planAllocatePublicIp) {
		resp.PlanValue = types.ListUnknown(types.ObjectType{AttrTypes: networks.ReadVirtualMachineNetworkDataResponseTF{}.AttrTypes()})
		return
	}

	// Otherwise, preserve the state value (like UseStateForUnknown)
	resp.PlanValue = req.StateValue
}

// ConfigurationPlanModifier marks configuration as unknown when size_id changes
type ConfigurationPlanModifier struct{}

func (m ConfigurationPlanModifier) Description(_ context.Context) string {
	return "Marks configuration as unknown when size_id changes"
}

func (m ConfigurationPlanModifier) MarkdownDescription(_ context.Context) string {
	return "Marks configuration as unknown when size_id changes"
}

func (m ConfigurationPlanModifier) PlanModifyMap(ctx context.Context, req planmodifier.MapRequest, resp *planmodifier.MapResponse) {
	// If the resource is being created, leave it unknown
	if req.StateValue.IsNull() {
		return
	}

	// Get size_id from both state and plan
	var stateSizeId, planSizeId types.String
	req.State.GetAttribute(ctx, path.Root("size_id"), &stateSizeId)
	req.Plan.GetAttribute(ctx, path.Root("size_id"), &planSizeId)

	// If size_id is changing, mark configuration as unknown
	if !stateSizeId.Equal(planSizeId) {
		resp.PlanValue = types.MapUnknown(types.StringType)
		return
	}

	// Otherwise, preserve the state value (like UseStateForUnknown)
	resp.PlanValue = req.StateValue
}

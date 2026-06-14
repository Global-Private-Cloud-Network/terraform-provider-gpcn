package virtualmachines

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// publicIpPlanModifier marks public_ip as unknown when allocate_public_ip changes
type PublicIpPlanModifier struct{}

func (m PublicIpPlanModifier) Description(_ context.Context) string {
	return "Marks public_ip as unknown when allocate_public_ip changes"
}

func (m PublicIpPlanModifier) MarkdownDescription(_ context.Context) string {
	return "Marks public_ip as unknown when allocate_public_ip changes"
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

	// If allocate_public_ip is changing, mark public_ip as unknown
	if !stateAllocatePublicIp.Equal(planAllocatePublicIp) {
		resp.PlanValue = types.StringUnknown()
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

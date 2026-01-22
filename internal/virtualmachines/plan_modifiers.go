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

// SecretsPlanModifier marks secrets as unknown when display_secrets changes
type SecretsPlanModifier struct{}

func (m SecretsPlanModifier) Description(_ context.Context) string {
	return "Marks secrets as unknown when display_secrets changes"
}

func (m SecretsPlanModifier) MarkdownDescription(_ context.Context) string {
	return "Marks secrets as unknown when display_secrets changes"
}

func (m SecretsPlanModifier) PlanModifyMap(ctx context.Context, req planmodifier.MapRequest, resp *planmodifier.MapResponse) {
	// If the resource is being created, leave it unknown
	if req.StateValue.IsNull() {
		return
	}

	// Get display_secrets from both state and plan
	var stateDisplaySecrets, planDisplaySecrets types.Bool
	req.State.GetAttribute(ctx, path.Root("display_secrets"), &stateDisplaySecrets)
	req.Plan.GetAttribute(ctx, path.Root("display_secrets"), &planDisplaySecrets)

	// If display_secrets is changing, mark secrets as unknown
	if !stateDisplaySecrets.Equal(planDisplaySecrets) {
		resp.PlanValue = types.MapUnknown(types.StringType)
		return
	}

	// Otherwise, preserve the state value (like UseStateForUnknown)
	resp.PlanValue = req.StateValue
}

package virtualmachines

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
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

// ConfigurationPlanModifier marks configuration as unknown when size changes
type ConfigurationPlanModifier struct{}

func (m ConfigurationPlanModifier) Description(_ context.Context) string {
	return "Marks configuration as unknown when size changes"
}

func (m ConfigurationPlanModifier) MarkdownDescription(_ context.Context) string {
	return "Marks configuration as unknown when size changes"
}

func (m ConfigurationPlanModifier) PlanModifyMap(ctx context.Context, req planmodifier.MapRequest, resp *planmodifier.MapResponse) {
	// If the resource is being created, leave it unknown
	if req.StateValue.IsNull() {
		return
	}

	// Get size from both state and plan
	var stateSize, planSize types.Object
	req.State.GetAttribute(ctx, path.Root("size"), &stateSize)
	req.Plan.GetAttribute(ctx, path.Root("size"), &planSize)

	// If size is changing, mark configuration as unknown
	if !stateSize.Equal(planSize) {
		resp.PlanValue = types.MapUnknown(types.StringType)
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

// SizePlanModifier requires replacement if category changes or tier decreases
type SizePlanModifier struct{}

func (m SizePlanModifier) Description(_ context.Context) string {
	return "Requires replacement if the category changes or the tier decreases within the same category"
}

func (m SizePlanModifier) MarkdownDescription(_ context.Context) string {
	return "Requires replacement if the category changes or the tier decreases within the same category"
}

func (m SizePlanModifier) PlanModifyObject(ctx context.Context, req planmodifier.ObjectRequest, resp *planmodifier.ObjectResponse) {
	// If the resource is being created, no replacement needed
	if req.StateValue.IsNull() {
		return
	}

	var stateSize ResourceModelSize
	req.StateValue.As(ctx, &stateSize, basetypes.ObjectAsOptions{})
	var planSize ResourceModelSize
	req.PlanValue.As(ctx, &planSize, basetypes.ObjectAsOptions{})

	// If the category changes, require replacement
	if stateSize.Category.ValueString() != planSize.Category.ValueString() {
		resp.RequiresReplace = true
		return
	}

	// Categories are the same, check if tier is decreasing
	// Determine which tier list to use based on category
	var tierList []string
	if stateSize.Category.ValueString() == string(CategoryGeneral) {
		tierList = GeneralTiers
	} else {
		tierList = MemoryTiers
	}

	// Find the index of state and plan tiers in the list
	stateIdx := -1
	planIdx := -1
	for i, tier := range tierList {
		if tier == stateSize.Tier.ValueString() {
			stateIdx = i
		}
		if tier == planSize.Tier.ValueString() {
			planIdx = i
		}
	}

	// If either tier is not found (shouldn't happen due to validators), don't require replacement
	if stateIdx < 0 || planIdx < 0 {
		return
	}

	// Require replacement if the plan tier has a lower index (smaller size) than state tier
	resp.RequiresReplace = planIdx < stateIdx
}

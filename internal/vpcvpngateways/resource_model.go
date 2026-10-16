package vpcvpngateways

import "github.com/hashicorp/terraform-plugin-framework/types"

type ResourceModel struct {
	ID            types.String `tfsdk:"id"`
	VpcID         types.String `tfsdk:"vpc_id"`
	Kind          types.String `tfsdk:"kind"`
	Address       types.String `tfsdk:"address"`
	ReservedBlock types.String `tfsdk:"reserved_block"`
	State         types.String `tfsdk:"state"`
	FailureReason types.String `tfsdk:"failure_reason"`
}

// optionalString maps a nullable API field. A null stays null, because a
// Computed attribute that shows "" for an absent value hides the difference.
func optionalString(raw *string) types.String {
	if raw == nil {
		return types.StringNull()
	}
	return types.StringValue(*raw)
}

// MapGatewayToModel writes every attribute the API reports. The gateway has
// one configurable attribute, vpc_id, and the API's vpcId is what Create and
// Read already hold, so the configured value stays. Every other attribute is
// Computed and comes from the response, null where the API sends null.
func MapGatewayToModel(response *ApiVpnGateway, model ResourceModel) ResourceModel {
	model.ID = types.StringValue(response.ID)
	if model.VpcID.IsNull() || model.VpcID.IsUnknown() {
		model.VpcID = types.StringValue(response.VpcID)
	}
	model.Kind = types.StringValue(response.Kind)
	model.Address = optionalString(response.Address)
	model.ReservedBlock = optionalString(response.ReservedBlock)
	model.State = types.StringValue(response.State)
	model.FailureReason = optionalString(response.FailureReason)
	return model
}

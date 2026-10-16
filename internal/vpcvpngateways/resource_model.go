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

// optionalString keeps a null as null. An empty string would hide the difference.
func optionalString(raw *string) types.String {
	if raw == nil {
		return types.StringNull()
	}
	return types.StringValue(*raw)
}

// MapGatewayToModel keeps the configured vpc_id and takes every other
// attribute from the response.
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

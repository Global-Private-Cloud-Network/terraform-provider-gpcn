package vpcsubnets

import (
	"time"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

type ResourceModel struct {
	ID               types.String `tfsdk:"id"`
	VpcID            types.String `tfsdk:"vpc_id"`
	Name             types.String `tfsdk:"name"`
	Description      types.String `tfsdk:"description"`
	CIDR             types.String `tfsdk:"cidr"`
	Prefix           types.Int64  `tfsdk:"prefix"`
	NsgID            types.String `tfsdk:"nsg_id"`
	NsgName          types.String `tfsdk:"nsg_name"`
	State            types.String `tfsdk:"state"`
	AttachedNicCount types.Int64  `tfsdk:"attached_nic_count"`
	FailureReason    types.String `tfsdk:"failure_reason"`
	CreatedTime      types.String `tfsdk:"created_time"`
	LastUpdated      types.String `tfsdk:"last_updated"`
}

// formatTimestamp renders an API timestamp in the house format. A timestamp the
// API sends in another shape reads as "unknown" rather than failing the apply.
func formatTimestamp(raw string) types.String {
	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return types.StringValue("unknown")
	}
	return types.StringValue(parsed.Format(time.RFC850))
}

// optionalString maps a nullable API field. A null stays null, because a
// Computed attribute that shows "" for an absent value hides the difference.
func optionalString(raw *string) types.String {
	if raw == nil {
		return types.StringNull()
	}
	return types.StringValue(*raw)
}

// isUnset reports whether the model has no value the caller chose. An import
// leaves a null, and a Computed attribute reaches Create as unknown.
func isUnset(value types.String) bool {
	return value.IsNull() || value.IsUnknown()
}

// MapSubnetResponseToModel writes the Computed attributes and fills the
// configurable ones only when the caller chose no value, which is what an
// import and a Create both leave behind. A configured CIDR must survive:
// reconciling a drifted one would destroy a subnet that can hold live
// interfaces.
func MapSubnetResponseToModel(response *ApiSubnet, model ResourceModel) ResourceModel {
	model.ID = types.StringValue(response.ID)
	model.State = types.StringValue(response.State)
	model.NsgName = optionalString(response.NsgName)
	model.AttachedNicCount = types.Int64Value(response.AttachedNicCount)
	model.FailureReason = optionalString(response.FailureReason)
	model.CreatedTime = formatTimestamp(response.CreatedAt)
	model.LastUpdated = formatTimestamp(response.UpdatedAt)

	if isUnset(model.Name) {
		model.Name = types.StringValue(response.Name)
	}
	if isUnset(model.Description) {
		// The API stores a null description. The schema defaults to the empty
		// string, so an import must land on the same value a create does.
		if response.Description == nil {
			model.Description = types.StringValue("")
		} else {
			model.Description = types.StringValue(*response.Description)
		}
	}
	if isUnset(model.CIDR) {
		model.CIDR = types.StringValue(response.CIDR)
	}
	if isUnset(model.NsgID) {
		model.NsgID = types.StringValue(response.NsgID)
	}

	return model
}

// RefreshSubnetModelFromResponse shows the two changes Terraform reconciles in
// place: a rename, and a rebind to another security group. The CIDR is the
// allocator's reservation and the prefix is request-only, so neither refreshes.
// Read calls this after the mapper; Create and Update keep the planned values.
func RefreshSubnetModelFromResponse(response *ApiSubnet, model ResourceModel) ResourceModel {
	if response.Name != "" {
		model.Name = types.StringValue(response.Name)
	}
	if response.NsgID != "" {
		model.NsgID = types.StringValue(response.NsgID)
	}
	return model
}

package vpcpublicips

import (
	"time"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

type ResourceModel struct {
	ID               types.String `tfsdk:"id"`
	VpcID            types.String `tfsdk:"vpc_id"`
	IPAddress        types.String `tfsdk:"ip_address"`
	State            types.String `tfsdk:"state"`
	Held             types.Bool   `tfsdk:"held"`
	VirtualMachineID types.String `tfsdk:"virtual_machine_id"`
	FailureReason    types.String `tfsdk:"failure_reason"`
	CreatedTime      types.String `tfsdk:"created_time"`
	LastUpdated      types.String `tfsdk:"last_updated"`
}

type AttachmentResourceModel struct {
	ID               types.String `tfsdk:"id"`
	VpcID            types.String `tfsdk:"vpc_id"`
	PublicIpID       types.String `tfsdk:"public_ip_id"`
	NicID            types.String `tfsdk:"nic_id"`
	VirtualMachineID types.String `tfsdk:"virtual_machine_id"`
}

// optionalString keeps a null the API sent as a null in state. An empty string
// would read as an address, a reason or a machine the platform never named.
func optionalString(value *string) types.String {
	if value == nil {
		return types.StringNull()
	}
	return types.StringValue(*value)
}

// timestamp renders an API timestamp in the provider's RFC850 convention.
func timestamp(raw string) types.String {
	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return types.StringValue("unknown")
	}
	return types.StringValue(parsed.Format(time.RFC850))
}

// MapPublicIpResponseToModel fills every computed attribute from the listing
// row. The VPC ID is the resource's only input and keeps the configured value.
func MapPublicIpResponseToModel(publicIp *PublicIp, model ResourceModel) ResourceModel {
	model.ID = types.StringValue(publicIp.Id)
	model.IPAddress = optionalString(publicIp.IpAddress)
	model.State = types.StringValue(publicIp.State)
	model.VirtualMachineID = optionalString(publicIp.VirtualMachineId)
	model.FailureReason = optionalString(publicIp.FailureReason)
	model.CreatedTime = timestamp(publicIp.CreatedAt)
	model.LastUpdated = timestamp(publicIp.UpdatedAt)
	// The platform derives held the same way: an address is held when no machine
	// holds it. The wire field is therefore a second copy of this one fact.
	model.Held = types.BoolValue(publicIp.VirtualMachineId == nil)
	return model
}

// MapAcquiredIdToModel records the id the acquire answered with before the job
// finished. Nothing about the row is read yet, so every other attribute is null.
func MapAcquiredIdToModel(publicIpID string, model ResourceModel) ResourceModel {
	model.ID = types.StringValue(publicIpID)
	model.IPAddress = types.StringNull()
	model.State = types.StringNull()
	model.Held = types.BoolNull()
	model.VirtualMachineID = types.StringNull()
	model.FailureReason = types.StringNull()
	model.CreatedTime = types.StringNull()
	model.LastUpdated = types.StringNull()
	return model
}

// MapAttachedIdToAttachmentModel records the binding the attach made. The
// address itself is not read yet, so the machine it serves stays null.
func MapAttachedIdToAttachmentModel(model AttachmentResourceModel) AttachmentResourceModel {
	model.ID = model.PublicIpID
	model.VirtualMachineID = types.StringNull()
	return model
}

// MapPublicIpResponseToAttachmentModel records the machine the address now
// serves. The attachment carries no other state of its own.
func MapPublicIpResponseToAttachmentModel(publicIp *PublicIp, model AttachmentResourceModel) AttachmentResourceModel {
	model.ID = types.StringValue(publicIp.Id)
	model.VirtualMachineID = optionalString(publicIp.VirtualMachineId)
	return model
}

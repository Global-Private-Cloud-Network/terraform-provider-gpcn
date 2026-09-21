package l2segments

import (
	"time"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

type ResourceModel struct {
	ID               types.String `tfsdk:"id"`
	Name             types.String `tfsdk:"name"`
	DatacenterId     types.String `tfsdk:"datacenter_id"`
	Description      types.String `tfsdk:"description"`
	State            types.String `tfsdk:"state"`
	Offering         types.String `tfsdk:"offering"`
	FailureReason    types.String `tfsdk:"failure_reason"`
	DatacenterName   types.String `tfsdk:"datacenter_name"`
	AttachedNicCount types.Int64  `tfsdk:"attached_nic_count"`
	CreatedTime      types.String `tfsdk:"created_time"`
	LastUpdated      types.String `tfsdk:"last_updated"`
}

// nullableString keeps a platform null as a Terraform null. A null failure reason
// means the segment never failed, which is not the same as an empty explanation.
func nullableString(raw *string) types.String {
	if raw == nil {
		return types.StringNull()
	}
	return types.StringValue(*raw)
}

// descriptionValue normalizes an absent description. The schema defaults the
// attribute to the empty string, and an adopted segment stores null. A plan
// after an import would otherwise show null -> "" on every such segment.
func descriptionValue(raw *string) types.String {
	if raw == nil {
		return types.StringValue("")
	}
	return types.StringValue(*raw)
}

// formatTimestamp renders an API timestamp in the house format.
func formatTimestamp(raw string) types.String {
	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return types.StringValue("unknown")
	}
	return types.StringValue(parsed.Format(time.RFC850))
}

// MapL2SegmentResponseToModel writes the computed attributes. It fills the
// configurable ones only when they are null. An import starts from that state,
// and Create and Update keep the planned values.
func MapL2SegmentResponseToModel(response *readL2SegmentResponse, model ResourceModel) ResourceModel {
	model.ID = types.StringValue(response.Data.ID)
	model.State = types.StringValue(response.Data.State)
	model.Offering = types.StringValue(response.Data.Offering)
	model.FailureReason = nullableString(response.Data.FailureReason)
	model.DatacenterName = nullableString(response.Data.DatacenterName)
	model.AttachedNicCount = types.Int64Value(response.Data.AttachedNicCount)
	model.CreatedTime = formatTimestamp(response.Data.CreatedAt)
	model.LastUpdated = formatTimestamp(response.Data.UpdatedAt)

	if model.Name.IsNull() {
		model.Name = types.StringValue(response.Data.Name)
	}
	if model.DatacenterId.IsNull() {
		model.DatacenterId = types.StringValue(response.Data.DatacenterId)
	}
	if model.Description.IsNull() {
		model.Description = descriptionValue(response.Data.Description)
	}

	return model
}

// RefreshL2SegmentModelFromResponse shows an out-of-band edit. The name and the
// description are the only attributes the platform reconciles in place, through
// the update verb. The datacenter has no update verb. A refreshed drift there
// would plan a replacement of a carrier that live traffic uses.
func RefreshL2SegmentModelFromResponse(response *readL2SegmentResponse, model ResourceModel) ResourceModel {
	model.Name = types.StringValue(response.Data.Name)
	model.Description = descriptionValue(response.Data.Description)
	return model
}

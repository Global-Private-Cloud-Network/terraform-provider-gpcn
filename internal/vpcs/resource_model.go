package vpcs

import (
	"context"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

type ResourceModel struct {
	ID                 types.String `tfsdk:"id"`
	Name               types.String `tfsdk:"name"`
	DatacenterId       types.String `tfsdk:"datacenter_id"`
	CIDR               types.String `tfsdk:"cidr"`
	Description        types.String `tfsdk:"description"`
	DNSNameservers     types.List   `tfsdk:"dns_nameservers"`
	AcknowledgeOverlap types.Bool   `tfsdk:"acknowledge_overlap"`
	Status             types.String `tfsdk:"status"`
	EgressIp           types.String `tfsdk:"egress_ip"`
	FailureReason      types.String `tfsdk:"failure_reason"`
	ActiveJobId        types.String `tfsdk:"active_job_id"`
	CreatedTime        types.String `tfsdk:"created_time"`
	LastUpdated        types.String `tfsdk:"last_updated"`
}

// MapCreatedVpcToModel fills the model from the row the 202 carries. The id
// reaches state from here, before the create job is polled.
func MapCreatedVpcToModel(ctx context.Context, response *createVpcResponse, model ResourceModel) ResourceModel {
	return mapVpcPayloadToModel(ctx, response.Data.Vpc, model)
}

func MapVpcResponseToModel(ctx context.Context, response *readVpcResponse, model ResourceModel) ResourceModel {
	return mapVpcPayloadToModel(ctx, response.Data, model)
}

// RefreshVpcModelFromResponse shows a rename, which Terraform reconciles in
// place. Every other configured attribute requires replacement, so an
// out-of-band change to one must not reach the plan.
func RefreshVpcModelFromResponse(response *readVpcResponse, model ResourceModel) ResourceModel {
	if response.Data.Name != "" {
		model.Name = types.StringValue(response.Data.Name)
	}
	return model
}

func mapVpcPayloadToModel(_ context.Context, payload vpcPayload, model ResourceModel) ResourceModel {
	model.ID = types.StringValue(payload.ID)
	model.Status = types.StringValue(payload.Status)
	model.EgressIp = nullableString(payload.EgressIp)
	model.FailureReason = nullableString(payload.FailureReason)
	model.ActiveJobId = nullableString(payload.ActiveJobId)
	model.CreatedTime = rfc850Timestamp(payload.CreatedAt)
	model.LastUpdated = rfc850Timestamp(payload.UpdatedAt)

	// The schema defaults the description to the empty string. A null in state
	// would plan a change on every apply.
	model.Description = types.StringValue(derefString(payload.Description))

	if model.Name.IsNull() || model.Name.IsUnknown() {
		model.Name = types.StringValue(payload.Name)
	}
	if model.CIDR.IsNull() || model.CIDR.IsUnknown() {
		model.CIDR = types.StringValue(payload.CIDR)
	}
	if model.DatacenterId.IsNull() || model.DatacenterId.IsUnknown() {
		model.DatacenterId = types.StringValue(payload.Datacenter.ID)
	}
	if model.DNSNameservers.IsNull() || model.DNSNameservers.IsUnknown() {
		model.DNSNameservers = nameserverList(payload.DnsNameservers)
	}

	return model
}

// The platform resolves its own nameservers when the request omits them, and
// the list is frozen at create. Only an unset value takes the API's answer.
func nameserverList(nameservers []string) types.List {
	if len(nameservers) == 0 {
		return types.ListNull(types.StringType)
	}
	elements := make([]attr.Value, len(nameservers))
	for i, nameserver := range nameservers {
		elements[i] = types.StringValue(nameserver)
	}
	list, diags := types.ListValue(types.StringType, elements)
	if diags.HasError() {
		return types.ListNull(types.StringType)
	}
	return list
}

func rfc850Timestamp(timestamp string) types.String {
	parsed, err := time.Parse(time.RFC3339, timestamp)
	if err != nil {
		return types.StringValue("unknown")
	}
	return types.StringValue(parsed.Format(time.RFC850))
}

func nullableString(value *string) types.String {
	if value == nil {
		return types.StringNull()
	}
	return types.StringValue(*value)
}

func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

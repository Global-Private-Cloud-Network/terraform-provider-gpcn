package networks

import (
	"context"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

type ResourceModel struct {
	ID               types.String `tfsdk:"id"`
	Name             types.String `tfsdk:"name"`
	Description      types.String `tfsdk:"description"`
	CreatedTime      types.String `tfsdk:"created_time"`
	LastUpdated      types.String `tfsdk:"last_updated"`
	SNAT             types.String `tfsdk:"snat"`
	CIDRBlock        types.String `tfsdk:"cidr_block"`
	Gateway          types.String `tfsdk:"gateway"`
	ConnectedVMs     types.String `tfsdk:"connected_vms"`
	NetworkType      types.String `tfsdk:"network_type"`
	DatacenterId     types.String `tfsdk:"datacenter_id"`
	Location         types.Map    `tfsdk:"location"`
	DNSServers       types.String `tfsdk:"dns_servers"`
	DHCPStartAddress types.String `tfsdk:"dhcp_start_address"`
	DHCPEndAddress   types.String `tfsdk:"dhcp_end_address"`
}

// Update the plan or state with new values from the GET response
func MapNetworkResponseToModel(ctx context.Context, response *readNetworkResponse, model ResourceModel) ResourceModel {
	model.ID = types.StringValue(response.Data.ID)
	model.Description = types.StringValue(response.Data.Description)
	model.SNAT = types.StringValue(response.Data.SNAT)
	model.CIDRBlock = types.StringValue(response.Data.CIDRBlock)
	model.Gateway = types.StringValue(response.Data.Gateway)
	model.ConnectedVMs = types.StringValue(response.Data.ConnectedVMs)
	model.DNSServers = types.StringValue(response.Data.DNSServers)

	// Construct time entries
	createdTime, err := time.Parse(time.RFC3339, response.Data.CreatedAt)
	if err != nil {
		model.CreatedTime = types.StringValue("unknown")
	} else {
		model.CreatedTime = types.StringValue(createdTime.Format(time.RFC850))
	}
	updatedTime, err := time.Parse(time.RFC3339, response.Data.UpdatedAt)
	if err != nil {
		model.LastUpdated = types.StringValue("unknown")
	} else {
		model.LastUpdated = types.StringValue(updatedTime.Format(time.RFC850))
	}

	// Construct the location object
	model.Location, _ = types.MapValueFrom(ctx, types.StringType, map[string]string{
		"country":    response.Data.Datacenter.Country,
		"region":     response.Data.Datacenter.Region,
		"datacenter": response.Data.Datacenter.Name,
	})

	// Construct the DHCPStart and EndAddresses
	isStandardNetwork := model.NetworkType == types.StringValue("standard")
	if isStandardNetwork {
		model.DHCPStartAddress = types.StringValue(response.Data.AllocationPools[0].Start)
		model.DHCPEndAddress = types.StringValue(response.Data.AllocationPools[0].End)
	}

	// If model doesn't already have these populated, set them
	model = setModelValuesNotPresent(response, model)

	return model
}

func setModelValuesNotPresent(response *readNetworkResponse, model ResourceModel) ResourceModel {
	if model.DatacenterId.IsNull() {
		model.DatacenterId = types.StringValue(response.Data.Datacenter.ID)
	}
	if model.Name.IsNull() {
		model.Name = types.StringValue(response.Data.Name)
	}
	if model.NetworkType.IsNull() {
		model.NetworkType = types.StringValue(response.Data.NetworkType)
	}
	if model.DHCPStartAddress.IsNull() && len(response.Data.AllocationPools) > 0 {
		model.DHCPStartAddress = types.StringValue(response.Data.AllocationPools[0].Start)
	}
	if model.DHCPEndAddress.IsNull() && len(response.Data.AllocationPools) > 0 {
		model.DHCPEndAddress = types.StringValue(response.Data.AllocationPools[0].End)
	}
	return model
}

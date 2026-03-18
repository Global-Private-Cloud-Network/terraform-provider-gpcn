package networks

import (
	"context"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
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
	DNSServers       types.List   `tfsdk:"dns_servers"`
	DHCPStartAddress types.String `tfsdk:"dhcp_start_address"`
	DHCPEndAddress   types.String `tfsdk:"dhcp_end_address"`
}

// parseDNSServers converts the API's comma-separated DNS servers string into a types.List.
func parseDNSServers(raw string) types.List {
	if raw == "" {
		return types.ListValueMust(types.StringType, []attr.Value{})
	}
	parts := strings.Split(raw, ", ")
	elements := make([]attr.Value, len(parts))
	for i, p := range parts {
		elements[i] = types.StringValue(strings.TrimSpace(p))
	}
	list, diags := types.ListValue(types.StringType, elements)
	if diags.HasError() {
		return types.ListNull(types.StringType)
	}
	return list
}

// getDHCPAddresses safely extracts DHCP start and end addresses from allocation pools.
// Returns empty strings if no allocation pools are available.
func getDHCPAddresses(response *readNetworkResponse) (start, end string) {
	if len(response.Data.AllocationPools) > 0 {
		return response.Data.AllocationPools[0].Start, response.Data.AllocationPools[0].End
	}
	return "", ""
}

// Update the plan or state with new values from the GET response
func MapNetworkResponseToModel(ctx context.Context, response *readNetworkResponse, model ResourceModel) ResourceModel {
	model.ID = types.StringValue(response.Data.ID)
	model.Description = types.StringValue(response.Data.Description)
	model.SNAT = types.StringValue(response.Data.SNAT)
	model.CIDRBlock = types.StringValue(response.Data.CIDRBlock)
	model.Gateway = types.StringValue(response.Data.Gateway)
	model.ConnectedVMs = types.StringValue(response.Data.ConnectedVMs)
	model.DNSServers = parseDNSServers(response.Data.DNSServers)

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
	var diags diag.Diagnostics
	model.Location, diags = types.MapValueFrom(ctx, types.StringType, map[string]string{
		"country":    response.Data.Datacenter.Country,
		"region":     response.Data.Datacenter.Region,
		"datacenter": response.Data.Datacenter.Name,
	})
	if diags.HasError() {
		// Log the error but continue with a null value
		model.Location = types.MapNull(types.StringType)
	}

	// Construct the DHCPStart and EndAddresses for standard networks
	isStandardNetwork := model.NetworkType == types.StringValue("standard")
	if isStandardNetwork {
		start, end := getDHCPAddresses(response)
		if start != "" {
			model.DHCPStartAddress = types.StringValue(start)
		}
		if end != "" {
			model.DHCPEndAddress = types.StringValue(end)
		}
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
	start, end := getDHCPAddresses(response)
	if model.DHCPStartAddress.IsNull() && start != "" {
		model.DHCPStartAddress = types.StringValue(start)
	}
	if model.DHCPEndAddress.IsNull() && end != "" {
		model.DHCPEndAddress = types.StringValue(end)
	}
	return model
}

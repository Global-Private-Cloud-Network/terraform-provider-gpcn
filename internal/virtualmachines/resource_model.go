package virtualmachines

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"terraform-provider-gpcn/internal/client"
	"terraform-provider-gpcn/internal/networks"
	"terraform-provider-gpcn/internal/virtualmachineimages"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
)

type ResourceModel struct {
	ID               types.String `tfsdk:"id"`
	Name             types.String `tfsdk:"name"`
	DatacenterId     types.String `tfsdk:"datacenter_id"`
	Size             types.Object `tfsdk:"size"`
	ImageId          types.String `tfsdk:"image_id"`
	CreatedTime      types.String `tfsdk:"created_time"`
	LastUpdated      types.String `tfsdk:"last_updated"`
	Location         types.Map    `tfsdk:"location"`
	Configuration    types.Map    `tfsdk:"configuration"`
	AllocatePublicIp types.Bool   `tfsdk:"allocate_public_ip"`
	PublicIp         types.String `tfsdk:"public_ip"`
	NetworkIds       types.List   `tfsdk:"network_ids"`
	VolumeIds        types.List   `tfsdk:"volume_ids"`
	NetworkHotplug   types.Bool   `tfsdk:"network_hotplug"`
	InitialAuth      types.Object `tfsdk:"initial_auth"`
	ResourceGroupId  types.String `tfsdk:"resource_group_id"`
}

type ResourceModelInitialAuth struct {
	SshKeyId types.String `tfsdk:"ssh_key_id"`
	Username types.String `tfsdk:"username"`
	Password types.String `tfsdk:"password"`
}

func (o ResourceModelInitialAuth) AttrTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"ssh_key_id": types.StringType,
		"username":   types.StringType,
		"password":   types.StringType,
	}
}

type ResourceModelSize struct {
	Category types.String `tfsdk:"category"`
	Name     types.String `tfsdk:"name"`
}

func (o ResourceModelSize) AttrTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"category": types.StringType,
		"name":     types.StringType,
	}
}

// MapVirtualMachineResponseToModel updates the plan or state with new values from the GET response.
// Returns the updated model and any diagnostics encountered during mapping.
func MapVirtualMachineResponseToModel(ctx context.Context, gpcnClient *client.GpcnClient, response *ReadVirtualMachinesResponse, model ResourceModel) (ResourceModel, diag.Diagnostics) {
	var allDiags diag.Diagnostics

	model.ID = types.StringValue(response.Data.ID)
	model.NetworkHotplug = types.BoolValue(response.Data.NetworkHotplug == 1)

	if response.Data.ResourceGroupId != "" {
		model.ResourceGroupId = types.StringValue(response.Data.ResourceGroupId)
	} else {
		model.ResourceGroupId = types.StringNull()
	}

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
		allDiags.Append(diags...)
		model.Location = types.MapNull(types.StringType)
	}

	// Construct the configuration object
	model.Configuration, diags = types.MapValueFrom(ctx, types.StringType, map[string]string{
		"name":         response.Data.Configuration.Name,
		"cpu":          strconv.FormatInt(response.Data.Configuration.CPU, 10) + " cores",
		"ram":          strconv.FormatInt(response.Data.Configuration.RAM, 10) + " GB",
		"base_storage": strconv.FormatInt(response.Data.Configuration.Disk, 10) + " GB",
		"sku_id":       response.Data.Configuration.SkuId,
		"sku_code":     response.Data.Configuration.SkuCode,
	})
	if diags.HasError() {
		allDiags.Append(diags...)
		model.Configuration = types.MapNull(types.StringType)
	}

	// If model doesn't already have these populated, set them
	model, diags = setModelValuesNotPresent(ctx, gpcnClient, response, model)
	allDiags.Append(diags...)

	return model, allDiags
}

func setModelValuesNotPresent(ctx context.Context, gpcnClient *client.GpcnClient, response *ReadVirtualMachinesResponse, model ResourceModel) (ResourceModel, diag.Diagnostics) {
	var allDiags diag.Diagnostics

	if model.DatacenterId.IsNull() {
		model.DatacenterId = types.StringValue(response.Data.Datacenter.ID)
	}
	var imageIdDiags diag.Diagnostics
	model.ImageId, imageIdDiags = resolveImageId(gpcnClient, ctx, model.ImageId, model.DatacenterId.ValueString(), response)
	allDiags.Append(imageIdDiags...)
	if model.Name.IsNull() {
		model.Name = types.StringValue(response.Data.Name)
	}
	if model.Size.IsNull() {
		size := ResourceModelSize{}
		var sizeDiags diag.Diagnostics
		model.Size, sizeDiags = types.ObjectValueFrom(ctx, size.AttrTypes(), ResourceModelSize{
			Category: types.StringValue(response.Data.Configuration.CategoryCode),
			Name:     types.StringValue(response.Data.Configuration.Name),
		})
		if sizeDiags.HasError() {
			allDiags.Append(sizeDiags...)
			model.Size = types.ObjectNull(size.AttrTypes())
		}
	}

	var networkDiags diag.Diagnostics
	model, networkDiags = setNetworkModelValuesNotPresent(ctx, gpcnClient, response.Data.ID, model)
	allDiags.Append(networkDiags...)

	// Populate auth from response. On import, auth is null and must be constructed from the response.
	var authDiags diag.Diagnostics
	model.InitialAuth, authDiags = populateAuth(ctx, model.InitialAuth, response)
	allDiags.Append(authDiags...)

	return model, allDiags
}

// Fill auth fields from the API response. On import, current is null
func populateAuth(ctx context.Context, current types.Object, response *ReadVirtualMachinesResponse) (types.Object, diag.Diagnostics) {
	if current.IsUnknown() {
		return current, nil
	}

	var auth ResourceModelInitialAuth
	if current.IsNull() {
		auth = ResourceModelInitialAuth{
			Username: types.StringValue(response.Data.Username),
			SshKeyId: types.StringNull(),
			Password: types.StringNull(),
		}
		if response.Data.SshKeyId != "" {
			auth.SshKeyId = types.StringValue(response.Data.SshKeyId)
		}
	} else {
		if diags := current.As(ctx, &auth, basetypes.ObjectAsOptions{}); diags.HasError() {
			return current, diags
		}
		if auth.Username.IsNull() && response.Data.Username != "" {
			auth.Username = types.StringValue(response.Data.Username)
		}
	}

	return types.ObjectValueFrom(ctx, auth.AttrTypes(), auth)
}

// Derive the image_id from the image name. On import, current is null
func resolveImageId(gpcnClient *client.GpcnClient, ctx context.Context, current types.String, datacenterId string, response *ReadVirtualMachinesResponse) (types.String, diag.Diagnostics) {
	var diags diag.Diagnostics

	if !current.IsNull() {
		return current, diags
	}

	if datacenterId == "" {
		datacenterId = response.Data.Datacenter.ID
	}

	images, err := virtualmachineimages.FetchImages(gpcnClient, ctx, datacenterId)
	if err != nil {
		diags.AddWarning(
			"Unable to resolve image ID",
			fmt.Sprintf("Failed to fetch images for datacenter %s to resolve image name %q: %s", datacenterId, response.Data.Image, err.Error()),
		)
		return types.StringNull(), diags
	}

	for _, img := range images {
		if img.Name == response.Data.Image {
			return types.StringValue(img.ID), diags
		}
	}

	diags.AddWarning(
		"Unable to resolve image ID",
		fmt.Sprintf("Image %q was not found in the virtual machine images list for datacenter %s. The image_id field will remain empty.", response.Data.Image, datacenterId),
	)
	return types.StringNull(), diags
}

func setNetworkModelValuesNotPresent(ctx context.Context, gpcnClient *client.GpcnClient, virtualMachineID string, model ResourceModel) (ResourceModel, diag.Diagnostics) {
	var allDiags diag.Diagnostics

	// Set the base public IP, might be replaced later
	model.PublicIp = types.StringValue("")
	// Fetch network interfaces for the virtual machine
	networkInterfaces, err := networks.GetNetworkInterfaces(gpcnClient, ctx, virtualMachineID)
	if err != nil {
		allDiags.AddWarning(
			"Unable to fetch network interfaces",
			fmt.Sprintf("Failed to fetch network interfaces for VM %s: %s", virtualMachineID, err.Error()),
		)
		return model, allDiags
	}

	if len(networkInterfaces) > 0 {
		// Extract network IDs from network interfaces
		var networkIds []string
		hasPublicIp := false
		for _, iface := range networkInterfaces {
			networkIds = append(networkIds, iface.NetworkID.ValueString())
			// Check if this interface has a public IP
			if !iface.PublicIP.IsNull() && iface.PublicIP.ValueString() != "" {
				hasPublicIp = true
				// If it does, set the model's public IP here
				model.PublicIp = iface.PublicIP
			}
		}
		// Set the network IDs in the model
		if model.NetworkIds.IsNull() {
			var networkDiags diag.Diagnostics
			model.NetworkIds, networkDiags = types.ListValueFrom(ctx, types.StringType, networkIds)
			if networkDiags.HasError() {
				allDiags.Append(networkDiags...)
				model.NetworkIds = types.ListNull(types.StringType)
			}
		}

		// Set AllocatePublicIp if it's currently null
		if model.AllocatePublicIp.IsNull() {
			model.AllocatePublicIp = types.BoolValue(hasPublicIp)
		}
	}
	return model, allDiags
}

package virtualmachines

import (
	"context"
	"fmt"
	"slices"
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
	ID                types.String `tfsdk:"id"`
	Name              types.String `tfsdk:"name"`
	DatacenterId      types.String `tfsdk:"datacenter_id"`
	SizeId            types.String `tfsdk:"size_id"`
	ImageId           types.String `tfsdk:"image_id"`
	CreatedTime       types.String `tfsdk:"created_time"`
	LastUpdated       types.String `tfsdk:"last_updated"`
	Location          types.Map    `tfsdk:"location"`
	Configuration     types.Map    `tfsdk:"configuration"`
	AllocatePublicIp  types.Bool   `tfsdk:"allocate_public_ip"`
	PublicIp          types.String `tfsdk:"public_ip"`
	PublicIpId        types.String `tfsdk:"public_ip_id"`
	SubnetId          types.String `tfsdk:"subnet_id"`
	L2SegmentIds      types.List   `tfsdk:"l2_segment_ids"`
	NetworkInterfaces types.List   `tfsdk:"network_interfaces"`
	NetworkHotplug    types.Bool   `tfsdk:"network_hotplug"`
	InitialAuth       types.Object `tfsdk:"initial_auth"`
	ResourceGroupId   types.String `tfsdk:"resource_group_id"`
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

	// Fill only the values the model does not already carry.
	model, diags = setModelValuesNotPresent(ctx, gpcnClient, response, model)
	allDiags.Append(diags...)

	return model, allDiags
}

// Read calls this after MapVirtualMachineResponseToModel, so an out-of-band change shows
// as drift. Create and Update must not call it, because a lagging API then overwrites the
// planned values. Only the name refreshes, because Terraform reconciles a rename in place.
// A refresh of any other attribute plans a replacement or discards a configured value.
// An empty name is an omission, not a rename.
func RefreshVirtualMachineModelFromResponse(response *ReadVirtualMachinesResponse, model ResourceModel) ResourceModel {
	if response.Data.Name != "" {
		model.Name = types.StringValue(response.Data.Name)
	}
	return model
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
	if model.SizeId.IsNull() {
		model.SizeId = types.StringValue(response.Data.Configuration.SkuId)
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

// ReleasesAcquiredAddress reports whether the destroy asks GPCN to give the address
// back. Terraform releases only an address it acquired, and only a VPC interface holds
// one. The release key also needs the vpc-public-ip:delete permission, which a legacy
// machine must not have to spend.
// Returns the decision and any diagnostics encountered while reading the interfaces.
func ReleasesAcquiredAddress(ctx context.Context, state ResourceModel) (bool, diag.Diagnostics) {
	var diags diag.Diagnostics

	if !state.AllocatePublicIp.ValueBool() {
		return false, diags
	}
	if state.NetworkInterfaces.IsNull() || state.NetworkInterfaces.IsUnknown() {
		return false, diags
	}

	var interfaces []networks.ReadVirtualMachineNetworkDataResponseTF
	diags.Append(state.NetworkInterfaces.ElementsAs(ctx, &interfaces, false)...)
	if diags.HasError() {
		return false, diags
	}

	primaryIdx := slices.IndexFunc(interfaces, func(iface networks.ReadVirtualMachineNetworkDataResponseTF) bool {
		return iface.IsPrimary.ValueBool()
	})
	if primaryIdx < 0 {
		return false, diags
	}
	return interfaces[primaryIdx].World.ValueString() == networks.NicWorldVpc, diags
}

func setNetworkModelValuesNotPresent(ctx context.Context, gpcnClient *client.GpcnClient, virtualMachineID string, model ResourceModel) (ResourceModel, diag.Diagnostics) {
	var allDiags diag.Diagnostics

	model.PublicIp = types.StringNull()
	interfaceElemType := types.ObjectType{AttrTypes: networks.ReadVirtualMachineNetworkDataResponseTF{}.AttrTypes()}
	model.NetworkInterfaces = types.ListNull(interfaceElemType)
	// Fetch network interfaces for the virtual machine
	networkInterfaces, err := networks.GetNetworkInterfaces(gpcnClient, ctx, virtualMachineID)
	if err != nil {
		allDiags.AddWarning(
			"Unable to fetch network interfaces",
			fmt.Sprintf("Failed to fetch network interfaces for VM %s: %s", virtualMachineID, err.Error()),
		)
		return model, allDiags
	}

	interfaceList, interfaceDiags := types.ListValueFrom(ctx, interfaceElemType, networkInterfaces)
	if interfaceDiags.HasError() {
		allDiags.Append(interfaceDiags...)
		model.NetworkInterfaces = types.ListNull(interfaceElemType)
	} else {
		model.NetworkInterfaces = interfaceList
	}

	primaryIdx := slices.IndexFunc(networkInterfaces, func(iface networks.ReadVirtualMachineNetworkDataResponseTF) bool {
		return iface.IsPrimary.ValueBool()
	})
	if primaryIdx >= 0 {
		primary := networkInterfaces[primaryIdx]
		// public_ip mirrors the address on the birth interface. A machine without one
		// reports null, because GPCN sends null and not an empty address.
		model.PublicIp = primary.PublicIP
		// The detail projection carries no subnet, so an import learns the birth subnet
		// here. A configured value must survive, because subnet_id requires replacement.
		if model.SubnetId.IsNull() {
			model.SubnetId = primary.VpcSubnetID
		}
		// allocate_public_ip records an intent that the API never reports. GPCN stores an
		// acquired address and a held one in the same row. An import therefore records the
		// address as held, and a destroy leaves it with the operator.
		if model.AllocatePublicIp.IsNull() {
			model.AllocatePublicIp = types.BoolValue(false)
			if model.PublicIpId.IsNull() && !primary.PublicIPID.IsNull() {
				model.PublicIpId = primary.PublicIPID
			}
		}
	}

	// The configured list holds the user's ordered intent, so only an import takes the
	// segments from the interface list.
	if model.L2SegmentIds.IsNull() {
		segmentIds := []string{}
		for _, iface := range networkInterfaces {
			if iface.World.ValueString() == networks.NicWorldL2 && !iface.L2SegmentID.IsNull() {
				segmentIds = append(segmentIds, iface.L2SegmentID.ValueString())
			}
		}
		var segmentDiags diag.Diagnostics
		model.L2SegmentIds, segmentDiags = types.ListValueFrom(ctx, types.StringType, segmentIds)
		if segmentDiags.HasError() {
			allDiags.Append(segmentDiags...)
			model.L2SegmentIds = types.ListNull(types.StringType)
		}
	}
	return model, allDiags
}

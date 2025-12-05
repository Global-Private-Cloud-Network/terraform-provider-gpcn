package virtualmachines

import (
	"context"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

type ResourceModel struct {
	ID               types.String `tfsdk:"id"`
	Name             types.String `tfsdk:"name"`
	DatacenterId     types.String `tfsdk:"datacenter_id"`
	WaitForStartup   types.Bool   `tfsdk:"wait_for_startup"`
	Size             types.String `tfsdk:"size"`
	Image            types.String `tfsdk:"image"`
	CreatedTime      types.String `tfsdk:"created_time"`
	LastUpdated      types.String `tfsdk:"last_updated"`
	Location         types.Map    `tfsdk:"location"`
	Configuration    types.Map    `tfsdk:"configuration"`
	AllocatePublicIp types.Bool   `tfsdk:"allocate_public_ip"`
	NetworkIds       types.List   `tfsdk:"network_ids"`
	VolumeIds        types.List   `tfsdk:"volume_ids"`
	AdditionalImages types.List   `tfsdk:"additional_images"`
	AdditionalSizes  types.List   `tfsdk:"additional_sizes"`
	ImageId          types.Int64  `tfsdk:"image_id"`
	SizeId           types.Int64  `tfsdk:"size_id"`
}

// Update the plan or state with new values from the GET response
func MapVirtualMachineResponseToModel(ctx context.Context, response *ReadVirtualMachinesResponse, images []VirtualMachineImagesDataResponseTF, sizes []VirtualMachineSizesDataResponseTF, model ResourceModel) ResourceModel {
	model.ID = types.StringValue(response.Data.VirtualMachine.ID)

	// Construct time entries
	createdTime, err := time.Parse(time.RFC3339, response.Data.VirtualMachine.CreatedAt)
	if err != nil {
		model.CreatedTime = types.StringValue("unknown")
	} else {
		model.CreatedTime = types.StringValue(createdTime.Format(time.RFC850))
	}
	updatedTime, err := time.Parse(time.RFC3339, response.Data.VirtualMachine.UpdatedAt)
	if err != nil {
		model.LastUpdated = types.StringValue("unknown")
	} else {
		model.LastUpdated = types.StringValue(updatedTime.Format(time.RFC850))
	}

	// Construct the location object
	model.Location, _ = types.MapValueFrom(ctx, types.StringType, map[string]string{
		"country":    response.Data.VirtualMachine.Country,
		"region":     response.Data.VirtualMachine.Region,
		"datacenter": response.Data.VirtualMachine.Datacenter,
	})

	// Construct the configuration object
	model.Configuration, _ = types.MapValueFrom(ctx, types.StringType, map[string]string{
		"name":         response.Data.VirtualMachine.Configuration,
		"cpu":          strconv.FormatInt(response.Data.VirtualMachine.CPU, 10) + " cores",
		"ram":          strconv.FormatInt(response.Data.VirtualMachine.RAM, 10) + " GB",
		"base_storage": strconv.FormatInt(response.Data.VirtualMachine.Disk, 10) + " GB",
	})

	// Construct images and sizes
	model.AdditionalImages, _ = types.ListValueFrom(ctx, types.ObjectType{AttrTypes: VirtualMachineImagesDataResponseTF{}.AttrTypes()}, images)
	model.AdditionalSizes, _ = types.ListValueFrom(ctx, types.ObjectType{AttrTypes: VirtualMachineSizesDataResponseTF{}.AttrTypes()}, sizes)

	// Find the imageId and sizeId from the objects. Not possible to be < 0
	sizeIdx := slices.IndexFunc(sizes, func(virtualMachineSize VirtualMachineSizesDataResponseTF) bool {
		return strings.EqualFold(virtualMachineSize.Name.ValueString(), model.Size.ValueString())
	})
	imageIdx := slices.IndexFunc(images, func(virtualMachineImage VirtualMachineImagesDataResponseTF) bool {
		return strings.EqualFold(virtualMachineImage.Name.ValueString(), model.Image.ValueString())
	})
	model.ImageId = images[imageIdx].ID
	model.SizeId = sizes[sizeIdx].ID

	return model
}

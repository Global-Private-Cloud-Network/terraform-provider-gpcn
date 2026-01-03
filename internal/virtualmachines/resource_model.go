package virtualmachines

import (
	"context"
	"strconv"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

type ResourceModel struct {
	ID               types.String `tfsdk:"id"`
	Name             types.String `tfsdk:"name"`
	DatacenterId     types.String `tfsdk:"datacenter_id"`
	WaitForStartup   types.Bool   `tfsdk:"wait_for_startup"`
	Size             types.Object `tfsdk:"size"`
	Image            types.String `tfsdk:"image"`
	CreatedTime      types.String `tfsdk:"created_time"`
	LastUpdated      types.String `tfsdk:"last_updated"`
	Location         types.Map    `tfsdk:"location"`
	Configuration    types.Map    `tfsdk:"configuration"`
	AllocatePublicIp types.Bool   `tfsdk:"allocate_public_ip"`
	NetworkIds       types.List   `tfsdk:"network_ids"`
	VolumeIds        types.List   `tfsdk:"volume_ids"`
}

type ResourceModelSize struct {
	Category types.String `tfsdk:"category"`
	Tier     types.String `tfsdk:"tier"`
}

func (o ResourceModelSize) AttrTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"category": types.StringType,
		"tier":     types.StringType,
	}
}

// Update the plan or state with new values from the GET response
func MapVirtualMachineResponseToModel(ctx context.Context, response *ReadVirtualMachinesResponse, model ResourceModel) ResourceModel {
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
		"country":    response.Data.VirtualMachine.Datacenter.Country,
		"region":     response.Data.VirtualMachine.Datacenter.Region,
		"datacenter": response.Data.VirtualMachine.Datacenter.Name,
	})

	// Construct the configuration object
	model.Configuration, _ = types.MapValueFrom(ctx, types.StringType, map[string]string{
		"name":         response.Data.VirtualMachine.Configuration,
		"cpu":          strconv.FormatInt(response.Data.VirtualMachine.CPU, 10) + " cores",
		"ram":          strconv.FormatInt(response.Data.VirtualMachine.RAM, 10) + " GB",
		"base_storage": strconv.FormatInt(response.Data.VirtualMachine.Disk, 10) + " GB",
	})

	// If model doesn't already have these populated, set them
	model = setModelValuesNotPresent(ctx, response, model)

	return model
}

func setModelValuesNotPresent(ctx context.Context, response *ReadVirtualMachinesResponse, model ResourceModel) ResourceModel {
	if model.DatacenterId.IsNull() {
		model.DatacenterId = types.StringValue(response.Data.VirtualMachine.Datacenter.ID)
	}
	if model.Image.IsNull() {
		model.Image = types.StringValue(response.Data.VirtualMachine.Image)
	}
	if model.Name.IsNull() {
		model.Name = types.StringValue(response.Data.VirtualMachine.Name)
	}
	if model.Size.IsNull() {
		size := ResourceModelSize{}
		model.Size, _ = types.ObjectValueFrom(ctx, size.AttrTypes(), ResourceModelSize{
			Category: types.StringValue(response.Data.VirtualMachine.ConfigurationCategoryCode),
			Tier:     types.StringValue(response.Data.VirtualMachine.ConfigurationCode),
		})
	}

	return model
}

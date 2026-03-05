package virtualmachines

import (
	"context"
	"strconv"
	"time"

	"terraform-provider-gpcn/internal/client"
	"terraform-provider-gpcn/internal/networks"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
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
	PublicIp         types.String `tfsdk:"public_ip"`
	DisplaySecrets   types.Bool   `tfsdk:"display_secrets"`
	Secrets          types.Map    `tfsdk:"secrets"`
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
func MapVirtualMachineResponseToModel(ctx context.Context, gpcnClient *client.GpcnClient, response *ReadVirtualMachinesResponse, model ResourceModel) ResourceModel {
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
	var diags diag.Diagnostics
	model.Location, diags = types.MapValueFrom(ctx, types.StringType, map[string]string{
		"country":    response.Data.VirtualMachine.Datacenter.Country,
		"region":     response.Data.VirtualMachine.Datacenter.Region,
		"datacenter": response.Data.VirtualMachine.Datacenter.Name,
	})
	if diags.HasError() {
		model.Location = types.MapNull(types.StringType)
	}

	// Construct the configuration object
	model.Configuration, diags = types.MapValueFrom(ctx, types.StringType, map[string]string{
		"name":         response.Data.VirtualMachine.Configuration,
		"cpu":          strconv.FormatInt(response.Data.VirtualMachine.CPU, 10) + " cores",
		"ram":          strconv.FormatInt(response.Data.VirtualMachine.RAM, 10) + " GB",
		"base_storage": strconv.FormatInt(response.Data.VirtualMachine.Disk, 10) + " GB",
	})
	if diags.HasError() {
		model.Configuration = types.MapNull(types.StringType)
	}

	// If model doesn't already have these populated, set them
	model = setModelValuesNotPresent(ctx, gpcnClient, response, model)

	// If user requested secret values, fetch them now if needed
	model = setSecretValues(ctx, gpcnClient, response, model)

	return model
}

func setModelValuesNotPresent(ctx context.Context, gpcnClient *client.GpcnClient, response *ReadVirtualMachinesResponse, model ResourceModel) ResourceModel {
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
		var sizeDiags diag.Diagnostics
		model.Size, sizeDiags = types.ObjectValueFrom(ctx, size.AttrTypes(), ResourceModelSize{
			Category: types.StringValue(response.Data.VirtualMachine.ConfigurationCategoryCode),
			Tier:     types.StringValue(response.Data.VirtualMachine.ConfigurationCode),
		})
		if sizeDiags.HasError() {
			model.Size = types.ObjectNull(size.AttrTypes())
		}
	}

	model = setNetworkModelValuesNotPresent(ctx, gpcnClient, response.Data.VirtualMachine.ID, model)

	if model.WaitForStartup.IsNull() {
		// Set WaitForStartup to the default value
		model.WaitForStartup = types.BoolValue(true)
	}

	return model
}

func setNetworkModelValuesNotPresent(ctx context.Context, gpcnClient *client.GpcnClient, virtualMachineID string, model ResourceModel) ResourceModel {
	// Set the base public IP, might be replaced later
	model.PublicIp = types.StringValue("")
	// Fetch network interfaces for the virtual machine
	networkInterfaces, err := networks.GetNetworkInterfaces(gpcnClient, ctx, virtualMachineID)
	if err == nil && len(networkInterfaces) > 0 {
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
				model.NetworkIds = types.ListNull(types.StringType)
			}
		}

		// Set AllocatePublicIp if it's currently null
		if model.AllocatePublicIp.IsNull() {
			model.AllocatePublicIp = types.BoolValue(hasPublicIp)
		}
	}
	return model
}

func setSecretValues(ctx context.Context, gpcnClient *client.GpcnClient, response *ReadVirtualMachinesResponse, model ResourceModel) ResourceModel {
	// Construct the base object
	var secretsDiags diag.Diagnostics
	model.Secrets, secretsDiags = types.MapValueFrom(ctx, types.StringType, map[string]string{
		"username": "",
		"password": "",
		"ssh_key":  "",
	})
	if secretsDiags.HasError() {
		model.Secrets = types.MapNull(types.StringType)
	}

	if model.DisplaySecrets.ValueBool() {
		virtualMachineID := response.Data.VirtualMachine.ID
		sshKeyResponse, _ := GetSSHKey(gpcnClient, ctx, virtualMachineID)
		sshPasswordResponse, _ := GetPassword(gpcnClient, ctx, virtualMachineID)
		// Construct the secrets object
		model.Secrets, secretsDiags = types.MapValueFrom(ctx, types.StringType, map[string]string{
			"username": response.Data.VirtualMachine.Username,
			"password": sshPasswordResponse.Data.SSHPassword,
			"ssh_key":  sshKeyResponse.Data.PrivateKey,
		})
		if secretsDiags.HasError() {
			model.Secrets = types.MapNull(types.StringType)
		}
	}
	return model
}

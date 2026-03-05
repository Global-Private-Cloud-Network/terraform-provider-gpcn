package virtualmachines

import (
	"context"
	"fmt"
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

// MapVirtualMachineResponseToModel updates the plan or state with new values from the GET response.
// Returns the updated model and any diagnostics encountered during mapping.
func MapVirtualMachineResponseToModel(ctx context.Context, gpcnClient *client.GpcnClient, response *ReadVirtualMachinesResponse, model ResourceModel) (ResourceModel, diag.Diagnostics) {
	var allDiags diag.Diagnostics

	model.ID = types.StringValue(response.Data.ID)

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
	})
	if diags.HasError() {
		allDiags.Append(diags...)
		model.Configuration = types.MapNull(types.StringType)
	}

	// If model doesn't already have these populated, set them
	model, diags = setModelValuesNotPresent(ctx, gpcnClient, response, model)
	allDiags.Append(diags...)

	// If user requested secret values, fetch them now if needed
	model, diags = setSecretValues(ctx, gpcnClient, response, model)
	allDiags.Append(diags...)

	return model, allDiags
}

func setModelValuesNotPresent(ctx context.Context, gpcnClient *client.GpcnClient, response *ReadVirtualMachinesResponse, model ResourceModel) (ResourceModel, diag.Diagnostics) {
	var allDiags diag.Diagnostics

	if model.DatacenterId.IsNull() {
		model.DatacenterId = types.StringValue(response.Data.Datacenter.ID)
	}
	if model.Image.IsNull() {
		model.Image = types.StringValue(response.Data.Image)
	}
	if model.Name.IsNull() {
		model.Name = types.StringValue(response.Data.Name)
	}
	if model.Size.IsNull() {
		size := ResourceModelSize{}
		var sizeDiags diag.Diagnostics
		model.Size, sizeDiags = types.ObjectValueFrom(ctx, size.AttrTypes(), ResourceModelSize{
			Category: types.StringValue(response.Data.Configuration.CategoryCode),
			Tier:     types.StringValue(response.Data.Configuration.Code),
		})
		if sizeDiags.HasError() {
			allDiags.Append(sizeDiags...)
			model.Size = types.ObjectNull(size.AttrTypes())
		}
	}

	var networkDiags diag.Diagnostics
	model, networkDiags = setNetworkModelValuesNotPresent(ctx, gpcnClient, response.Data.ID, model)
	allDiags.Append(networkDiags...)

	if model.WaitForStartup.IsNull() {
		// Set WaitForStartup to the default value
		model.WaitForStartup = types.BoolValue(true)
	}

	return model, allDiags
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

func setSecretValues(ctx context.Context, gpcnClient *client.GpcnClient, response *ReadVirtualMachinesResponse, model ResourceModel) (ResourceModel, diag.Diagnostics) {
	var allDiags diag.Diagnostics

	// Construct the base object
	var secretsDiags diag.Diagnostics
	model.Secrets, secretsDiags = types.MapValueFrom(ctx, types.StringType, map[string]string{
		"username": "",
		"password": "",
		"ssh_key":  "",
	})
	if secretsDiags.HasError() {
		allDiags.Append(secretsDiags...)
		model.Secrets = types.MapNull(types.StringType)
	}

	if model.DisplaySecrets.ValueBool() {
		virtualMachineID := response.Data.ID
		sshKeyResponse, err := GetSSHKey(gpcnClient, ctx, virtualMachineID)
		if err != nil {
			allDiags.AddWarning(
				"Unable to fetch SSH key",
				fmt.Sprintf("Failed to fetch SSH key for VM %s: %s", virtualMachineID, err.Error()),
			)
		}
		sshPasswordResponse, err := GetPassword(gpcnClient, ctx, virtualMachineID)
		if err != nil {
			allDiags.AddWarning(
				"Unable to fetch password",
				fmt.Sprintf("Failed to fetch password for VM %s: %s", virtualMachineID, err.Error()),
			)
		}

		// Construct the secrets object with whatever values we were able to retrieve
		sshKey := ""
		password := ""
		if sshKeyResponse != nil {
			sshKey = sshKeyResponse.Data.PrivateKey
		}
		if sshPasswordResponse != nil {
			password = sshPasswordResponse.Data.SSHPassword
		}

		model.Secrets, secretsDiags = types.MapValueFrom(ctx, types.StringType, map[string]string{
			"username": response.Data.Username,
			"password": password,
			"ssh_key":  sshKey,
		})
		if secretsDiags.HasError() {
			allDiags.Append(secretsDiags...)
			model.Secrets = types.MapNull(types.StringType)
		}
	}
	return model, allDiags
}

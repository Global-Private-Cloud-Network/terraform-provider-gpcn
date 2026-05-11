package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"slices"

	"terraform-provider-gpcn/internal/client"
	"terraform-provider-gpcn/internal/networks"
	"terraform-provider-gpcn/internal/volumes"

	"terraform-provider-gpcn/internal/virtualmachines"

	"regexp"

	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/mapplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/objectplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

// Ensure the implementation satisfies the expected interfaces.
var (
	_ resource.Resource                = &virtualMachinesResource{}
	_ resource.ResourceWithConfigure   = &virtualMachinesResource{}
	_ resource.ResourceWithImportState = &virtualMachinesResource{}
)

// NewVirtualMachinesResource is a helper function to simplify the provider implementation.
func NewVirtualMachinesResource() resource.Resource {
	return &virtualMachinesResource{}
}

// virtualMachinesResource is the resource implementation.
type virtualMachinesResource struct {
	client *client.GpcnClient
}

// Metadata returns the resource type name.
func (r *virtualMachinesResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_virtualmachine"
}

// Schema defines the schema for the resource.
func (r *virtualMachinesResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a virtual machine instance with configurable compute resources, networking, and storage",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "Unique identifier for the virtual machine in UUID format",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				Description: "Human-readable name for the virtual machine",
				Required:    true,
			},
			"datacenter_id": schema.StringAttribute{
				Description: "Unique identifier of the datacenter where the virtual machine will be created. Changing this value requires replacing the virtual machine",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					// Changing the datacenter_id requires us to destroy and create a new VM
					stringplanmodifier.RequiresReplace(),
				},
			},
			"size": schema.SingleNestedAttribute{
				Description: "Hardware size configuration defining the compute resources (CPU, memory, disk) for the virtual machine. Specified using a category and tier pairing. Downsizing requires replacing the virtual machine. Note that not all sizes are available for every datacenter",
				Required:    true,
				Attributes: map[string]schema.Attribute{
					"category": schema.StringAttribute{
						Description: "Short code representing the category. Must be one of: 'general' or 'memory'",
						Required:    true,
						Validators: []validator.String{
							stringvalidator.OneOf(virtualmachines.AllCategoryStrings()...),
						},
					},
					"tier": schema.StringAttribute{
						Description: "Human-readable name of the size configuration. Must be one of: 'g-micro-1', 'g-small-1', 'g-medium-1', 'g-large-1', 'g-xl-1', 'm-micro-1', 'm-small-1', 'm-medium-1', 'm-large-1', 'm-xl-1'",
						Required:    true,
						Validators: []validator.String{
							stringvalidator.OneOf(virtualmachines.AllTiers...),
						},
					},
				},
				PlanModifiers: []planmodifier.Object{
					virtualmachines.SizePlanModifier{},
				},
			},
			"image": schema.StringAttribute{
				Description: "Operating system image to use for the virtual machine. Must be one of the supported image names. Changing this value requires replacing the virtual machine. Note that not all images are available for every datacenter",
				Required:    true,
				Validators: []validator.String{
					virtualmachines.ImageNameValidator{},
				},
				PlanModifiers: []planmodifier.String{
					// Changing the image requires us to destroy and create a new VM
					stringplanmodifier.RequiresReplace(),
				},
			},
			"created_time": schema.StringAttribute{
				Description: "Timestamp when the virtual machine was created in ISO-8601 format",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"last_updated": schema.StringAttribute{
				Description: "Timestamp when the virtual machine was last updated in ISO-8601 format",
				Computed:    true,
			},
			"location": schema.MapAttribute{
				Description: "Location details including datacenter, region, and country information",
				ElementType: types.StringType,
				Computed:    true,
				PlanModifiers: []planmodifier.Map{
					mapplanmodifier.UseStateForUnknown(),
				},
			},
			"configuration": schema.MapAttribute{
				Description: "Hardware configuration details including CPU, RAM, and disk specifications",
				ElementType: types.StringType,
				Computed:    true,
				PlanModifiers: []planmodifier.Map{
					virtualmachines.ConfigurationPlanModifier{},
				},
			},
			"allocate_public_ip": schema.BoolAttribute{
				Description: "Whether to allocate a public IP address for the virtual machine",
				Required:    true,
			},
			"public_ip": schema.StringAttribute{
				Description: "The public IP address, if allocate_public_ip is True",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					virtualmachines.PublicIpPlanModifier{},
				},
			},
			"network_ids": schema.ListAttribute{
				Description: "List of network IDs to attach to the virtual machine. Maximum of 5 networks allowed",
				ElementType: types.StringType,
				Optional:    true,
				Computed:    true,
				Validators: []validator.List{
					listvalidator.SizeAtMost(virtualmachines.MAX_NETWORKS_ATTACHED_ALLOWED),
					listvalidator.UniqueValues(),
				},
				Default: listdefault.StaticValue(types.ListValueMust(types.StringType, []attr.Value{})),
			},
			"volume_ids": schema.ListAttribute{
				Description: "List of volume IDs to attach to the virtual machine. Maximum of 5 volumes allowed. A volume can only be attached to a single virtual machine, so this parameter will not work as expected when using Terraform's count meta-attribute",
				ElementType: types.StringType,
				Optional:    true,
				Computed:    true,
				Validators: []validator.List{
					listvalidator.SizeAtMost(virtualmachines.MAX_VOLUMES_ATTACHED_ALLOWED),
					listvalidator.UniqueValues(),
				},
				Default: listdefault.StaticValue(types.ListValueMust(types.StringType, []attr.Value{})),
			},
			"network_hotplug": schema.BoolAttribute{
				Description: "Whether the virtual machine supports hot modifications without the virtual machine being in Shutoff status",
				Computed:    true,
				PlanModifiers: []planmodifier.Bool{
					boolplanmodifier.UseStateForUnknown(),
				},
			},
			"resource_group_id": schema.StringAttribute{
				Description: "Optional ID of the resource group to assign this virtual machine to",
				Optional:    true,
			},
			"auth": schema.SingleNestedAttribute{
				Description: "Authentication configuration for the virtual machine. Either ssh_key_id or password must be specified. Changing any value in this block requires replacing the virtual machine",
				Required:    true,
				PlanModifiers: []planmodifier.Object{
					objectplanmodifier.RequiresReplace(),
				},
				Attributes: map[string]schema.Attribute{
					"ssh_key_id": schema.StringAttribute{
						Description: "ID of the SSH key to use for authentication. Cannot be set together with password",
						Optional:    true,
						Validators: []validator.String{
							stringvalidator.ConflictsWith(
								path.MatchRelative().AtParent().AtName("password"),
							),
							stringvalidator.AtLeastOneOf(
								path.MatchRelative().AtParent().AtName("ssh_key_id"),
								path.MatchRelative().AtParent().AtName("password"),
							),
						},
					},
					"username": schema.StringAttribute{
						Description: "Username for the virtual machine. Must be 3-20 characters matching ^[a-zA-Z_][a-zA-Z0-9_-]*$",
						Required:    true,
						Validators: []validator.String{
							stringvalidator.LengthBetween(3, 20),
							stringvalidator.RegexMatches(
								regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_-]*$`),
								"Username must start with a letter or underscore and contain only letters, digits, underscores, and hyphens",
							),
						},
					},
					"password": schema.StringAttribute{
						Description: "Password for authentication. Must be 12-20 characters, contain only letters, digits, and ! @ # % - _ ., and include at least one uppercase letter, one lowercase letter, one digit, and one symbol. Cannot be set when ssh_key_id is set. username defaults to the image default if not specified",
						Optional:    true,
						Sensitive:   true,
						Validators: []validator.String{
							virtualmachines.PasswordValidator{},
							stringvalidator.ConflictsWith(
								path.MatchRelative().AtParent().AtName("ssh_key_id"),
							),
							stringvalidator.AtLeastOneOf(
								path.MatchRelative().AtParent().AtName("ssh_key_id"),
								path.MatchRelative().AtParent().AtName("password"),
							),
						},
					},
				},
			},
		},
	}
}

// Configure adds the provider configured client to the resource.
func (r *virtualMachinesResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	gpcnClient, ok := req.ProviderData.(*client.GpcnClient)

	if !ok {
		resp.Diagnostics.AddError(
			virtualmachines.ErrSummaryUnexpectedConfigureType,
			fmt.Sprintf(virtualmachines.ErrDetailExpectedGpcnClient, req.ProviderData),
		)

		return
	}

	r.client = gpcnClient
}

// Create creates the resource and sets the initial Terraform state.
func (r *virtualMachinesResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	// Add correlation ID for request tracing
	ctx = client.WithCorrelationID(ctx)
	tflog.Info(ctx, virtualmachines.LogStartingCreateGPCNVirtualMachine)
	// Retrieve values from plan
	var plan virtualmachines.ResourceModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Verify the image selected is still available
	imageId, _, err := virtualmachines.GetVirtualMachineImageId(r.client, ctx, plan.DatacenterId.ValueString(), plan.Image.ValueString())

	if err != nil {
		resp.Diagnostics.AddError(
			virtualmachines.ErrSummaryErrorVerifyingImage,
			fmt.Errorf("%s: %w", fmt.Sprintf(virtualmachines.ErrDetailImageVerificationFailed, plan.Image.ValueString(), plan.DatacenterId.ValueString()), err).Error(),
		)
		return
	}

	// Verify the size selected is still available
	var size virtualmachines.ResourceModelSize
	plan.Size.As(ctx, &size, basetypes.ObjectAsOptions{})
	sizeId, _, err := virtualmachines.GetVirtualMachineSizeConfigurationId(r.client, ctx, plan.DatacenterId.ValueString(), size.Tier.ValueString())
	if err != nil {
		resp.Diagnostics.AddError(
			virtualmachines.ErrSummaryErrorVerifyingSize,
			fmt.Errorf("%s: %w", fmt.Sprintf(virtualmachines.ErrDetailSizeVerificationFailed, size.Category.ValueString(), size.Tier.ValueString(), plan.DatacenterId.ValueString()), err).Error(),
		)
		return
	}

	getVirtualMachineResponse, err := virtualmachines.CreateVirtualMachine(r.client, ctx, imageId, sizeId, plan)
	if err != nil {
		resp.Diagnostics.AddError(
			virtualmachines.ErrSummaryUnableToCreateVM,
			err.Error(),
		)
		return
	}

	var mapDiags diag.Diagnostics
	plan, mapDiags = virtualmachines.MapVirtualMachineResponseToModel(ctx, r.client, getVirtualMachineResponse, plan)
	resp.Diagnostics.Append(mapDiags...)

	// Attach each volume
	if !plan.VolumeIds.IsNull() {
		var volumeIds []string
		plan.VolumeIds.ElementsAs(ctx, &volumeIds, true)
		for _, volumeId := range volumeIds {
			err = volumes.AddVolumeToVirtualMachine(r.client, ctx, plan.ID.ValueString(), volumeId)
			if err != nil {
				resp.Diagnostics.AddWarning(
					virtualmachines.WarnSummaryAttachingVolumeFailed,
					fmt.Errorf("%s: %w", fmt.Sprintf(virtualmachines.WarnDetailAttachingVolumeWithIDFailed, volumeId), err).Error(),
				)
			}
		}
	}

	// Set state to fully populated data
	diags = resp.State.Set(ctx, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Info(ctx, virtualmachines.LogSuccessfullyFinishedCreateGPCNVirtualMachine)
}

// Read refreshes the Terraform state with the latest data.
func (r *virtualMachinesResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	// Add correlation ID for request tracing
	ctx = client.WithCorrelationID(ctx)
	tflog.Info(ctx, virtualmachines.LogStartingReadGPCNVirtualMachine)
	// Get current state
	var state virtualmachines.ResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Perform a GET call to retrieve actual information about the Virtual Machine
	getVirtualMachineResponse, err := virtualmachines.GetVirtualMachine(r.client, ctx, state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError(
			virtualmachines.ErrSummaryRetrievingVMInfoFailed,
			fmt.Errorf("%s: %w", virtualmachines.ErrDetailVMInfoFailedCanImport, err).Error(),
		)
		return
	}

	var mapDiags diag.Diagnostics
	state, mapDiags = virtualmachines.MapVirtualMachineResponseToModel(ctx, r.client, getVirtualMachineResponse, state)
	resp.Diagnostics.Append(mapDiags...)

	// Set state to fully populated data
	diags = resp.State.Set(ctx, state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Info(ctx, virtualmachines.LogSuccessfullyFinishedReadGPCNVirtualMachine)
}

// Update updates the resource and sets the updated Terraform state on success.
func (r *virtualMachinesResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	// Add correlation ID for request tracing
	ctx = client.WithCorrelationID(ctx)
	tflog.Info(ctx, virtualmachines.LogStartingUpdateGPCNVirtualMachine)
	// Map both the plan and state to see what's changed
	var plan virtualmachines.ResourceModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	var state virtualmachines.ResourceModel
	diags = req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Validate we aren't removing every network
	err := virtualmachines.ValidateAllNetworksAreNotRemoved(state.NetworkIds, plan.NetworkIds)
	if err != nil {
		resp.Diagnostics.AddError(
			virtualmachines.ErrSummaryEncounteredValidationError,
			err.Error(),
		)
		return
	}

	// Validate the prospective primary network has a valid configuration for allocatePublicIp
	if plan.AllocatePublicIp != state.AllocatePublicIp {
		// First validate the primary network type is standard
		err := virtualmachines.ValidatePublicIpValue(r.client, ctx, plan)
		if err != nil {
			resp.Diagnostics.AddError(
				virtualmachines.ErrSummaryEncounteredValidationError,
				err.Error(),
			)
			return
		}
	}

	// Controls stopping the VM. Since this is time-expensive, we only need to do this in a few cases
	needStopVM := determineIfVMNeedsStopped(state, plan)

	// Before proceeding with update, conditionally stop the virtual machine
	if needStopVM {
		err = virtualmachines.StopVirtualMachine(r.client, ctx, state.ID.ValueString())
		if err != nil {
			resp.Diagnostics.AddError(
				virtualmachines.ErrSummaryUnableToUpdateVM,
				fmt.Errorf("%s: %w", fmt.Sprintf(virtualmachines.ErrDetailStoppingVM, state.ID.ValueString()), err).Error(),
			)
			return
		}
	}

	// Update network interfaces if changed
	networkDiags := virtualmachines.UpdateNetworkInterfacesIfChanged(r.client, ctx, state.ID.ValueString(), state, plan)
	resp.Diagnostics.Append(networkDiags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Update public IP allocation if changed
	publicIPDiags := virtualmachines.UpdatePublicIPIfChanged(r.client, ctx, state.ID.ValueString(), state, plan)
	resp.Diagnostics.Append(publicIPDiags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Update size if changed
	sizeDiags := virtualmachines.UpdateSizeIfChanged(r.client, ctx, plan.ID.ValueString(), state, plan)
	resp.Diagnostics.Append(sizeDiags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Update name if changed
	nameDiags := virtualmachines.UpdateChangeableAttributesIfChanged(r.client, ctx, state.ID.ValueString(), state, plan)
	resp.Diagnostics.Append(nameDiags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Update volumes if changed
	volumeDiags := virtualmachines.UpdateVolumesIfChanged(r.client, ctx, plan.ID.ValueString(), state, plan)
	resp.Diagnostics.Append(volumeDiags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Perform a GET call to retrieve actual information about the Virtual Machine
	tflog.Info(ctx, virtualmachines.LogAllVMUpdateOpsCompleteRetrievingLatestInfo)
	getVirtualMachineResponse, err := virtualmachines.GetVirtualMachine(r.client, ctx, plan.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError(
			virtualmachines.ErrSummaryRetrievingVMInfoFailed,
			fmt.Errorf("%s: %w", virtualmachines.ErrDetailVMInfoFailedCanImport, err).Error(),
		)
		return
	}

	tflog.Info(ctx, virtualmachines.LogRetrievedLatestVMInfoMappingToModel)
	var mapDiags diag.Diagnostics
	plan, mapDiags = virtualmachines.MapVirtualMachineResponseToModel(ctx, r.client, getVirtualMachineResponse, plan)
	resp.Diagnostics.Append(mapDiags...)

	// Once finished, conditionally start the virtual machine again
	if needStopVM {
		err = virtualmachines.StartVirtualMachine(r.client, ctx, state.ID.ValueString())
		if err != nil {
			tflog.Debug(ctx, fmt.Errorf("%s: %w", fmt.Sprintf(virtualmachines.ErrDetailStartingVM, state.ID.ValueString()), err).Error())
		}
	}
	tflog.Debug(ctx, fmt.Sprintf(virtualmachines.LogSuccessfullyUpdatedVMMayNotBeRunning, state.ID.ValueString()))

	// Set state to fully populated data
	diags = resp.State.Set(ctx, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Info(ctx, virtualmachines.LogSuccessfullyFinishedUpdateGPCNVirtualMachine)
}

// Delete deletes the resource and removes the Terraform state on success.
func (r *virtualMachinesResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	// Add correlation ID for request tracing
	ctx = client.WithCorrelationID(ctx)
	tflog.Info(ctx, virtualmachines.LogStartingDeleteGPCNVirtualMachine)
	var state virtualmachines.ResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Before proceeding with delete, stop the virtual machine
	err := virtualmachines.StopVirtualMachine(r.client, ctx, state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError(
			virtualmachines.ErrSummaryUnableToDeleteVM,
			fmt.Errorf("%s: %w", fmt.Sprintf(virtualmachines.ErrDetailStoppingVM, state.ID.ValueString()), err).Error(),
		)
		return
	}

	// Before deleting, detach any network interfaces first
	if !state.NetworkIds.IsNull() {
		networkInterfaces, err := networks.GetNetworkInterfaces(r.client, ctx, state.ID.ValueString())

		if err != nil {
			resp.Diagnostics.AddError(
				virtualmachines.ErrSummaryErrorRetrievingNetworkIfaces,
				fmt.Errorf("%s: %w", fmt.Sprintf(virtualmachines.ErrDetailNetworkInterfacesForVM, state.ID.ValueString()), err).Error(),
			)
			return
		}

		for _, adapter := range networkInterfaces {
			// Cannot remove the primary interface
			if adapter.IsPrimary.ValueInt64() != 1 {
				err = networks.RemoveNetworkInterface(r.client, ctx, state.ID.ValueString(), adapter.ID.ValueString())
				if err != nil {
					resp.Diagnostics.AddWarning(
						virtualmachines.WarnSummaryRemovingNetworkInterfaceFailed,
						fmt.Errorf("%s: %w", fmt.Sprintf(virtualmachines.WarnDetailRemovingNetworkInterfaceWithIDFailed, adapter.ID.ValueString()), err).Error(),
					)
				}
			}
		}
	}

	// Detach volumes too
	if !state.VolumeIds.IsNull() {
		var volumeIds []string
		state.VolumeIds.ElementsAs(ctx, &volumeIds, true)
		for _, volumeId := range volumeIds {
			err := volumes.RemoveVolumeFromVirtualMachine(r.client, ctx, volumeId)
			if err != nil {
				resp.Diagnostics.AddWarning(
					virtualmachines.WarnSummaryRemovingVolumeFailed,
					fmt.Errorf("%s: %w", fmt.Sprintf(virtualmachines.WarnDetailRemovingVolumeWithIDFailed, volumeId), err).Error()+". This warning should only be treated as an error if you are not trying to delete the virtual machine and volume in quick succession.",
				)
			}
		}
	}

	request, err := http.NewRequestWithContext(ctx, "DELETE", virtualmachines.BASE_URL_V1+state.ID.ValueString(), nil)
	if err != nil {
		resp.Diagnostics.AddError(
			virtualmachines.ErrSummaryUnableToCreateDeleteRequest,
			err.Error(),
		)
		return
	}
	tflog.Info(ctx, virtualmachines.LogConstructedDeleteGPCNVirtualMachineRequest)

	response, err := r.client.DoWithRetry(request)
	if err != nil {
		resp.Diagnostics.AddError(
			virtualmachines.ErrSummaryUnableToDeleteVM,
			fmt.Errorf("%s: %w", fmt.Sprintf(virtualmachines.ErrDetailUnableToDeleteVMWithID, state.ID.ValueString()), err).Error(),
		)
		return
	}
	defer response.Body.Close()
	tflog.Info(ctx, virtualmachines.LogIssuedDeleteGPCNVirtualMachineJob)

	// Read the response body and process it as deleteVirtualMachineResponse
	body, err := io.ReadAll(response.Body)
	if err != nil {
		resp.Diagnostics.AddError(
			virtualmachines.ErrSummaryErrorReadingDeleteBody,
			err.Error(),
		)
		return
	}

	var deleteVirtualMachineResponse client.JobStatusSingularResponse
	err = json.Unmarshal(body, &deleteVirtualMachineResponse)

	if err != nil {
		resp.Diagnostics.AddError(
			virtualmachines.ErrSummaryErrorUnmarshalingDelete,
			fmt.Errorf("%s: %w", fmt.Sprintf(virtualmachines.ErrDetailUnmarshalingDeleteWithID, state.ID.ValueString()), err).Error(),
		)
		return
	}

	_, err = client.PerformLongPolling(r.client, ctx, "Delete GPCN Virtual Machine", deleteVirtualMachineResponse.Data.JobID)

	if err != nil {
		resp.Diagnostics.AddError(
			virtualmachines.ErrSummaryEncounteredErrorGettingJobInfo,
			fmt.Errorf("%s: %w", virtualmachines.ErrDetailJobInfoCheckDashboard, err).Error(),
		)
		return
	}

	tflog.Info(ctx, virtualmachines.LogSuccessfullyFinishedDeleteGPCNVirtualMachine)
}

func (r *virtualMachinesResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

/*
Some actions can be done without stopping the VM. Since it's a heavy time investment to start and stop, determine that and use it for the rest of the update logic.
Cases where VM needs to be stopped:
  - NetworkHotplug is disabled AND one of the below
  - NetworkIds change
  - VolumeIds change
  - Size changes
*/
func determineIfVMNeedsStopped(state, plan virtualmachines.ResourceModel) bool {
	// If network hotplug is enabled, the VM does not need to be stopped
	if state.NetworkHotplug.ValueBool() {
		return false
	}

	// If network hotplug is disabled, the VM needs to be stopped for a few scenarios
	return (!slices.Equal(plan.NetworkIds.Elements(), state.NetworkIds.Elements())) ||
		(!slices.Equal(plan.VolumeIds.Elements(), state.VolumeIds.Elements())) ||
		!maps.Equal(state.Size.Attributes(), plan.Size.Attributes())
}

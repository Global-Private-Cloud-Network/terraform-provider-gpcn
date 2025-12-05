package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"terraform-provider-gpcn/internal/client"
	"terraform-provider-gpcn/internal/networks"
	"terraform-provider-gpcn/internal/volumes"

	"terraform-provider-gpcn/internal/virtualmachines"

	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/mapplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
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
	client *http.Client
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
			"wait_for_startup": schema.BoolAttribute{
				Description: "Determines if Terraform should wait for the virtual machine to start running before exiting. This will add a few minutes to virtual machine creation",
				Optional:    true,
				Computed:    true,
				PlanModifiers: []planmodifier.Bool{
					boolplanmodifier.UseStateForUnknown(),
				},
				Default: booldefault.StaticBool(true),
			},
			"size": schema.StringAttribute{
				Description: "Size specification defining CPU, RAM, and disk resources. Can be upgraded to a larger size without replacement, but downsizing requires replacement",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					// Changing the size requires us to destroy and create a new VM if the size is smaller
					stringplanmodifier.RequiresReplaceIf(func(ctx context.Context, req planmodifier.StringRequest, resp *stringplanmodifier.RequiresReplaceIfFuncResponse) {
						// Get other sizes and see if this is considered a size increase or not
						var additionalSizes []virtualmachines.VirtualMachineSizesDataResponseTF
						req.State.GetAttribute(ctx, path.Root("additional_sizes"), &additionalSizes)
						stateIdx := slices.IndexFunc(additionalSizes, func(size virtualmachines.VirtualMachineSizesDataResponseTF) bool {
							return strings.EqualFold(req.StateValue.ValueString(), size.Name.ValueString())
						})
						// This should never return -1 but just in case...
						if stateIdx < 0 {
							resp.Diagnostics.AddWarning(virtualmachines.ErrSummaryUnableToCompletePlan, virtualmachines.ErrDetailSizeNoLongerAvailable)
							resp.RequiresReplace = true
							return
						}

						planIdx := slices.IndexFunc(additionalSizes, func(size virtualmachines.VirtualMachineSizesDataResponseTF) bool {
							return strings.EqualFold(req.PlanValue.ValueString(), size.Name.ValueString())
						})
						if planIdx < 0 {
							var names []string
							for _, size := range additionalSizes {
								names = append(names, size.Name.ValueString())
							}
							sizesFormatted := strings.Join(names, ", ")
							resp.Diagnostics.AddError(
								virtualmachines.ErrSummaryUnableToCompletePlan,
								fmt.Sprintf(virtualmachines.ErrDetailSizeNotAvailableForDatacenterImage, req.PlanValue.ValueString(), sizesFormatted),
							)
							return
						}

						// Require a replacement if the plan CPU is smaller, or this is a size decrease
						resp.RequiresReplace = int(additionalSizes[planIdx].CPU.ValueInt64()) < int(additionalSizes[stateIdx].CPU.ValueInt64())
					}, "Requires a replacement if the planned size is smaller than the current size", "Requires a replacement if the plan size is smaller than the current size"),
				},
			},
			"image": schema.StringAttribute{
				Description: "Operating system image to use for the virtual machine. Changing this value requires replacing the virtual machine",
				Required:    true,
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
			},
			"allocate_public_ip": schema.BoolAttribute{
				Description: "Whether to allocate a public IP address for the virtual machine",
				Required:    true,
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
				Description: "List of volume IDs to attach to the virtual machine. Maximum of 5 volumes allowed. A volume can only be attached to a single virtual machine, so this parameter will not work as expected when using the count meta-attribute",
				ElementType: types.StringType,
				Optional:    true,
				Computed:    true,
				Validators: []validator.List{
					listvalidator.SizeAtMost(virtualmachines.MAX_VOLUMES_ATTACHED_ALLOWED),
					listvalidator.UniqueValues(),
				},
				Default: listdefault.StaticValue(types.ListValueMust(types.StringType, []attr.Value{})),
			},
			"additional_images": schema.ListNestedAttribute{
				Description: "List of available operating system images that can be used for this virtual machine",
				Computed:    true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id": schema.Int64Attribute{
							Description: "Unique identifier for the image",
							Computed:    true,
						},
						"name": schema.StringAttribute{
							Description: "Name of the image",
							Computed:    true,
						},
					},
				},
			},
			"additional_sizes": schema.ListNestedAttribute{
				Description: "List of available size configurations for this virtual machine",
				Computed:    true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id": schema.Int64Attribute{
							Description: "Unique identifier for the size configuration",
							Computed:    true,
						},
						"name": schema.StringAttribute{
							Description: "Name of the size configuration",
							Computed:    true,
						},
						"cpu": schema.Int64Attribute{
							Description: "Number of CPU cores",
							Computed:    true,
						},
						"ram": schema.Int64Attribute{
							Description: "Amount of RAM in MB",
							Computed:    true,
						},
						"disk": schema.Int64Attribute{
							Description: "Disk size in GB",
							Computed:    true,
						},
					},
				},
			},
			"image_id": schema.Int64Attribute{
				Description: "Internal identifier for the selected image",
				Computed:    true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
				},
			},
			"size_id": schema.Int64Attribute{
				Description: "Internal identifier for the selected size configuration",
				Computed:    true,
			},
		},
	}
}

// Configure adds the provider configured client to the resource.
func (r *virtualMachinesResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	client, ok := req.ProviderData.(*http.Client)

	if !ok {
		resp.Diagnostics.AddError(
			virtualmachines.ErrSummaryUnexpectedConfigureType,
			fmt.Sprintf(virtualmachines.ErrDetailExpectedHTTPClient, req.ProviderData),
		)

		return
	}

	r.client = client
}

// Create creates the resource and sets the initial Terraform state.
func (r *virtualMachinesResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	tflog.Info(ctx, virtualmachines.LogStartingCreateGPCNVirtualMachine)
	// Retrieve values from plan
	var plan virtualmachines.ResourceModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Verify the image selected is still available
	imageId, images, err := virtualmachines.GetVirtualMachineImageId(r.client, ctx, plan.DatacenterId.ValueString(), plan.Image.ValueString())

	if err != nil {
		resp.Diagnostics.AddError(
			virtualmachines.ErrSummaryErrorVerifyingImage,
			fmt.Sprintf(virtualmachines.ErrDetailImageVerificationFailed, plan.Image.ValueString(), plan.DatacenterId.ValueString())+": "+err.Error(),
		)
		return
	}

	// Verify the size selected is still available
	sizeId, sizes, err := virtualmachines.GetVirtualMachineSizeId(r.client, ctx, imageId, plan.DatacenterId.ValueString(), plan.Size.ValueString())
	if err != nil {
		resp.Diagnostics.AddError(
			virtualmachines.ErrSummaryErrorVerifyingSize,
			fmt.Sprintf(virtualmachines.ErrDetailSizeVerificationFailed, plan.Size.ValueString(), plan.DatacenterId.ValueString())+": "+err.Error(),
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

	plan = virtualmachines.MapVirtualMachineResponseToModel(ctx, getVirtualMachineResponse, images, sizes, plan)

	// Attach each volume
	if !plan.VolumeIds.IsNull() {
		var volumeIds []string
		plan.VolumeIds.ElementsAs(ctx, &volumeIds, true)
		for _, volumeId := range volumeIds {
			err = volumes.AddVolumeToVirtualMachine(r.client, ctx, plan.ID.ValueString(), volumeId)
			if err != nil {
				resp.Diagnostics.AddWarning(
					virtualmachines.WarnSummaryAttachingVolumeFailed,
					fmt.Sprintf(virtualmachines.WarnDetailAttachingVolumeWithIDFailed, volumeId)+": "+err.Error(),
				)
			}
		}
	}

	// Once finished, start the virtual machine. It may already be started, in which case this will be a quick call
	err = virtualmachines.StartVirtualMachine(r.client, ctx, plan.ID.ValueString(), plan.WaitForStartup.ValueBool())
	if err != nil {
		resp.Diagnostics.AddWarning(
			virtualmachines.WarnSummaryUnableToStartVM,
			fmt.Sprintf(virtualmachines.ErrDetailStartingVM, plan.ID.ValueString())+": "+err.Error(),
		)
	}
	tflog.Debug(ctx, virtualmachines.LogSuccessfullyCreatedVMMayNotBeRunning)

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
			virtualmachines.ErrDetailVMInfoFailedCanImport+": "+err.Error(),
		)
		return
	}

	imageId, images, err := virtualmachines.GetVirtualMachineImageId(r.client, ctx, getVirtualMachineResponse.Data.VirtualMachine.DatacenterId, getVirtualMachineResponse.Data.VirtualMachine.Image)

	if err != nil {
		resp.Diagnostics.AddError(
			virtualmachines.ErrSummaryErrorVerifyingImage,
			fmt.Sprintf(virtualmachines.ErrDetailImageVerificationFailed, getVirtualMachineResponse.Data.VirtualMachine.Image, getVirtualMachineResponse.Data.VirtualMachine.DatacenterId)+": "+err.Error(),
		)
		return
	}

	_, sizes, err := virtualmachines.GetVirtualMachineSizeId(r.client, ctx, imageId, getVirtualMachineResponse.Data.VirtualMachine.DatacenterId, getVirtualMachineResponse.Data.VirtualMachine.Configuration)
	if err != nil {
		resp.Diagnostics.AddError(
			virtualmachines.ErrSummaryErrorVerifyingSize,
			fmt.Sprintf(virtualmachines.ErrDetailSizeVerificationFailed, getVirtualMachineResponse.Data.VirtualMachine.Configuration, getVirtualMachineResponse.Data.VirtualMachine.DatacenterId)+": "+err.Error(),
		)
		return
	}

	state = virtualmachines.MapVirtualMachineResponseToModel(ctx, getVirtualMachineResponse, images, sizes, state)

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
				fmt.Sprintf(virtualmachines.ErrDetailStoppingVM, state.ID.ValueString())+": "+err.Error(),
			)
			return
		}
	}

	// If network ids are updated, need to call add/remove network interface
	if !slices.Equal(plan.NetworkIds.Elements(), state.NetworkIds.Elements()) {
		networkInterfaces, err := networks.GetNetworkInterfaces(r.client, ctx, state.ID.ValueString())
		if err != nil {
			resp.Diagnostics.AddError(
				virtualmachines.ErrSummaryErrorRetrievingNetworkIfaces,
				err.Error(),
			)
			return
		}

		var oldNetworksList, newNetworksList []string
		state.NetworkIds.ElementsAs(ctx, &oldNetworksList, true)
		plan.NetworkIds.ElementsAs(ctx, &newNetworksList, true)
		// Validate new network interface size will not increase beyond network cap
		err = virtualmachines.ValidateNetworkInterfacesDoesNotExceedCap(oldNetworksList, newNetworksList, networkInterfaces)
		if err != nil {
			resp.Diagnostics.AddError(
				virtualmachines.ErrSummaryErrorUpdatingNetworkInterfaces,
				err.Error(),
			)
			return
		}

		err = networks.UpdateNetworkInterfaces(r.client, ctx, plan.ID.ValueString(), oldNetworksList, newNetworksList, networkInterfaces)
		if err != nil {
			resp.Diagnostics.AddError(
				virtualmachines.ErrSummaryErrorUpdatingNetworkInterfaces,
				err.Error(),
			)
			return
		}
	}

	// If we are changing our public IP allocation, need to call public-ip
	if plan.AllocatePublicIp != state.AllocatePublicIp {
		// Cannot use the call from above since these were just updated
		networkInterfaces, err := networks.GetNetworkInterfaces(r.client, ctx, state.ID.ValueString())
		if err != nil {
			resp.Diagnostics.AddError(
				virtualmachines.ErrSummaryErrorRetrievingNetworkIfaces,
				err.Error(),
			)
			return
		}
		// Find the primary network interface
		interfaceIdx := slices.IndexFunc(networkInterfaces, func(inter networks.ReadVirtualMachineNetworkDataResponseTF) bool {
			return inter.IsPrimary == types.Int64Value(1)
		})
		// This means none are set to primary as far as we can tell, which should be impossible since there must always be one
		if interfaceIdx < 0 {
			resp.Diagnostics.AddError(
				virtualmachines.ErrSummaryErrorRetrievingNetworkIfaces,
				virtualmachines.ErrDetailNetworkInterfacesForVM,
			)
			return
		}
		primaryNetworkInterfaceId := networkInterfaces[interfaceIdx].ID.ValueString()

		// If this is true, allocate IP
		if plan.AllocatePublicIp.ValueBool() {
			err := networks.AllocatePublicIp(r.client, ctx, plan.ID.ValueString(), primaryNetworkInterfaceId)
			if err != nil {
				resp.Diagnostics.AddError(
					virtualmachines.ErrSummaryUnableToUpdatePublicIPConfiguration,
					err.Error(),
				)
				return
			}
		} else {
			err := networks.ReleasePublicIp(r.client, ctx, plan.ID.ValueString(), primaryNetworkInterfaceId)
			if err != nil {
				resp.Diagnostics.AddError(
					virtualmachines.ErrSummaryUnableToUpdatePublicIPConfiguration,
					err.Error(),
				)
				return
			}
		}
	}

	// If size is updated, need to call resize
	if plan.Size != state.Size {
		newSizeId, err := virtualmachines.ValidatePlanSizeLargerThanStateSize(r.client, ctx, state, plan)
		if err != nil {
			resp.Diagnostics.AddError(
				virtualmachines.ErrSummaryErrorUpdatingVMSize,
				err.Error(),
			)
			return
		}

		tflog.Info(ctx, virtualmachines.LogPerformingVirtualMachineResize)
		// Re-size the virtual machine
		err = virtualmachines.UpdateVirtualMachineSize(r.client, ctx, plan.ID.ValueString(), newSizeId)
		if err != nil {
			resp.Diagnostics.AddError(
				virtualmachines.ErrSummaryErrorUpdatingVMSize,
				err.Error(),
			)
			return
		}
	}

	// Check for name updated
	if plan.Name != state.Name {
		tflog.Info(ctx, virtualmachines.LogNameChangedUpdatingVirtualMachine)
		err := virtualmachines.UpdateVirtualMachine(r.client, ctx, state.ID.ValueString(), plan.Name.ValueString())
		if err != nil {
			resp.Diagnostics.AddError(
				virtualmachines.ErrSummaryErrorUpdatingVMName,
				err.Error(),
			)
			return
		}
	}

	// If volume ids are updated, need to call attach/detach volume
	if !slices.Equal(plan.VolumeIds.Elements(), state.VolumeIds.Elements()) {
		var oldVolumesList, newVolumesList []string
		state.VolumeIds.ElementsAs(ctx, &oldVolumesList, true)
		plan.VolumeIds.ElementsAs(ctx, &newVolumesList, true)

		err := virtualmachines.UpdateVolumes(r.client, ctx, plan.ID.ValueString(), oldVolumesList, newVolumesList)
		if err != nil {
			resp.Diagnostics.AddError(
				virtualmachines.ErrSummaryErrorUpdatingVolumes,
				err.Error(),
			)
			return
		}
	}

	// Perform a GET call to retrieve actual information about the Virtual Machine
	tflog.Info(ctx, virtualmachines.LogAllVMUpdateOpsCompleteRetrievingLatestInfo)
	getVirtualMachineResponse, err := virtualmachines.GetVirtualMachine(r.client, ctx, plan.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError(
			virtualmachines.ErrSummaryRetrievingVMInfoFailed,
			virtualmachines.ErrDetailVMInfoFailedCanImport+": "+err.Error(),
		)
		return
	}

	imageId, images, err := virtualmachines.GetVirtualMachineImageId(r.client, ctx, plan.DatacenterId.ValueString(), plan.Image.ValueString())

	if err != nil {
		resp.Diagnostics.AddError(
			virtualmachines.ErrSummaryErrorVerifyingImage,
			fmt.Sprintf(virtualmachines.ErrDetailImageVerificationFailed, plan.Image.ValueString(), plan.DatacenterId.ValueString())+": "+err.Error(),
		)
		return
	}

	_, sizes, err := virtualmachines.GetVirtualMachineSizeId(r.client, ctx, imageId, plan.DatacenterId.ValueString(), plan.Size.ValueString())
	if err != nil {
		resp.Diagnostics.AddError(
			virtualmachines.ErrSummaryErrorVerifyingSize,
			fmt.Sprintf(virtualmachines.ErrDetailSizeVerificationFailed, plan.Size.ValueString(), plan.DatacenterId.ValueString())+": "+err.Error(),
		)
		return
	}

	tflog.Info(ctx, virtualmachines.LogRetrievedLatestVMInfoMappingToModel)
	plan = virtualmachines.MapVirtualMachineResponseToModel(ctx, getVirtualMachineResponse, images, sizes, plan)

	// Once finished, conditionally start the virtual machine again
	if needStopVM {
		err = virtualmachines.StartVirtualMachine(r.client, ctx, state.ID.ValueString(), plan.WaitForStartup.ValueBool())
		if err != nil {
			resp.Diagnostics.AddWarning(
				virtualmachines.WarnSummaryUnableToStartVM,
				fmt.Sprintf(virtualmachines.ErrDetailStartingVM, state.ID.ValueString())+": "+err.Error(),
			)
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
			fmt.Sprintf(virtualmachines.ErrDetailStoppingVM, state.ID.ValueString())+": "+err.Error(),
		)
		return
	}

	// Before deleting, detach any network interfaces first
	if !state.NetworkIds.IsNull() {
		networkInterfaces, err := networks.GetNetworkInterfaces(r.client, ctx, state.ID.ValueString())

		if err != nil {
			resp.Diagnostics.AddError(
				virtualmachines.ErrSummaryErrorRetrievingNetworkIfaces,
				fmt.Sprintf(virtualmachines.ErrDetailNetworkInterfacesForVM, state.ID.ValueString())+": "+err.Error(),
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
						fmt.Sprintf(virtualmachines.WarnDetailRemovingNetworkInterfaceWithIDFailed, adapter.ID.ValueString())+": "+err.Error(),
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
					fmt.Sprintf(virtualmachines.WarnDetailRemovingVolumeWithIDFailed, volumeId)+": "+err.Error(),
				)
			}
		}
	}

	request, err := http.NewRequest("DELETE", virtualmachines.BASE_URL+state.ID.ValueString(), nil)
	if err != nil {
		resp.Diagnostics.AddError(
			virtualmachines.ErrSummaryUnableToCreateDeleteRequest,
			err.Error(),
		)
		return
	}
	tflog.Info(ctx, virtualmachines.LogConstructedDeleteGPCNVirtualMachineRequest)

	response, err := r.client.Do(request)
	if err != nil {
		resp.Diagnostics.AddError(
			virtualmachines.ErrSummaryUnableToDeleteVM,
			fmt.Sprintf(virtualmachines.ErrDetailUnableToDeleteVMWithID, state.ID.ValueString())+": "+err.Error(),
		)
		return
	}
	tflog.Info(ctx, virtualmachines.LogIssuedDeleteGPCNVirtualMachineJob)
	defer response.Body.Close()

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
			fmt.Sprintf(virtualmachines.ErrDetailUnmarshalingDeleteWithID, state.ID.ValueString())+": "+err.Error(),
		)
		return
	}

	_, err = client.PerformLongPolling(r.client, ctx, "Delete GPCN Virtual Machine", deleteVirtualMachineResponse.Data.JobID)

	if err != nil {
		resp.Diagnostics.AddError(
			virtualmachines.ErrSummaryEncounteredErrorGettingJobInfo,
			virtualmachines.ErrDetailJobInfoCheckDashboard+": "+err.Error(),
		)
		return
	}

	tflog.Info(ctx, virtualmachines.LogSuccessfullyFinishedDeleteGPCNVirtualMachine)
}

func (r *virtualMachinesResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

/*
  - Some actions can be done without stopping the VM. Since it's a heavy time investment to start and stop, determine that and use it for the rest of the update logic
    Cases where VM needs to be stopped:
  - NetworkIds change
  - VolumeIds change
  - Size changes

*
*/
func determineIfVMNeedsStopped(state, plan virtualmachines.ResourceModel) bool {
	return (!slices.Equal(plan.NetworkIds.Elements(), state.NetworkIds.Elements())) ||
		(!slices.Equal(plan.VolumeIds.Elements(), state.VolumeIds.Elements())) ||
		state.Size != plan.Size
}

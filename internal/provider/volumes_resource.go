package provider

import (
	"context"
	"fmt"

	"terraform-provider-gpcn/internal/client"
	"terraform-provider-gpcn/internal/volumes"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/mapplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

// Ensure the implementation satisfies the expected interfaces.
var (
	_ resource.Resource                = &volumesResource{}
	_ resource.ResourceWithConfigure   = &volumesResource{}
	_ resource.ResourceWithImportState = &volumesResource{}
)

// NewVolumesResource is a helper function to simplify the provider implementation.
func NewVolumesResource() resource.Resource {
	return &volumesResource{}
}

// volumesResource is the resource implementation.
type volumesResource struct {
	client *client.GpcnClient
}

// Metadata returns the resource type name.
func (r *volumesResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_volume"
}

// Schema defines the schema for the resource.
func (r *volumesResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a storage volume that can be attached to virtual machines",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "Unique identifier for the volume in UUID format",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				Description: "Human-readable name for the volume. Must be 1-255 characters. Changing this value requires replacing the volume",
				Required:    true,
				Validators: []validator.String{
					stringvalidator.LengthBetween(1, 255),
				},
				PlanModifiers: []planmodifier.String{
					// GPCN cannot rename a volume, so a new name replaces it.
					stringplanmodifier.RequiresReplace(),
				},
			},
			"datacenter_id": schema.StringAttribute{
				Description: "Unique identifier of the data center where the volume will be created. Changing this value requires replacing the volume",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					// Changing the datacenter_id requires us to destroy and create a new volume
					stringplanmodifier.RequiresReplace(),
				},
			},
			"volume_type": schema.StringAttribute{
				Description: "Type of storage: 'SSD', 'NVMe', or a storage component code such as 'vol-add-ultra'. Use \"SSD\" or \"NVMe\" for the built-in storage classes and the component code for any other class. The data center decides which codes it offers, and a code it does not offer is refused with the list of codes it does offer. Changing this value requires replacing the volume. A volume whose SKU the platform cannot resolve reads \"Unknown\"; after the platform repairs it, remove the volume from state and import it again so the code is recorded. The built-in codes vol-add-ssd and vol-add-nvme are refused at plan time; write SSD or NVMe instead.",
				Required:    true,
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
					volumes.TypeSpellingValidator{},
				},
				PlanModifiers: []planmodifier.String{
					// Changing the volume_type requires us to destroy and create a new volume
					stringplanmodifier.RequiresReplace(),
				},
			},
			"volume_type_code": schema.StringAttribute{
				Description: "Component code of the storage class, for example 'vol-add-ssd'. This is the identifier the API uses for a volume type",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					// A resize cannot change the storage class, so the known code survives it.
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"volume_type_id": schema.Int64Attribute{
				Description:        "Always null. The API identifies a volume type by code",
				Computed:           true,
				DeprecationMessage: "volume_type_id is deprecated and always null: the API identifies a volume type by code. Use volume_type_code.",
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
				},
			},
			"size_gb": schema.Int64Attribute{
				Description: "Size of the volume in GB. Can be increased without replacement, but shrinking requires replacing the volume",
				Required:    true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.RequiresReplaceIf(func(ctx context.Context, req planmodifier.Int64Request, resp *int64planmodifier.RequiresReplaceIfFuncResponse) {
						// Requires replace if new value is less than current value
						resp.RequiresReplace = req.PlanValue.ValueInt64() < req.StateValue.ValueInt64()
					}, "Requires a replacement if the plan value is less than the current state value", "Requires a replacement if the plan value is less than the current state value"),
				},
			},
			"created_time": schema.StringAttribute{
				Description: "Timestamp when the volume was created in ISO-8601 format",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"last_updated": schema.StringAttribute{
				Description: "Timestamp when the volume was last updated in ISO-8601 format",
				Computed:    true,
			},
			"resource_group_id": schema.StringAttribute{
				Description: "ID of the resource group this volume belongs to. When omitted, the volume is created in the tenant's Default resource group and this attribute reports that group. Changing it moves the volume in place. Removing it from configuration leaves the volume where it is, but a replacement created while it is unset joins the Default resource group",
				Optional:    true,
				Computed:    true,
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseNonNullStateForUnknown(),
				},
			},
			"location": schema.MapAttribute{
				Description: "Location details including data center, region, and country information",
				ElementType: types.StringType,
				Computed:    true,
				PlanModifiers: []planmodifier.Map{
					mapplanmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

// Configure adds the provider configured client to the resource.
func (r *volumesResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	gpcnClient, ok := req.ProviderData.(*client.GpcnClient)

	if !ok {
		resp.Diagnostics.AddError(
			volumes.ErrSummaryUnexpectedConfigureType,
			fmt.Sprintf(volumes.ErrDetailExpectedGpcnClient, req.ProviderData),
		)

		return
	}

	r.client = gpcnClient
}

// Create creates the resource and sets the initial Terraform state.
func (r *volumesResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	// Add correlation ID for request tracing
	ctx = client.WithCorrelationID(ctx)
	tflog.Info(ctx, volumes.LogStartingCreateGPCNVolume)
	// Retrieve values from plan
	var plan volumes.ResourceModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	getVolumeResponse, err := volumes.CreateVolume(r.client, ctx, plan)
	if err != nil {
		resp.Diagnostics.AddError(
			volumes.ErrSummaryUnableToCreateVolume,
			err.Error(),
		)
		return
	}

	plannedGroup := plan.ResourceGroupId
	plan = volumes.MapVolumeResponseToModel(ctx, getVolumeResponse, plan)
	plan.ResourceGroupId = keepPlannedVolumeGroup(plannedGroup, plan.ResourceGroupId)

	// Set state to fully populated data
	diags = resp.State.Set(ctx, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Info(ctx, volumes.LogSuccessfullyFinishedCreateGPCNVolume)
}

// Read refreshes the Terraform state with the latest data.
func (r *volumesResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	// Add correlation ID for request tracing
	ctx = client.WithCorrelationID(ctx)
	tflog.Info(ctx, volumes.LogStartingReadGPCNVolume)
	// Get current state
	var state volumes.ResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	getVolumeResponse, err := volumes.GetVolume(r.client, ctx, state.ID.ValueString())
	if err != nil {
		// Resource was deleted outside of Terraform
		if client.IsNotFound(err) {
			tflog.Info(ctx, volumes.LogVolumeNotFoundRemovingFromState)
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError(
			volumes.ErrSummaryUnableToGetVolume,
			fmt.Sprintf(volumes.ErrDetailUnableToGetVolumeWithID, state.ID.ValueString())+": "+err.Error(),
		)
		return
	}

	state = volumes.MapVolumeResponseToModel(ctx, getVolumeResponse, state)

	// Set state to fully populated data
	diags = resp.State.Set(ctx, state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Info(ctx, volumes.LogSuccessfullyFinishedReadGPCNVolume)
}

// Update updates the resource and sets the updated Terraform state on success.
func (r *volumesResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	// Add correlation ID for request tracing
	ctx = client.WithCorrelationID(ctx)
	tflog.Info(ctx, volumes.LogStartingUpdateGPCNVolume)
	var plan volumes.ResourceModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	var state volumes.ResourceModel
	diags = req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	volumeId := plan.ID.ValueString()

	sizeChanged := !plan.SizeGb.Equal(state.SizeGb)
	if sizeChanged {
		componentCode := volumes.ComponentCodeForVolumeType(plan.VolumeType.ValueString())
		if _, err := volumes.GetVolumeSkuId(r.client, ctx, plan.DatacenterId.ValueString(), componentCode, plan.SizeGb.ValueInt64()); err != nil {
			resp.Diagnostics.AddError(
				volumes.ErrSummaryUnableToUpdateVolume,
				fmt.Sprintf(volumes.ErrDetailUnableToUpdateVolumeWithID, volumeId)+": "+err.Error(),
			)
			return
		}
	}

	groupChanged := !plan.ResourceGroupId.IsUnknown() && !plan.ResourceGroupId.IsNull() &&
		!plan.ResourceGroupId.Equal(state.ResourceGroupId)
	if groupChanged {
		if err := volumes.MoveVolume(r.client, ctx, volumeId, plan.ResourceGroupId.ValueString()); err != nil {
			resp.Diagnostics.AddError(
				volumes.ErrSummaryUnableToUpdateVolume,
				fmt.Sprintf(volumes.ErrDetailUnableToUpdateVolumeWithID, volumeId)+": "+err.Error(),
			)
			return
		}
	}

	var getVolumeResponse *volumes.ReadVolumesResponse
	var err error
	if sizeChanged {
		getVolumeResponse, err = volumes.UpdateVolume(r.client, ctx, volumeId, plan)
	} else {
		getVolumeResponse, err = volumes.GetVolume(r.client, ctx, volumeId)
	}
	if err != nil {
		resp.Diagnostics.AddError(
			volumes.ErrSummaryUnableToUpdateVolume,
			fmt.Sprintf(volumes.ErrDetailUnableToUpdateVolumeWithID, volumeId)+": "+err.Error(),
		)
		return
	}

	plannedGroup := plan.ResourceGroupId
	plan = volumes.MapVolumeResponseToModel(ctx, getVolumeResponse, plan)
	plan.ResourceGroupId = keepPlannedVolumeGroup(plannedGroup, plan.ResourceGroupId)

	// Set state to fully populated data
	diags = resp.State.Set(ctx, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Info(ctx, volumes.LogSuccessfullyFinishedUpdateGPCNVolume)
}

// Delete deletes the resource and removes the Terraform state on success.
func (r *volumesResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	// Add correlation ID for request tracing
	ctx = client.WithCorrelationID(ctx)
	tflog.Info(ctx, volumes.LogStartingDeleteGPCNVolume)
	var state volumes.ResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := volumes.DeleteVolume(r.client, ctx, state.ID.ValueString())
	if client.IsNotFound(err) {
		// Already deleted outside of Terraform
		tflog.Info(ctx, volumes.LogVolumeAlreadyDeleted)
	} else if err != nil {
		resp.Diagnostics.AddError(
			volumes.ErrSummaryUnableToDeleteVolume,
			fmt.Sprintf(volumes.ErrDetailUnableToDeleteVolumeWithID, state.ID.ValueString())+": "+err.Error(),
		)
		return
	}

	tflog.Info(ctx, volumes.LogSuccessfullyFinishedDeleteGPCNVolume)
}

func (r *volumesResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// A known planned group must reach state unchanged, or Terraform fails the
// apply as inconsistent. A group moved out of band during the apply then shows
// as drift on the next plan.
func keepPlannedVolumeGroup(planned, reported types.String) types.String {
	if planned.IsUnknown() {
		return reported
	}
	return planned
}

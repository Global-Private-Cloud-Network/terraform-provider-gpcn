package provider

import (
	"context"
	"fmt"
	"net/http"
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
	client *http.Client
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
				Description: "Human-readable name for the volume. Changing this value requires replacing the volume",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					// Changing the name requires us to destroy and create a new volume. There's no update call for volumes
					stringplanmodifier.RequiresReplace(),
				},
			},
			"datacenter_id": schema.StringAttribute{
				Description: "Unique identifier of the datacenter where the volume will be created. Changing this value requires replacing the volume",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					// Changing the datacenter_id requires us to destroy and create a new volume
					stringplanmodifier.RequiresReplace(),
				},
			},
			"volume_type": schema.StringAttribute{
				Description: "Type of storage: either 'SSD' or 'NVMe'. Changing this value requires replacing the volume. Note that not all volume types are available for every datacenter",
				Required:    true,
				Validators: []validator.String{
					stringvalidator.OneOf("SSD", "NVMe"),
				},
				PlanModifiers: []planmodifier.String{
					// Changing the volume_type requires us to destroy and create a new volume
					stringplanmodifier.RequiresReplace(),
				},
			},
			"volume_type_id": schema.Int64Attribute{
				Description: "Internal identifier for the volume type",
				Computed:    true,
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
			"location": schema.MapAttribute{
				Description: "Location details including datacenter, region, and country information",
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

	client, ok := req.ProviderData.(*http.Client)

	if !ok {
		resp.Diagnostics.AddError(
			volumes.ErrSummaryUnexpectedConfigureType,
			fmt.Sprintf(volumes.ErrDetailExpectedHTTPClient, req.ProviderData),
		)

		return
	}

	r.client = client
}

// Create creates the resource and sets the initial Terraform state.
func (r *volumesResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
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

	plan = volumes.MapVolumeResponseToModel(ctx, getVolumeResponse, plan)

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
	tflog.Info(ctx, volumes.LogStartingUpdateGPCNVolume)
	var plan volumes.ResourceModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	getVolumeResponse, err := volumes.UpdateVolume(r.client, ctx, plan.ID.ValueString(), plan)
	if err != nil {
		resp.Diagnostics.AddError(
			volumes.ErrSummaryUnableToUpdateVolume,
			fmt.Sprintf(volumes.ErrDetailUnableToUpdateVolumeWithID, plan.ID.ValueString())+": "+err.Error(),
		)
		return
	}

	plan = volumes.MapVolumeResponseToModel(ctx, getVolumeResponse, plan)

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
	tflog.Info(ctx, volumes.LogStartingDeleteGPCNVolume)
	var state volumes.ResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := volumes.DeleteVolume(r.client, ctx, state.ID.ValueString())
	if err != nil {
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

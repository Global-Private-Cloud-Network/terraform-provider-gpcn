package provider

import (
	"context"
	"fmt"
	"net/http"
	"terraform-provider-gpcn/internal/gpu"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

// Ensure the implementation satisfies the expected interfaces.
var (
	_ resource.Resource                = &gpuResource{}
	_ resource.ResourceWithConfigure   = &gpuResource{}
	_ resource.ResourceWithImportState = &gpuResource{}
)

// NewGPUResource is a helper function to simplify the provider implementation.
func NewGPUResource() resource.Resource {
	return &gpuResource{}
}

// gpuResource is the resource implementation.
type gpuResource struct {
	client *http.Client
}

// Metadata returns the resource type name.
func (r *gpuResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_gpu"
}

// Schema defines the schema for the resource.
func (r *gpuResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a GPU resource in GPCN",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "Unique identifier for the GPU in UUID format",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				Description: "Human-readable name for the GPU",
				Required:    true,
			},
			"datacenter_id": schema.StringAttribute{
				Description: "Unique identifier of the datacenter where the GPU will be created. Changing this value requires replacing the GPU",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					// Changing the datacenter_id requires us to destroy and create a new GPU
					stringplanmodifier.RequiresReplace(),
				},
			},
			"series_name": schema.StringAttribute{
				Description: "Human-readable name of the GPU series. Exactly one of series_name or series_code must be specified",
				Optional:    true,
				Validators: []validator.String{
					stringvalidator.ExactlyOneOf(path.Expressions{
						path.MatchRoot("series_code"),
					}...),
					stringvalidator.OneOf(gpu.GPUSeriesNames...),
				},
				PlanModifiers: []planmodifier.String{
					// Changing the series_name requires us to destroy and create a new GPU
					stringplanmodifier.RequiresReplace(),
				},
			},
			"series_code": schema.StringAttribute{
				Description: "Short code of the GPU series. Exactly one of series_name or series_code must be specified",
				Optional:    true,
				Validators: []validator.String{
					stringvalidator.ExactlyOneOf(path.Expressions{
						path.MatchRoot("series_name"),
					}...),
					stringvalidator.OneOf(gpu.GPUSeriesCodes...),
				},
				PlanModifiers: []planmodifier.String{
					// Changing the series_code requires us to destroy and create a new GPU
					stringplanmodifier.RequiresReplace(),
				},
			},
			"gpu_count": schema.Int64Attribute{
				Description: "The number of GPUs tied to the Virtual Machine. Must be 1, 2, or 4",
				Required:    true,
				PlanModifiers: []planmodifier.Int64{
					// Changing the gpu_count requires us to destroy and create a new GPU
					int64planmodifier.RequiresReplace(),
				},
				Validators: []validator.Int64{
					int64validator.OneOf(1, 2, 4),
				},
			},
			"created_time": schema.StringAttribute{
				Description: "Timestamp when the GPU was created in ISO-8601 format",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"last_updated": schema.StringAttribute{
				Description: "Timestamp when the GPU was last updated in ISO-8601 format",
				Computed:    true,
			},
			"location": schema.MapAttribute{
				Description: "Location details including datacenter, region, and country information",
				ElementType: types.StringType,
				Computed:    true,
			},
		},
	}
}

// Configure adds the provider configured client to the resource.
func (r *gpuResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	client, ok := req.ProviderData.(*http.Client)

	if !ok {
		resp.Diagnostics.AddError(
			gpu.ErrSummaryUnexpectedConfigureType,
			fmt.Sprintf(gpu.ErrDetailExpectedHTTPClient, req.ProviderData),
		)

		return
	}

	r.client = client
}

// Create creates the resource and sets the initial Terraform state.
func (r *gpuResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	tflog.Info(ctx, gpu.LogStartingCreateGPU)

	// Retrieve values from plan
	var plan gpu.ResourceModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	// TODO: Check series code availability with datacenterId and GPU Count

	// Call no-op Create function
	getGPUResponse, err := gpu.CreateGPU(r.client, ctx, plan)
	if err != nil {
		resp.Diagnostics.AddError(
			gpu.ErrSummaryUnableToCreateGPU,
			err.Error(),
		)
		return
	}

	plan = gpu.MapGPUResponseToModel(ctx, getGPUResponse, plan)

	// Set state to fully populated data
	diags = resp.State.Set(ctx, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Info(ctx, gpu.LogSuccessfullyFinishedCreateGPU)
}

// Read refreshes the Terraform state with the latest data.
func (r *gpuResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	tflog.Info(ctx, gpu.LogStartingReadGPU)

	// Get current state
	var state gpu.ResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Call no-op Read function
	getGPUResponse, err := gpu.GetGPU(r.client, ctx, state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError(
			gpu.ErrSummaryUnableToReadGPU,
			fmt.Sprintf(gpu.ErrDetailReadGPUFailed, state.ID.ValueString())+": "+err.Error(),
		)
		return
	}

	state = gpu.MapGPUResponseToModel(ctx, getGPUResponse, state)

	// Set state to fully populated data
	diags = resp.State.Set(ctx, state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Info(ctx, gpu.LogSuccessfullyFinishedReadGPU)
}

// Update updates the resource and sets the updated Terraform state on success.
func (r *gpuResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	tflog.Info(ctx, gpu.LogStartingUpdateGPU)

	// Retrieve values from plan
	var plan gpu.ResourceModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	var state gpu.ResourceModel
	diags = req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Call no-op Update function
	err := gpu.UpdateGPU(r.client, ctx, state.ID.ValueString(), plan)
	if err != nil {
		resp.Diagnostics.AddError(
			gpu.ErrSummaryUnableToUpdateGPU,
			fmt.Sprintf(gpu.ErrDetailUpdateGPUFailed, state.ID.ValueString())+": "+err.Error(),
		)
		return
	}

	// Perform a GET call to retrieve actual information about the GPU
	getGPUResponse, err := gpu.GetGPU(r.client, ctx, state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError(
			gpu.ErrSummaryUnableToReadGPU,
			fmt.Sprintf(gpu.ErrDetailReadGPUFailed, state.ID.ValueString())+": "+err.Error(),
		)
		return
	}

	plan = gpu.MapGPUResponseToModel(ctx, getGPUResponse, plan)

	// Set state to fully populated data
	diags = resp.State.Set(ctx, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Info(ctx, gpu.LogSuccessfullyFinishedUpdateGPU)
}

// Delete deletes the resource and removes the Terraform state on success.
func (r *gpuResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	tflog.Info(ctx, gpu.LogStartingDeleteGPU)

	var state gpu.ResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Call no-op Delete function
	err := gpu.DeleteGPU(r.client, ctx, state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError(
			gpu.ErrSummaryUnableToDeleteGPU,
			fmt.Sprintf(gpu.ErrDetailDeleteGPUFailed, state.ID.ValueString())+": "+err.Error(),
		)
		return
	}

	tflog.Info(ctx, gpu.LogSuccessfullyFinishedDeleteGPU)
}

func (r *gpuResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

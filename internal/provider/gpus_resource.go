package provider

import (
	"context"
	"fmt"
	"regexp"

	"terraform-provider-gpcn/internal/client"
	"terraform-provider-gpcn/internal/gpu"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
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
	client *client.GpcnClient
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
				Description: "Human-readable name for the GPU. Must be 1-60 characters, starting and ending with an alphanumeric character, containing only letters, digits, spaces, periods, and hyphens",
				Required:    true,
				Validators: []validator.String{
					stringvalidator.LengthBetween(1, 60),
					stringvalidator.RegexMatches(
						regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9 .\-]*[a-zA-Z0-9])?$`),
						"Name must start and end with an alphanumeric character and contain only letters, digits, spaces, periods, and hyphens",
					),
				},
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
				Computed:    true,
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
				Computed:    true,
				Validators: []validator.String{
					stringvalidator.ExactlyOneOf(path.Expressions{
						path.MatchRoot("series_name"),
					}...),
					stringvalidator.OneOf(gpu.GPUSeriesCodes...),
				},
				PlanModifiers: []planmodifier.String{
					// Changing the series_code requires us to destroy and create a new GPU
					stringplanmodifier.RequiresReplace(),
					stringplanmodifier.UseStateForUnknown(),
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
			"image_name": schema.StringAttribute{
				Description: "The operating system image to use for the GPU. Must be one of: \"ubuntu-22.04\" or \"ubuntu-24.04\"",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					// Changing the image_name requires us to destroy and create a new GPU
					stringplanmodifier.RequiresReplace(),
				},
				Validators: []validator.String{
					stringvalidator.OneOf(gpu.GPUImageNames...),
				},
			},
			"initial_auth": schema.SingleNestedAttribute{
				Description: "Initial authentication configuration for the GPU. This block is only applied at creation time; subsequent changes update the Terraform state only and do not affect the running machine",
				Required:    true,
				Attributes: map[string]schema.Attribute{
					"ssh_key_id": schema.StringAttribute{
						Description: "ID of the SSH key to use for authentication",
						Required:    true,
					},
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
				PlanModifiers: []planmodifier.Map{
					mapplanmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

// Configure adds the provider configured client to the resource.
func (r *gpuResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	gpcnClient, ok := req.ProviderData.(*client.GpcnClient)

	if !ok {
		resp.Diagnostics.AddError(
			gpu.ErrSummaryUnexpectedConfigureType,
			fmt.Sprintf(gpu.ErrDetailExpectedGpcnClient, req.ProviderData),
		)

		return
	}

	r.client = gpcnClient
}

// Create creates the resource and sets the initial Terraform state.
func (r *gpuResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	// Add correlation ID for request tracing
	ctx = client.WithCorrelationID(ctx)
	tflog.Info(ctx, gpu.LogStartingCreateGPU)

	// Retrieve values from plan
	var plan gpu.ResourceModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// If series code is not populated, perform a lookup and set it
	if plan.SeriesCode.IsNull() || plan.SeriesCode.ValueString() == "" {
		code := gpu.GPUSeriesNameToCode[plan.SeriesName.ValueString()]
		plan.SeriesCode = types.StringValue(code)
	}

	// Check series code availability with datacenterId and GPU Count
	inventory, err := gpu.CheckInventory(r.client, ctx, plan)

	if err != nil {
		resp.Diagnostics.AddError(
			"Unable to create GPCN GPU",
			err.Error(),
		)
		return
	}

	// Extract the series ID from the inventory response
	seriesId := inventory.Data.Series[0].ID

	getGPUResponse, err := gpu.CreateGPU(r.client, ctx, seriesId, plan)
	if err != nil {
		if handlePartialCreate(ctx, err, req.Plan, resp, gpu.ErrSummaryUnableToCreateGPU) {
			return
		}
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
	// Add correlation ID for request tracing
	ctx = client.WithCorrelationID(ctx)
	tflog.Info(ctx, gpu.LogStartingReadGPU)

	// Get current state
	var state gpu.ResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	getGPUResponse, err := gpu.GetGPU(r.client, ctx, state.ID.ValueString())
	if err != nil {
		// Resource was deleted outside of Terraform
		if client.IsNotFound(err) {
			tflog.Info(ctx, gpu.LogGPUNotFoundRemovingFromState)
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError(
			gpu.ErrSummaryUnableToReadGPU,
			fmt.Errorf("%s: %w", fmt.Sprintf(gpu.ErrDetailReadGPUFailed, state.ID.ValueString()), err).Error(),
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
	// Add correlation ID for request tracing
	ctx = client.WithCorrelationID(ctx)
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

	err := gpu.UpdateGPU(r.client, ctx, state.ID.ValueString(), plan.Name.ValueString())
	if err != nil {
		resp.Diagnostics.AddError(
			gpu.ErrSummaryUnableToUpdateGPU,
			fmt.Errorf("%s: %w", fmt.Sprintf(gpu.ErrDetailUpdateGPUFailed, state.ID.ValueString()), err).Error(),
		)
		return
	}

	// Perform a GET call to retrieve actual information about the GPU
	getGPUResponse, err := gpu.GetGPU(r.client, ctx, state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError(
			gpu.ErrSummaryUnableToReadGPU,
			fmt.Errorf("%s: %w", fmt.Sprintf(gpu.ErrDetailReadGPUFailed, state.ID.ValueString()), err).Error(),
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
	// Add correlation ID for request tracing
	ctx = client.WithCorrelationID(ctx)
	tflog.Info(ctx, gpu.LogStartingDeleteGPU)

	var state gpu.ResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := gpu.DeleteGPU(r.client, ctx, state.ID.ValueString())
	if client.IsNotFound(err) {
		// Already deleted outside of Terraform
		tflog.Info(ctx, gpu.LogGPUAlreadyDeleted)
	} else if err != nil {
		resp.Diagnostics.AddError(
			gpu.ErrSummaryUnableToDeleteGPU,
			fmt.Errorf("%s: %w", fmt.Sprintf(gpu.ErrDetailDeleteGPUFailed, state.ID.ValueString()), err).Error(),
		)
		return
	}

	tflog.Info(ctx, gpu.LogSuccessfullyFinishedDeleteGPU)
}

func (r *gpuResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

package provider

import (
	"context"
	"fmt"

	"terraform-provider-gpcn/internal/client"
	"terraform-provider-gpcn/internal/resourcegroups"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

// Ensure the implementation satisfies the expected interfaces.
var (
	_ resource.Resource                = &resourceGroupResource{}
	_ resource.ResourceWithConfigure   = &resourceGroupResource{}
	_ resource.ResourceWithImportState = &resourceGroupResource{}
)

// NewResourceGroupResource is a helper function to simplify the provider implementation.
func NewResourceGroupResource() resource.Resource {
	return &resourceGroupResource{}
}

// resourceGroupResource is the resource implementation.
type resourceGroupResource struct {
	client *client.GpcnClient
}

// Metadata returns the resource type name.
func (r *resourceGroupResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_resource_group"
}

// Schema defines the schema for the resource.
func (r *resourceGroupResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a resource group in GPCN. Resource groups bind related resources together as a team.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "Unique identifier for the resource group in UUID format",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				Description: "Human-readable name for the resource group. Must be 1-64 characters",
				Required:    true,
				Validators: []validator.String{
					stringvalidator.LengthBetween(1, 64),
				},
			},
			"description": schema.StringAttribute{
				Description: "Optional description for the resource group. Maximum 255 characters",
				Optional:    true,
				Validators: []validator.String{
					stringvalidator.LengthAtMost(255),
				},
			},
			"created_time": schema.StringAttribute{
				Description: "Timestamp when the resource group was created in ISO-8601 format",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"last_updated": schema.StringAttribute{
				Description: "Timestamp when the resource group was last updated in ISO-8601 format",
				Computed:    true,
			},
		},
	}
}

// Configure adds the provider configured client to the resource.
func (r *resourceGroupResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	gpcnClient, ok := req.ProviderData.(*client.GpcnClient)
	if !ok {
		resp.Diagnostics.AddError(
			resourcegroups.ErrSummaryUnexpectedConfigureType,
			fmt.Sprintf(resourcegroups.ErrDetailExpectedGpcnClient, req.ProviderData),
		)
		return
	}

	r.client = gpcnClient
}

// Create creates the resource and sets the initial Terraform state.
func (r *resourceGroupResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	ctx = client.WithCorrelationID(ctx)
	tflog.Info(ctx, resourcegroups.LogStartingCreateResourceGroup)

	var plan resourcegroups.ResourceModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	resourceGroupResponse, err := resourcegroups.CreateResourceGroup(r.client, ctx, plan)
	if err != nil {
		resp.Diagnostics.AddError(
			resourcegroups.ErrSummaryUnableToCreateResourceGroup,
			err.Error(),
		)
		return
	}

	plan = resourcegroups.MapResourceGroupResponseToModel(resourceGroupResponse, plan)

	diags = resp.State.Set(ctx, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Info(ctx, resourcegroups.LogSuccessfullyFinishedCreateResourceGroup)
}

// Read refreshes the Terraform state with the latest data.
func (r *resourceGroupResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	ctx = client.WithCorrelationID(ctx)
	tflog.Info(ctx, resourcegroups.LogStartingReadResourceGroup)

	var state resourcegroups.ResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	resourceGroupResponse, err := resourcegroups.GetResourceGroup(r.client, ctx, state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError(
			resourcegroups.ErrSummaryUnableToReadResourceGroup,
			fmt.Errorf("%s: %w", fmt.Sprintf(resourcegroups.ErrDetailReadResourceGroupFailed, state.ID.ValueString()), err).Error(),
		)
		return
	}

	state = resourcegroups.MapResourceGroupResponseToModel(resourceGroupResponse, state)

	diags = resp.State.Set(ctx, state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Info(ctx, resourcegroups.LogSuccessfullyFinishedReadResourceGroup)
}

// Update updates the resource and sets the updated Terraform state on success.
func (r *resourceGroupResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	ctx = client.WithCorrelationID(ctx)
	tflog.Info(ctx, resourcegroups.LogStartingUpdateResourceGroup)

	var plan resourcegroups.ResourceModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	var state resourcegroups.ResourceModel
	diags = req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := resourcegroups.UpdateResourceGroup(r.client, ctx, state.ID.ValueString(), plan); err != nil {
		resp.Diagnostics.AddError(
			resourcegroups.ErrSummaryUnableToUpdateResourceGroup,
			fmt.Errorf("%s: %w", fmt.Sprintf(resourcegroups.ErrDetailUpdateResourceGroupFailed, state.ID.ValueString()), err).Error(),
		)
		return
	}

	resourceGroupResponse, err := resourcegroups.GetResourceGroup(r.client, ctx, state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError(
			resourcegroups.ErrSummaryUnableToReadResourceGroup,
			fmt.Errorf("%s: %w", fmt.Sprintf(resourcegroups.ErrDetailReadResourceGroupFailed, state.ID.ValueString()), err).Error(),
		)
		return
	}

	plan = resourcegroups.MapResourceGroupResponseToModel(resourceGroupResponse, plan)

	diags = resp.State.Set(ctx, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Info(ctx, resourcegroups.LogSuccessfullyFinishedUpdateResourceGroup)
}

// Delete deletes the resource and removes the Terraform state on success.
func (r *resourceGroupResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	ctx = client.WithCorrelationID(ctx)
	tflog.Info(ctx, resourcegroups.LogStartingDeleteResourceGroup)

	var state resourcegroups.ResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := resourcegroups.DeleteResourceGroup(r.client, ctx, state.ID.ValueString()); err != nil {
		resp.Diagnostics.AddError(
			resourcegroups.ErrSummaryUnableToDeleteResourceGroup,
			fmt.Errorf("%s: %w", fmt.Sprintf(resourcegroups.ErrDetailDeleteResourceGroupFailed, state.ID.ValueString()), err).Error(),
		)
		return
	}

	tflog.Info(ctx, resourcegroups.LogSuccessfullyFinishedDeleteResourceGroup)
}

func (r *resourceGroupResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

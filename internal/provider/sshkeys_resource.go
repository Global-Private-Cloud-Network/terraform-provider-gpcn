package provider

import (
	"context"
	"fmt"

	"terraform-provider-gpcn/internal/client"
	"terraform-provider-gpcn/internal/sshkeys"

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
	_ resource.Resource                = &sshKeyResource{}
	_ resource.ResourceWithConfigure   = &sshKeyResource{}
	_ resource.ResourceWithImportState = &sshKeyResource{}
)

// NewSSHKeyResource is a helper function to simplify the provider implementation.
func NewSSHKeyResource() resource.Resource {
	return &sshKeyResource{}
}

// sshKeyResource is the resource implementation.
type sshKeyResource struct {
	client *client.GpcnClient
}

// Metadata returns the resource type name.
func (r *sshKeyResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_ssh_key"
}

// Schema defines the schema for the resource.
func (r *sshKeyResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages an SSH key resource in GPCN by uploading a public key.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "Unique identifier for the SSH key in UUID format",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				Description: "Human-readable name for the SSH key. Must be 1-30 characters",
				Required:    true,
				Validators: []validator.String{
					stringvalidator.LengthBetween(1, 30),
				},
			},
			"public_key": schema.StringAttribute{
				Description: "The public SSH key to upload",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
			},
			"created_time": schema.StringAttribute{
				Description: "Timestamp when the SSH key was created in ISO-8601 format",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"last_updated": schema.StringAttribute{
				Description: "Timestamp when the SSH key was last updated in ISO-8601 format",
				Computed:    true,
			},
		},
	}
}

// Configure adds the provider configured client to the resource.
func (r *sshKeyResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	gpcnClient, ok := req.ProviderData.(*client.GpcnClient)
	if !ok {
		resp.Diagnostics.AddError(
			sshkeys.ErrSummaryUnexpectedConfigureType,
			fmt.Sprintf(sshkeys.ErrDetailExpectedGpcnClient, req.ProviderData),
		)
		return
	}

	r.client = gpcnClient
}

// Create creates the resource and sets the initial Terraform state.
func (r *sshKeyResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	ctx = client.WithCorrelationID(ctx)
	tflog.Info(ctx, sshkeys.LogStartingCreateSSHKey)

	var plan sshkeys.ResourceModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	sshKeyResponse, err := sshkeys.CreateSSHKey(r.client, ctx, plan)
	if err != nil {
		resp.Diagnostics.AddError(
			sshkeys.ErrSummaryUnableToCreateSSHKey,
			err.Error(),
		)
		return
	}

	plan = sshkeys.MapSSHKeyResponseToModel(sshKeyResponse, plan)

	diags = resp.State.Set(ctx, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Info(ctx, sshkeys.LogSuccessfullyFinishedCreateSSHKey)
}

// Read refreshes the Terraform state with the latest data.
func (r *sshKeyResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	ctx = client.WithCorrelationID(ctx)
	tflog.Info(ctx, sshkeys.LogStartingReadSSHKey)

	var state sshkeys.ResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	sshKeyResponse, err := sshkeys.GetSSHKey(r.client, ctx, state.ID.ValueString())
	if err != nil {
		// Resource was deleted outside of Terraform
		if client.IsNotFound(err) {
			tflog.Info(ctx, sshkeys.LogSSHKeyNotFoundRemovingFromState)
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError(
			sshkeys.ErrSummaryUnableToReadSSHKey,
			fmt.Errorf("%s: %w", fmt.Sprintf(sshkeys.ErrDetailReadSSHKeyFailed, state.ID.ValueString()), err).Error(),
		)
		return
	}

	state = sshkeys.MapSSHKeyResponseToModel(sshKeyResponse, state)

	diags = resp.State.Set(ctx, state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Info(ctx, sshkeys.LogSuccessfullyFinishedReadSSHKey)
}

// Update updates the resource and sets the updated Terraform state on success.
func (r *sshKeyResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	ctx = client.WithCorrelationID(ctx)
	tflog.Info(ctx, sshkeys.LogStartingUpdateSSHKey)

	var plan sshkeys.ResourceModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	var state sshkeys.ResourceModel
	diags = req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := sshkeys.UpdateSSHKey(r.client, ctx, state.ID.ValueString(), plan.Name.ValueString()); err != nil {
		resp.Diagnostics.AddError(
			sshkeys.ErrSummaryUnableToUpdateSSHKey,
			fmt.Errorf("%s: %w", fmt.Sprintf(sshkeys.ErrDetailUpdateSSHKeyFailed, state.ID.ValueString()), err).Error(),
		)
		return
	}

	sshKeyResponse, err := sshkeys.GetSSHKey(r.client, ctx, state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError(
			sshkeys.ErrSummaryUnableToReadSSHKey,
			fmt.Errorf("%s: %w", fmt.Sprintf(sshkeys.ErrDetailReadSSHKeyFailed, state.ID.ValueString()), err).Error(),
		)
		return
	}

	plan = sshkeys.MapSSHKeyResponseToModel(sshKeyResponse, plan)

	diags = resp.State.Set(ctx, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Info(ctx, sshkeys.LogSuccessfullyFinishedUpdateSSHKey)
}

// Delete deletes the resource and removes the Terraform state on success.
func (r *sshKeyResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	ctx = client.WithCorrelationID(ctx)
	tflog.Info(ctx, sshkeys.LogStartingDeleteSSHKey)

	var state sshkeys.ResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := sshkeys.DeleteSSHKey(r.client, ctx, state.ID.ValueString()); client.IsNotFound(err) {
		// Already deleted outside of Terraform
	} else if err != nil {
		resp.Diagnostics.AddError(
			sshkeys.ErrSummaryUnableToDeleteSSHKey,
			fmt.Errorf("%s: %w", fmt.Sprintf(sshkeys.ErrDetailDeleteSSHKeyFailed, state.ID.ValueString()), err).Error(),
		)
		return
	}

	tflog.Info(ctx, sshkeys.LogSuccessfullyFinishedDeleteSSHKey)
}

func (r *sshKeyResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

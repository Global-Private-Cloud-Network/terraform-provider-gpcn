package provider

import (
	"context"
	"fmt"

	"terraform-provider-gpcn/internal/client"
	"terraform-provider-gpcn/internal/vpcs"

	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

// Ensure the implementation satisfies the expected interfaces.
var (
	_ resource.Resource                = &vpcResource{}
	_ resource.ResourceWithConfigure   = &vpcResource{}
	_ resource.ResourceWithImportState = &vpcResource{}
)

// NewVpcResource is a helper function to simplify the provider implementation.
func NewVpcResource() resource.Resource {
	return &vpcResource{}
}

// vpcResource is the resource implementation.
type vpcResource struct {
	client *client.GpcnClient
}

// Metadata returns the resource type name.
func (r *vpcResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_vpc"
}

// Schema defines the schema for the resource.
func (r *vpcResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a VPC, the routed private network that holds subnets, security groups and public IP addresses in one datacenter. The API key needs vpc:read, vpc:create, vpc:update and vpc:delete",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "Unique identifier for the VPC in UUID format",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				Description: "Human-readable name for the VPC. Must be 1-64 characters with no leading or trailing whitespace, and unique within the datacenter",
				Required:    true,
				Validators: []validator.String{
					stringvalidator.LengthBetween(1, 64),
					vpcs.NoSurroundingWhitespaceValidator{Attribute: "name"},
				},
			},
			"datacenter_id": schema.StringAttribute{
				Description: "Unique identifier of the datacenter that holds the VPC. Changing this value requires replacing the VPC",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"cidr": schema.StringAttribute{
				Description: "Super-CIDR the VPC allocates its subnets from (e.g. 10.50.0.0/16). Must be an RFC1918 IPv4 CIDR at its network address with a prefix between /16 and /24. Changing this value requires replacing the VPC",
				Required:    true,
				Validators: []validator.String{
					vpcs.SuperCidrValidator{},
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"description": schema.StringAttribute{
				Description: "Additional information about the VPC to provide context for its purpose. The value must not start or end with whitespace",
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString(""),
				Validators: []validator.String{
					vpcs.NoSurroundingWhitespaceValidator{Attribute: "description"},
				},
			},
			"dns_nameservers": schema.ListAttribute{
				Description: "One or two IPv4 DNS servers the guests in the VPC receive. The platform chooses them when the list is omitted. The provider applies them once, at creation, because a provider discards a later change. Changing this value requires replacing the VPC",
				Optional:    true,
				Computed:    true,
				ElementType: types.StringType,
				Validators: []validator.List{
					listvalidator.SizeBetween(1, 2),
					listvalidator.UniqueValues(),
				},
				PlanModifiers: []planmodifier.List{
					// An unconfigured list plans a replacement on every apply
					// unless the state answers for it before the replace check.
					listplanmodifier.UseStateForUnknown(),
					listplanmodifier.RequiresReplace(),
				},
			},
			"acknowledge_overlap": schema.BoolAttribute{
				Description: "Set to true to create a VPC whose CIDR overlaps an existing VPC. Overlapping VPCs can never be connected to each other. The value is sent with the create request and is never read back",
				Optional:    true,
			},
			"status": schema.StringAttribute{
				Description: "Lifecycle state of the VPC: 'creating', 'active', 'failed' or 'deleting'",
				Computed:    true,
			},
			"egress_ip": schema.StringAttribute{
				Description: "Source address the VPC presents to the internet. Null until the platform reports it",
				Computed:    true,
			},
			"failure_reason": schema.StringAttribute{
				Description: "Why the VPC failed, as the platform reported it. Null while nothing has failed",
				Computed:    true,
			},
			"active_job_id": schema.StringAttribute{
				Description: "Identifier of the job that owns the VPC right now. Null while no job owns it",
				Computed:    true,
			},
			"created_time": schema.StringAttribute{
				Description: "Timestamp when the VPC was created",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"last_updated": schema.StringAttribute{
				Description: "Timestamp when the VPC was last updated",
				Computed:    true,
			},
		},
	}
}

// Configure adds the provider configured client to the resource.
func (r *vpcResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	gpcnClient, ok := req.ProviderData.(*client.GpcnClient)

	if !ok {
		resp.Diagnostics.AddError(
			vpcs.ErrSummaryUnexpectedConfigureType,
			fmt.Sprintf(vpcs.ErrDetailExpectedGpcnClient, req.ProviderData),
		)

		return
	}

	r.client = gpcnClient
}

// Create creates the resource and sets the initial Terraform state.
func (r *vpcResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	ctx = client.WithCorrelationID(ctx)
	tflog.Info(ctx, vpcs.LogStartingCreateGPCNVpc)

	var plan vpcs.ResourceModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	createVpcResponse, err := vpcs.CreateVpc(r.client, ctx, plan)
	if err != nil {
		resp.Diagnostics.Append(vpcs.CreateFailureDiagnostic(err))
		return
	}

	// The API inserts the row inside the request, so the VPC exists before the
	// job runs. State takes it now, or a poll timeout orphans a live VPC.
	plan = vpcs.MapCreatedVpcToModel(ctx, createVpcResponse, plan)
	diags = resp.State.Set(ctx, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	tflog.Info(ctx, vpcs.LogVpcCreatedBeforePolling)

	if err := vpcs.AwaitVpcJob(r.client, ctx, vpcs.ACTION_CREATE_VPC, createVpcResponse.Data.JobID); err != nil {
		resp.Diagnostics.AddError(vpcs.ErrSummaryUnableToCreateVpc, err.Error())
		return
	}
	tflog.Info(ctx, vpcs.LogCreateVpcJobCompleted)

	getVpcResponse, err := vpcs.GetVpc(r.client, ctx, plan.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError(
			vpcs.ErrSummaryUnableToGetVpc,
			fmt.Sprintf(vpcs.ErrDetailUnableToGetVpcWithID, plan.ID.ValueString())+": "+err.Error(),
		)
		return
	}

	plan = vpcs.MapVpcResponseToModel(ctx, getVpcResponse, plan)

	diags = resp.State.Set(ctx, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Info(ctx, vpcs.LogFinishedCreateGPCNVpc)
}

// Read refreshes the Terraform state with the latest data.
func (r *vpcResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	ctx = client.WithCorrelationID(ctx)
	tflog.Info(ctx, vpcs.LogStartingReadGPCNVpc)

	var state vpcs.ResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	getVpcResponse, err := vpcs.GetVpc(r.client, ctx, state.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			tflog.Info(ctx, vpcs.LogVpcNotFoundRemovingFromState)
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError(
			vpcs.ErrSummaryUnableToGetVpc,
			fmt.Sprintf(vpcs.ErrDetailUnableToGetVpcWithID, state.ID.ValueString())+": "+err.Error(),
		)
		return
	}

	state = vpcs.MapVpcResponseToModel(ctx, getVpcResponse, state)
	state = vpcs.RefreshVpcModelFromResponse(getVpcResponse, state)

	// A failed VPC still exists and still bills. Destroy is its only exit, so
	// the reader has to be told rather than left to read the state.
	if state.Status.ValueString() == vpcs.VPC_STATUS_FAILED {
		resp.Diagnostics.Append(vpcs.FailedVpcWarning(state.ID.ValueString(), state.FailureReason.ValueString()))
	}

	diags = resp.State.Set(ctx, state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	tflog.Info(ctx, vpcs.LogFinishedReadGPCNVpc)
}

// Update updates the resource and sets the updated Terraform state on success.
func (r *vpcResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	ctx = client.WithCorrelationID(ctx)
	tflog.Info(ctx, vpcs.LogStartingUpdateGPCNVpc)

	var plan vpcs.ResourceModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	var state vpcs.ResourceModel
	diags = req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	getVpcResponse, err := vpcs.UpdateVpc(r.client, ctx, plan.ID.ValueString(), plan, state)
	if err != nil {
		resp.Diagnostics.AddError(
			vpcs.ErrSummaryUnableToUpdateVpc,
			fmt.Sprintf(vpcs.ErrDetailUnableToUpdateVpcWithID, plan.ID.ValueString())+": "+err.Error(),
		)
		return
	}

	plan = vpcs.MapVpcResponseToModel(ctx, getVpcResponse, plan)

	diags = resp.State.Set(ctx, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Info(ctx, vpcs.LogFinishedUpdateGPCNVpc)
}

// Delete deletes the resource and removes the Terraform state on success.
func (r *vpcResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	ctx = client.WithCorrelationID(ctx)
	tflog.Info(ctx, vpcs.LogStartingDeleteGPCNVpc)

	var state vpcs.ResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := vpcs.DeleteVpc(r.client, ctx, state.ID.ValueString())
	if client.IsNotFound(err) {
		tflog.Info(ctx, vpcs.LogVpcAlreadyDeleted)
	} else if err != nil {
		resp.Diagnostics.Append(vpcs.DeleteFailureDiagnostic(state.ID.ValueString(), err))
		return
	}

	tflog.Info(ctx, vpcs.LogFinishedDeleteGPCNVpc)
}

func (r *vpcResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

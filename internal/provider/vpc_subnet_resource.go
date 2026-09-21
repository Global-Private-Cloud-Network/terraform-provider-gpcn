package provider

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"terraform-provider-gpcn/internal/client"
	"terraform-provider-gpcn/internal/helpers"
	"terraform-provider-gpcn/internal/vpcsubnets"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

// Ensure the implementation satisfies the expected interfaces.
var (
	_ resource.Resource                = &vpcSubnetResource{}
	_ resource.ResourceWithConfigure   = &vpcSubnetResource{}
	_ resource.ResourceWithImportState = &vpcSubnetResource{}
)

// NewVPCSubnetResource is a helper function to simplify the provider implementation.
func NewVPCSubnetResource() resource.Resource {
	return &vpcSubnetResource{}
}

// vpcSubnetResource is the resource implementation.
type vpcSubnetResource struct {
	client *client.GpcnClient
}

// Metadata returns the resource type name.
func (r *vpcSubnetResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_vpc_subnet"
}

// Schema defines the schema for the resource.
func (r *vpcSubnetResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a subnet inside a GPCN VPC. A subnet carves a block out of the VPC super-CIDR and binds it to a network security group. The API key needs vpc:read to read a subnet, and vpc-subnet:create, vpc-subnet:update and vpc-subnet:delete to manage one.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "Unique identifier for the subnet in UUID format",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"vpc_id": schema.StringAttribute{
				Description: "Unique identifier of the VPC the subnet is carved from. Changing this value requires replacing the subnet",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"name": schema.StringAttribute{
				Description: "Human-readable name for the subnet. It must be unique within the VPC",
				Required:    true,
				Validators: []validator.String{
					helpers.NoOuterWhitespaceValidator{Summary: vpcsubnets.ErrSummaryInvalidSubnetAttribute, Attribute: "name"},
				},
			},
			"description": schema.StringAttribute{
				Description: "Additional information about the subnet to provide context for its purpose",
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString(""),
				Validators: []validator.String{
					helpers.NoOuterWhitespaceValidator{Summary: vpcsubnets.ErrSummaryInvalidSubnetAttribute, Attribute: "description"},
				},
			},
			"cidr": schema.StringAttribute{
				Description: "CIDR block for the subnet, which must lie inside the VPC super-CIDR (e.g., 10.50.1.0/24). If omitted, GPCN carves a free block of the requested prefix length. Changing this value requires replacing the subnet",
				Optional:    true,
				Computed:    true,
				Validators: []validator.String{
					stringvalidator.ConflictsWith(path.MatchRoot("prefix")),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
					stringplanmodifier.RequiresReplace(),
				},
			},
			"prefix": schema.Int64Attribute{
				Description: "Prefix length of the block to carve when cidr is omitted, between 22 and 28. GPCN defaults to 24. The API never reports it, so the provider reads it from the CIDR's mask length. Changing this value requires replacing the subnet",
				Optional:    true,
				Computed:    true,
				Validators: []validator.Int64{
					int64validator.Between(vpcsubnets.MinPrefix, vpcsubnets.MaxPrefix),
				},
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
					int64planmodifier.RequiresReplace(),
				},
			},
			"nsg_id": schema.StringAttribute{
				Description: "Unique identifier of the network security group the subnet is bound to. If omitted, GPCN binds the VPC's default group. Changing this value rebinds the subnet in place",
				Optional:    true,
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"nsg_name": schema.StringAttribute{
				Description: "Name of the network security group the subnet is bound to",
				Computed:    true,
			},
			"state": schema.StringAttribute{
				Description: "Lifecycle state of the subnet: one of 'creating', 'ready', 'failed' or 'removing'",
				Computed:    true,
			},
			"attached_nic_count": schema.Int64Attribute{
				Description: "The number of live network interfaces attached to this subnet. GPCN refuses to delete a subnet while this count is above zero",
				Computed:    true,
			},
			"failure_reason": schema.StringAttribute{
				Description: "Why the last operation on the subnet failed, or null when none has",
				Computed:    true,
			},
			"created_time": schema.StringAttribute{
				Description: "Timestamp when the subnet was created",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"last_updated": schema.StringAttribute{
				Description: "Timestamp when the subnet was last updated",
				Computed:    true,
			},
		},
	}
}

// Configure adds the provider configured client to the resource.
func (r *vpcSubnetResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	gpcnClient, ok := req.ProviderData.(*client.GpcnClient)
	if !ok {
		resp.Diagnostics.AddError(
			vpcsubnets.ErrSummaryUnexpectedConfigureType,
			fmt.Sprintf(vpcsubnets.ErrDetailExpectedGpcnClient, req.ProviderData),
		)
		return
	}

	r.client = gpcnClient
}

// Create creates the resource and sets the initial Terraform state.
func (r *vpcSubnetResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	ctx = client.WithCorrelationID(ctx)
	tflog.Info(ctx, vpcsubnets.LogStartingCreateGPCNSubnet)

	var plan vpcsubnets.ResourceModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	subnet, err := vpcsubnets.CreateSubnet(r.client, ctx, plan)
	if err != nil {
		resp.Diagnostics.AddError(vpcsubnets.ErrSummaryUnableToCreateSubnet, err.Error())
		return
	}

	plan = vpcsubnets.MapSubnetResponseToModel(subnet, plan)

	diags = resp.State.Set(ctx, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Info(ctx, vpcsubnets.LogSuccessfullyFinishedCreateGPCNSubnet)
}

// Read refreshes the Terraform state with the latest data.
func (r *vpcSubnetResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	ctx = client.WithCorrelationID(ctx)
	tflog.Info(ctx, vpcsubnets.LogStartingReadGPCNSubnet)

	var state vpcsubnets.ResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	subnet, err := vpcsubnets.GetSubnet(r.client, ctx, state.VpcID.ValueString(), state.ID.ValueString())
	if err != nil {
		// A deleted VPC answers 404 for its whole tree. Every subnet under it
		// is then absent, like a row missing from the listing.
		if errors.Is(err, vpcsubnets.ErrSubnetAbsent) || client.IsNotFound(err) {
			tflog.Info(ctx, vpcsubnets.LogSubnetNotFoundRemovingState)
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError(
			vpcsubnets.ErrSummaryUnableToGetSubnet,
			fmt.Sprintf(vpcsubnets.ErrDetailUnableToGetSubnetWithID, state.ID.ValueString())+": "+err.Error(),
		)
		return
	}

	state = vpcsubnets.MapSubnetResponseToModel(subnet, state)
	state = vpcsubnets.RefreshSubnetModelFromResponse(subnet, state)
	resp.Diagnostics.Append(vpcsubnets.SubnetFailedWarning(subnet)...)

	diags = resp.State.Set(ctx, state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Info(ctx, vpcsubnets.LogSuccessfullyFinishedReadGPCNSubnet)
}

// Update updates the resource and sets the updated Terraform state on success.
func (r *vpcSubnetResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	ctx = client.WithCorrelationID(ctx)
	tflog.Info(ctx, vpcsubnets.LogStartingUpdateGPCNSubnet)

	var plan vpcsubnets.ResourceModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	var state vpcsubnets.ResourceModel
	diags = req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	vpcID := plan.VpcID.ValueString()
	subnetID := state.ID.ValueString()

	// The relabel runs first. A rebind that fails then leaves the new name
	// applied, rather than a rebind applied under the old name.
	if !plan.Name.Equal(state.Name) || !plan.Description.Equal(state.Description) {
		if _, err := vpcsubnets.UpdateSubnet(r.client, ctx, vpcID, subnetID, plan.Name.ValueString(), plan.Description.ValueString()); err != nil {
			resp.Diagnostics.AddError(
				vpcsubnets.ErrSummaryUnableToUpdateSubnet,
				fmt.Sprintf(vpcsubnets.ErrDetailUnableToUpdateSubnetWithID, subnetID)+": "+err.Error(),
			)
			return
		}
	}

	if !plan.NsgID.Equal(state.NsgID) && !plan.NsgID.IsUnknown() && !plan.NsgID.IsNull() {
		if err := vpcsubnets.RebindSubnetNsg(r.client, ctx, vpcID, subnetID, plan.NsgID.ValueString()); err != nil {
			resp.Diagnostics.AddError(
				vpcsubnets.ErrSummaryUnableToUpdateSubnet,
				fmt.Sprintf(vpcsubnets.ErrDetailUnableToUpdateSubnetWithID, subnetID)+": "+err.Error(),
			)
			return
		}
	}

	subnet, err := vpcsubnets.GetSubnet(r.client, ctx, vpcID, subnetID)
	if err != nil {
		resp.Diagnostics.AddError(
			vpcsubnets.ErrSummaryUnableToGetSubnet,
			fmt.Sprintf(vpcsubnets.ErrDetailUnableToGetSubnetWithID, subnetID)+": "+err.Error(),
		)
		return
	}

	plan = vpcsubnets.MapSubnetResponseToModel(subnet, plan)

	diags = resp.State.Set(ctx, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Info(ctx, vpcsubnets.LogSuccessfullyFinishedUpdateGPCNSubnet)
}

// Delete deletes the resource and removes the Terraform state on success.
func (r *vpcSubnetResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	ctx = client.WithCorrelationID(ctx)
	tflog.Info(ctx, vpcsubnets.LogStartingDeleteGPCNSubnet)

	var state vpcsubnets.ResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := vpcsubnets.DeleteSubnet(r.client, ctx, state.VpcID.ValueString(), state.ID.ValueString())
	if client.IsNotFound(err) {
		tflog.Info(ctx, vpcsubnets.LogSubnetAlreadyDeleted)
	} else if err != nil {
		resp.Diagnostics.AddError(
			vpcsubnets.ErrSummaryUnableToDeleteSubnet,
			fmt.Sprintf(vpcsubnets.ErrDetailUnableToDeleteSubnetWithID, state.ID.ValueString())+": "+err.Error(),
		)
		return
	}

	tflog.Info(ctx, vpcsubnets.LogSuccessfullyFinishedDeleteGPCNSubnet)
}

// ImportState reads the composite ID the listing needs. A subnet projection
// carries no VPC ID, so the subnet ID alone cannot find the row to read.
func (r *vpcSubnetResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts := strings.Split(req.ID, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		resp.Diagnostics.AddError(
			vpcsubnets.ErrSummaryInvalidImportID,
			fmt.Sprintf(vpcsubnets.ErrDetailImportIDFormat, req.ID),
		)
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("vpc_id"), parts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), parts[1])...)
}

package provider

import (
	"context"
	"errors"
	"fmt"

	"terraform-provider-gpcn/internal/client"
	"terraform-provider-gpcn/internal/vpcvpngateways"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

// Ensure the implementation satisfies the expected interfaces.
var (
	_ resource.Resource                = &vpcVpnGatewayResource{}
	_ resource.ResourceWithConfigure   = &vpcVpnGatewayResource{}
	_ resource.ResourceWithImportState = &vpcVpnGatewayResource{}
	_ resource.ResourceWithModifyPlan  = &vpcVpnGatewayResource{}
)

// NewVPCVPNGatewayResource is a helper function to simplify the provider implementation.
func NewVPCVPNGatewayResource() resource.Resource {
	return &vpcVpnGatewayResource{}
}

// vpcVpnGatewayResource is the resource implementation.
type vpcVpnGatewayResource struct {
	client *client.GpcnClient
}

// Metadata returns the resource type name.
func (r *vpcVpnGatewayResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_vpc_vpn_gateway"
}

// Schema defines the schema for the resource.
func (r *vpcVpnGatewayResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages the VPN gateway of a GPCN™ VPC. A VPC has at most one VPN gateway. It is the public endpoint your peer gateway connects to for site-to-site VPN. Your organization needs the VPN feature, and the data center of the VPC must offer site-to-site VPN. The API key needs vpc:read to read the VPN gateway, and vpc-vpn-connection:create and vpc-vpn-connection:delete to manage it. A VPN gateway has no attribute that changes in place. A gateway that fails to build is replaced on the next apply.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "Unique identifier for the VPN gateway in UUID format",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"vpc_id": schema.StringAttribute{
				Description: "Unique identifier of the VPC the VPN gateway belongs to. A VPC has one VPN gateway. Changing this value requires replacing the VPN gateway",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"kind": schema.StringAttribute{
				Description: "How the data center provides the VPN gateway: 'router' or 'appliance'. The data center decides, and you cannot choose",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"address": schema.StringAttribute{
				Description: "Public IPv4 address of the VPN gateway. Give it to the administrator of your peer gateway, which connects to it",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"reserved_block": schema.StringAttribute{
				Description: "The /28 block GPCN™ reserves inside the VPC CIDR for an appliance VPN gateway. It is null when the kind is 'router'",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"state": schema.StringAttribute{
				Description: "Lifecycle state of the VPN gateway: one of 'creating', 'ready', 'failed' or 'removing'",
				Computed:    true,
			},
			"failure_reason": schema.StringAttribute{
				Description: "Why the VPN gateway failed to build or to be removed, or null when neither happened",
				Computed:    true,
			},
		},
	}
}

// Configure adds the provider configured client to the resource.
func (r *vpcVpnGatewayResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	gpcnClient, ok := req.ProviderData.(*client.GpcnClient)
	if !ok {
		resp.Diagnostics.AddError(
			vpcvpngateways.ErrSummaryUnexpectedConfigureType,
			fmt.Sprintf(vpcvpngateways.ErrDetailExpectedGpcnClient, req.ProviderData),
		)
		return
	}

	r.client = gpcnClient
}

// ModifyPlan replaces a gateway that failed to build. The API has no retry for
// it: a failed gateway can only be deleted and created again.
func (r *vpcVpnGatewayResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.State.Raw.IsNull() || req.Plan.Raw.IsNull() {
		return
	}

	var state vpcvpngateways.ResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if state.State.ValueString() != vpcvpngateways.StateFailed {
		return
	}

	tflog.Info(ctx, vpcvpngateways.LogFailedGatewayPlansReplacement)
	// Terraform ignores a replace path whose value does not change. The
	// configuration of a failed gateway has not changed, so state is the path:
	// the plan makes it unknown.
	resp.RequiresReplace = append(resp.RequiresReplace, path.Root("state"))
	resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("state"), types.StringUnknown())...)
}

// Create creates the resource and sets the initial Terraform state.
func (r *vpcVpnGatewayResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	ctx = client.WithCorrelationID(ctx)
	tflog.Info(ctx, vpcvpngateways.LogStartingCreateGPCNVpnGateway)

	var plan vpcvpngateways.ResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	vpcID := plan.VpcID.ValueString()
	if err := vpcvpngateways.CreateVpnGateway(r.client, ctx, vpcID); err != nil {
		resp.Diagnostics.AddError(vpcvpngateways.ErrSummaryUnableToCreateVpnGW, vpcvpngateways.ErrorDetail(err))
		return
	}

	gateway, err := vpcvpngateways.GetVpnGateway(r.client, ctx, vpcID)
	if err != nil {
		resp.Diagnostics.AddError(vpcvpngateways.ErrSummaryUnableToCreateVpnGW, vpcvpngateways.ErrorDetail(err))
		return
	}

	plan = vpcvpngateways.MapGatewayToModel(gateway, plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// A gateway that built badly is in state, so Terraform marks it tainted
	// and the next apply replaces it.
	if gateway.State == vpcvpngateways.StateFailed {
		detail := vpcvpngateways.ErrDetailCreateFailedNoReason
		if gateway.FailureReason != nil && *gateway.FailureReason != "" {
			detail = *gateway.FailureReason
		}
		resp.Diagnostics.AddError(vpcvpngateways.ErrSummaryUnableToCreateVpnGW, detail)
		return
	}

	tflog.Info(ctx, vpcvpngateways.LogSuccessfullyFinishedCreateGPCNVpnGateway)
}

// Read refreshes the Terraform state with the latest data.
func (r *vpcVpnGatewayResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	ctx = client.WithCorrelationID(ctx)
	tflog.Info(ctx, vpcvpngateways.LogStartingReadGPCNVpnGateway)

	var state vpcvpngateways.ResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	gateway, err := vpcvpngateways.GetVpnGateway(r.client, ctx, state.VpcID.ValueString())
	if err != nil {
		// A deleted VPC answers 404 for its whole tree, like a missing gateway.
		if errors.Is(err, vpcvpngateways.ErrVpnGatewayNotFound) {
			tflog.Info(ctx, vpcvpngateways.LogVpnGatewayNotFoundRemoving)
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError(vpcvpngateways.ErrSummaryUnableToGetVpnGW, vpcvpngateways.ErrorDetail(err))
		return
	}

	state = vpcvpngateways.MapGatewayToModel(gateway, state)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Info(ctx, vpcvpngateways.LogSuccessfullyFinishedReadGPCNVpnGateway)
}

// Update copies the plan to state. No attribute updates in place, because
// vpc_id replaces, so Terraform does not call it with a real change.
func (r *vpcVpnGatewayResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	ctx = client.WithCorrelationID(ctx)
	tflog.Info(ctx, vpcvpngateways.LogStartingUpdateGPCNVpnGateway)

	var plan vpcvpngateways.ResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Info(ctx, vpcvpngateways.LogSuccessfullyFinishedUpdateGPCNVpnGateway)
}

// Delete deletes the resource and removes the Terraform state on success.
func (r *vpcVpnGatewayResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	ctx = client.WithCorrelationID(ctx)
	tflog.Info(ctx, vpcvpngateways.LogStartingDeleteGPCNVpnGateway)

	var state vpcvpngateways.ResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	vpcID := state.VpcID.ValueString()
	err := vpcvpngateways.DeleteVpnGateway(r.client, ctx, vpcID)
	if client.IsNotFound(err) {
		tflog.Info(ctx, vpcvpngateways.LogVpnGatewayAlreadyDeleted)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError(vpcvpngateways.ErrSummaryUnableToDeleteVpnGW, vpcvpngateways.ErrorDetail(err))
		return
	}

	// The job ending does not mean the row is gone. Wait for the 404.
	if err := vpcvpngateways.WaitForVpnGatewayGone(r.client, ctx, vpcID); err != nil {
		resp.Diagnostics.AddError(vpcvpngateways.ErrSummaryUnableToDeleteVpnGW, vpcvpngateways.ErrorDetail(err))
		return
	}

	tflog.Info(ctx, vpcvpngateways.LogSuccessfullyFinishedDeleteGPCNVpnGateway)
}

// ImportState takes the ID of the VPC. A VPC has one VPN gateway, and the
// gateway route is keyed by the VPC. Read fills the rest.
func (r *vpcVpnGatewayResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if req.ID == "" {
		resp.Diagnostics.AddError(
			vpcvpngateways.ErrSummaryInvalidImportID,
			fmt.Sprintf(vpcvpngateways.ErrDetailImportIDFormat, req.ID),
		)
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("vpc_id"), types.StringValue(req.ID))...)
}

package provider

import (
	"context"
	"errors"
	"fmt"
	"time"

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

var (
	_ resource.Resource                = &vpcVpnGatewayResource{}
	_ resource.ResourceWithConfigure   = &vpcVpnGatewayResource{}
	_ resource.ResourceWithImportState = &vpcVpnGatewayResource{}
	_ resource.ResourceWithModifyPlan  = &vpcVpnGatewayResource{}
)

func NewVPCVPNGatewayResource() resource.Resource {
	return &vpcVpnGatewayResource{}
}

type vpcVpnGatewayResource struct {
	client *client.GpcnClient
}

func (r *vpcVpnGatewayResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_vpc_vpn_gateway"
}

func (r *vpcVpnGatewayResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages the VPN gateway of a GPCN™ VPC. A VPC has at most one VPN gateway. It holds the VPN gateway address, the public IPv4 address your peer gateway connects to for site-to-site VPN. Your organization needs the Site-to-Site VPN feature, and the data center of the VPC must offer site-to-site VPN. The API key needs vpc:read to read the VPN gateway, and vpc-vpn-connection:create and vpc-vpn-connection:delete to manage it. A VPN gateway has no attribute that changes in place. A gateway that fails to build is replaced on the next apply.",
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
				Description: "How the data center provides the VPN gateway: 'router' or 'appliance'. The data center decides, and you can't choose",
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
				Description: "The /28 block GPCN™ reserves inside the VPC CIDR for an appliance VPN gateway. It's null when the kind is 'router'",
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

// ModifyPlan replaces a failed gateway. The API has no retry for it.
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
	// Terraform ignores a replace path whose value does not change. The plan
	// makes state unknown, so the path changes.
	resp.RequiresReplace = append(resp.RequiresReplace, path.Root("state"))
	resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("state"), types.StringUnknown())...)
}

func (r *vpcVpnGatewayResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	ctx = client.WithCorrelationID(ctx)
	tflog.Info(ctx, vpcvpngateways.LogStartingCreateGPCNVpnGateway)

	var plan vpcvpngateways.ResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	vpcID := plan.VpcID.ValueString()
	requestStart := time.Now()
	rowMayExist, accepted, createErr := vpcvpngateways.CreateVpnGateway(r.client, ctx, vpcID)
	if createErr != nil && !rowMayExist {
		detail := vpcvpngateways.ErrorDetail(createErr)
		if code, _ := vpcvpngateways.ParseAPIError(createErr); code == vpcvpngateways.CodeGatewayExists {
			detail += "\n\n" + fmt.Sprintf(vpcvpngateways.ErrFmtNotOwnRow, vpcID)
		}
		resp.Diagnostics.AddError(vpcvpngateways.ErrSummaryUnableToCreateVpnGW, detail)
		return
	}

	// After the POST the API can hold a row whatever happened next. Terraform
	// taints the resource when state is saved with an error.
	var gateway *vpcvpngateways.ApiVpnGateway
	var getErr error
	if createErr != nil {
		gateway, getErr = vpcvpngateways.ReadAfterFailure(r.client, ctx, vpcID)
		if getErr == nil && !accepted && !vpcvpngateways.CreatedSince(gateway.CreatedAt, requestStart) {
			tflog.Info(ctx, vpcvpngateways.LogRowNotCreatedByThisRequest)
			resp.Diagnostics.AddError(vpcvpngateways.ErrSummaryUnableToCreateVpnGW, fmt.Sprintf(vpcvpngateways.ErrFmtNotOwnRow, vpcID))
			return
		}
		if getErr == nil && gateway.ActiveJobID != nil {
			vpcvpngateways.PollAdoptedJob(r.client, ctx, vpcvpngateways.ActionCreateVpnGateway, gateway.ActiveJobID, requestStart)
			if settled, settledErr := vpcvpngateways.ReadAfterFailure(r.client, ctx, vpcID); settledErr == nil {
				gateway = settled
			}
		}
	} else {
		gateway, getErr = vpcvpngateways.GetVpnGateway(r.client, ctx, vpcID)
	}
	if getErr == nil {
		resp.Diagnostics.Append(resp.State.Set(ctx, vpcvpngateways.MapGatewayToModel(gateway, plan))...)
		if resp.Diagnostics.HasError() {
			return
		}
	}

	switch {
	case createErr != nil:
		resp.Diagnostics.AddError(vpcvpngateways.ErrSummaryUnableToCreateVpnGW, createFailureDetail(gateway, getErr, createErr))
		return
	case getErr != nil:
		resp.Diagnostics.AddError(vpcvpngateways.ErrSummaryUnableToCreateVpnGW, vpcvpngateways.ErrorDetail(getErr))
		return
	case gateway.State == vpcvpngateways.StateFailed:
		resp.Diagnostics.AddError(vpcvpngateways.ErrSummaryUnableToCreateVpnGW, createFailureDetail(gateway, nil, nil))
		return
	}

	tflog.Info(ctx, vpcvpngateways.LogSuccessfullyFinishedCreateGPCNVpnGateway)
}

// createFailureDetail prefers the failure reason of the row. The error text
// follows after a blank line when it differs.
func createFailureDetail(gateway *vpcvpngateways.ApiVpnGateway, getErr, createErr error) string {
	reason := ""
	if getErr == nil && gateway != nil && gateway.FailureReason != nil {
		reason = *gateway.FailureReason
	}
	switch {
	case reason == "" && createErr == nil:
		return vpcvpngateways.ErrDetailCreateFailedNoReason
	case reason == "":
		return vpcvpngateways.ErrorDetail(createErr)
	case createErr == nil || vpcvpngateways.ErrorDetail(createErr) == reason:
		return reason
	}
	return reason + "\n\n" + vpcvpngateways.ErrorDetail(createErr)
}

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

// Update copies the plan to state. Terraform never calls it with a real change.
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

	tflog.Info(ctx, vpcvpngateways.LogSuccessfullyFinishedDeleteGPCNVpnGateway)
}

// ImportState takes the ID of the VPC, because the gateway route is keyed by it.
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

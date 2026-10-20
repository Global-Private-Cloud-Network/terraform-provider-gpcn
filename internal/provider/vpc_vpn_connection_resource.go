package provider

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"terraform-provider-gpcn/internal/client"
	"terraform-provider-gpcn/internal/helpers"
	"terraform-provider-gpcn/internal/vpcvpnconnections"
	"terraform-provider-gpcn/internal/vpcvpngateways"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/setvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

var (
	_ resource.Resource                = &vpcVpnConnectionResource{}
	_ resource.ResourceWithConfigure   = &vpcVpnConnectionResource{}
	_ resource.ResourceWithImportState = &vpcVpnConnectionResource{}
	_ resource.ResourceWithModifyPlan  = &vpcVpnConnectionResource{}
)

func NewVPCVPNConnectionResource() resource.Resource {
	return &vpcVpnConnectionResource{}
}

type vpcVpnConnectionResource struct {
	client *client.GpcnClient
}

func (r *vpcVpnConnectionResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_vpc_vpn_connection"
}

func (r *vpcVpnConnectionResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a site-to-site VPN connection between a GPCN™ VPC and a peer gateway. The VPN connection runs on the VPN gateway of the VPC, so create the VPN gateway first. Your organization needs the Site-to-Site VPN feature, and the data center of the VPC must offer site-to-site VPN. The API key needs vpc:read to read a VPN connection, vpc-vpn-connection:create, vpc-vpn-connection:update and vpc-vpn-connection:delete to manage one, and vpc-nsg:update to create or change one, because the VPN connection adds rules to the security groups of the VPC. GPCN™ never returns the pre-shared key, so Terraform can't detect a change made outside Terraform. Set the key with psk, or with psk_wo and psk_wo_version (Terraform 1.11 or later). Every attribute changes in place except vpc_id and vpn_gateway_id.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "Unique identifier for the VPN connection in UUID format",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"vpc_id": schema.StringAttribute{
				Description: "Unique identifier of the VPC the VPN connection belongs to. Changing this value requires replacing the VPN connection",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"vpn_gateway_id": schema.StringAttribute{
				Description: "Unique identifier of the VPN gateway of the VPC. It must be the VPN gateway of vpc_id. Referencing the VPN gateway resource makes Terraform create the VPN gateway first and delete it last. Changing this value requires replacing the VPN connection",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"name": schema.StringAttribute{
				Description: "Name of the VPN connection, 1 to 63 characters: letters, digits, spaces, dots and hyphens, starting and ending with a letter or digit. It must be unique within the VPC",
				Required:    true,
				Validators: []validator.String{
					helpers.NoOuterWhitespaceValidator{Summary: vpcvpnconnections.ErrSummaryInvalidAttribute, Attribute: "name"},
					stringvalidator.LengthBetween(vpcvpnconnections.NameMinLength, vpcvpnconnections.NameMaxLength),
					stringvalidator.RegexMatches(vpcvpnconnections.NamePattern, "name must contain only letters, digits, spaces, dots and hyphens, and start and end with a letter or digit"),
				},
			},
			"description": schema.StringAttribute{
				Description: "Additional information about the VPN connection, up to 500 characters",
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString(""),
				Validators: []validator.String{
					helpers.NoOuterWhitespaceValidator{Summary: vpcvpnconnections.ErrSummaryInvalidAttribute, Attribute: "description"},
					vpcvpnconnections.Utf16LengthAtMostValidator{Attribute: "description", Max: vpcvpnconnections.DescriptionMax},
				},
			},
			"peer_address": schema.StringAttribute{
				Description: "Public IPv4 address of the peer gateway, the device at your site at the other end of the tunnel. Private, shared, loopback, link-local and multicast addresses are refused",
				Required:    true,
				Validators: []validator.String{
					helpers.IPv4AddressValidator{Attribute: "peer_address", PublicOnly: true},
				},
			},
			"peer_cidrs": schema.SetAttribute{
				Description: "The peer networks: 1 to 10 IPv4 CIDR blocks reachable through the peer gateway, each with a prefix length from /8 to /32. Write each block as its network address, such as 192.168.10.0/24, because GPCN™ rewrites a block with host bits set. The blocks must not overlap each other, the VPC, or the peer networks of another VPN connection",
				ElementType: types.StringType,
				Required:    true,
				Validators: []validator.Set{
					setvalidator.SizeBetween(vpcvpnconnections.PeerCidrsMin, vpcvpnconnections.PeerCidrsMax),
					setvalidator.ValueStringsAre(helpers.IPv4CIDRValidator{Attribute: "peer_cidrs", MinPrefix: vpcvpnconnections.PeerCidrMinPrefix}),
				},
			},
			"psk": schema.StringAttribute{
				Description: "Pre-shared key, 16 to 64 printable ASCII characters with no spaces, double quotes or backslashes. Terraform stores it in state, marked sensitive. Set exactly one of psk and psk_wo. Changing psk sends the new key to GPCN™",
				Optional:    true,
				Sensitive:   true,
				Validators: []validator.String{
					vpcvpnconnections.PskValidator{Attribute: "psk"},
					stringvalidator.ExactlyOneOf(path.MatchRoot("psk"), path.MatchRoot("psk_wo")),
				},
			},
			"psk_wo": schema.StringAttribute{
				Description: "Pre-shared key, write-only: Terraform never stores it in the plan or the state. It needs Terraform 1.11 or later and psk_wo_version. Same format as psk. Set exactly one of psk and psk_wo. Terraform sends the key on create, and again when psk_wo_version changes",
				Optional:    true,
				Sensitive:   true,
				WriteOnly:   true,
				Validators: []validator.String{
					vpcvpnconnections.PskValidator{Attribute: "psk_wo"},
					stringvalidator.AlsoRequires(path.MatchRoot("psk_wo_version")),
				},
			},
			"psk_wo_version": schema.Int64Attribute{
				Description: "Version of psk_wo. Terraform can't see a change in a write-only value, so raise this number to send the current psk_wo to GPCN™ and rotate the key. It's null after an import, so the first apply sends the key again",
				Optional:    true,
				Validators: []validator.Int64{
					int64validator.AlsoRequires(path.MatchRoot("psk_wo")),
				},
			},
			"ike_version": schema.StringAttribute{
				Description: "Version of the IKE protocol: 'v1' or 'v2'. GPCN™ uses 'v2' when omitted",
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString(vpcvpnconnections.DefaultIkeVersion),
				Validators: []validator.String{
					stringvalidator.OneOf("v1", "v2"),
				},
			},
			"encryption": schema.StringAttribute{
				Description: "Encryption algorithm: 'aes128', 'aes192' or 'aes256'. GPCN™ uses 'aes256' when omitted",
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString(vpcvpnconnections.DefaultEncryption),
				Validators: []validator.String{
					stringvalidator.OneOf("aes128", "aes192", "aes256"),
				},
			},
			"integrity": schema.StringAttribute{
				Description: "Integrity algorithm: 'sha256', 'sha384' or 'sha512'. GPCN™ uses 'sha256' when omitted",
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString(vpcvpnconnections.DefaultIntegrity),
				Validators: []validator.String{
					stringvalidator.OneOf("sha256", "sha384", "sha512"),
				},
			},
			"dh_group": schema.Int64Attribute{
				Description: "Diffie-Hellman group for the IKE exchange: 2, 5 or 14. GPCN™ uses 14 when omitted",
				Optional:    true,
				Computed:    true,
				Default:     int64default.StaticInt64(vpcvpnconnections.DefaultDhGroup),
				Validators: []validator.Int64{
					int64validator.OneOf(2, 5, 14),
				},
			},
			"pfs_group": schema.Int64Attribute{
				Description: "Diffie-Hellman group for perfect forward secrecy, which is always on: 2, 5 or 14. GPCN™ uses 14 when omitted",
				Optional:    true,
				Computed:    true,
				Default:     int64default.StaticInt64(vpcvpnconnections.DefaultPfsGroup),
				Validators: []validator.Int64{
					int64validator.OneOf(2, 5, 14),
				},
			},
			"ike_lifetime_seconds": schema.Int64Attribute{
				Description: "Lifetime of the IKE security association in seconds: 60 to 86400, a multiple of 60. GPCN™ uses 28800 when omitted",
				Optional:    true,
				Computed:    true,
				Default:     int64default.StaticInt64(vpcvpnconnections.DefaultIkeLifetimeSeconds),
				Validators: []validator.Int64{
					int64validator.Between(vpcvpnconnections.LifetimeMin, vpcvpnconnections.LifetimeMax),
					vpcvpnconnections.MultipleOfValidator{Attribute: "ike_lifetime_seconds", Step: vpcvpnconnections.LifetimeStep},
				},
			},
			"ipsec_lifetime_seconds": schema.Int64Attribute{
				Description: "Lifetime of the IPsec security association in seconds: 60 to 86400, a multiple of 60. GPCN™ uses 3600 when omitted",
				Optional:    true,
				Computed:    true,
				Default:     int64default.StaticInt64(vpcvpnconnections.DefaultIpsecLifetimeSeconds),
				Validators: []validator.Int64{
					int64validator.Between(vpcvpnconnections.LifetimeMin, vpcvpnconnections.LifetimeMax),
					vpcvpnconnections.MultipleOfValidator{Attribute: "ipsec_lifetime_seconds", Step: vpcvpnconnections.LifetimeStep},
				},
			},
			"dpd": schema.BoolAttribute{
				Description: "Whether dead peer detection is on. When on, GPCN™ probes the peer gateway every 30 seconds and restarts the VPN connection after 120 seconds without an answer. GPCN™ turns it on when omitted",
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(vpcvpnconnections.DefaultDpd),
			},
			"state": schema.StringAttribute{
				Description: "Lifecycle state of the VPN connection: one of 'creating', 'connecting', 'connected', 'disconnected', 'error', 'failed' or 'removing'. It changes when GPCN™ reads the peer gateway. A VPN connection with no traffic rests in 'disconnected', which is not a fault",
				Computed:    true,
			},
			"failure_reason": schema.StringAttribute{
				Description: "Why the last operation on the VPN connection failed, or null when none has. It changes when an operation fails or succeeds",
				Computed:    true,
			},
			"desired_applied": schema.BoolAttribute{
				Description: "Whether the connection's saved settings are what the data center is running. It's false while saved settings are not yet applied, for example during an update or after one that failed",
				Computed:    true,
			},
			"gateway_kind": schema.StringAttribute{
				Description: "How the data center provides the VPN gateway: 'router' or 'appliance'",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"local_endpoint_address": schema.StringAttribute{
				Description: "The VPN gateway address: the public IPv4 address your peer gateway connects to",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"local_networks": schema.ListAttribute{
				Description: "The networks on the GPCN™ side of the VPN connection, as CIDR blocks. Configure them at your peer gateway. The list changes when the subnets of the VPC change",
				ElementType: types.StringType,
				Computed:    true,
			},
		},
	}
}

func (r *vpcVpnConnectionResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	gpcnClient, ok := req.ProviderData.(*client.GpcnClient)
	if !ok {
		resp.Diagnostics.AddError(
			vpcvpnconnections.ErrSummaryUnexpectedConfigureType,
			fmt.Sprintf(vpcvpnconnections.ErrDetailExpectedGpcnClient, req.ProviderData),
		)
		return
	}

	r.client = gpcnClient
}

// ModifyPlan covers the two cases where a refresh shows no change but the next
// apply must act. A failed connection that refuses an update is replaced. A
// connection whose saved settings do not run gets an update that sends them
// again, because the API saves the values before the job that applies them.
func (r *vpcVpnConnectionResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.State.Raw.IsNull() || req.Plan.Raw.IsNull() {
		return
	}

	var state vpcvpnconnections.ResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	refused, diags := req.Private.GetKey(ctx, vpcvpnconnections.PrivateKeyUpdateRefused)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	if string(refused) == "true" {
		tflog.Info(ctx, vpcvpnconnections.LogFailedWithoutUpdatePlansReplacement)
		// Terraform ignores a replace path whose value does not change. The plan
		// makes state unknown, so the path changes.
		resp.RequiresReplace = append(resp.RequiresReplace, path.Root("state"))
		resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("state"), types.StringUnknown())...)
		return
	}

	jobActive, diags := req.Private.GetKey(ctx, vpcvpnconnections.PrivateKeyJobActive)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	known := !state.DesiredApplied.IsNull() && !state.DesiredApplied.IsUnknown()
	if known && vpcvpnconnections.RetryUnappliedDue(state.DesiredApplied.ValueBool(), state.State.ValueString(), string(jobActive) == "true") {
		tflog.Info(ctx, vpcvpnconnections.LogUnappliedPlansUpdate)
		// The retried job settles the row, so the attributes it rewrites plan unknown.
		resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("desired_applied"), types.BoolUnknown())...)
		resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("state"), types.StringUnknown())...)
		resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("failure_reason"), types.StringUnknown())...)
		resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("local_networks"), types.ListUnknown(types.StringType))...)
	}
}

func (r *vpcVpnConnectionResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	ctx = client.WithCorrelationID(ctx)
	tflog.Info(ctx, vpcvpnconnections.LogStartingCreateGPCNVpnConnection)

	var plan vpcvpnconnections.ResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	var pskWo types.String
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("psk_wo"), &pskWo)...)
	if resp.Diagnostics.HasError() {
		return
	}

	vpcID := plan.VpcID.ValueString()
	gateway, err := vpcvpngateways.GetVpnGateway(r.client, ctx, vpcID)
	switch {
	case errors.Is(err, vpcvpngateways.ErrVpnGatewayNotFound), err == nil && gateway.ID != plan.VpnGatewayID.ValueString():
		resp.Diagnostics.AddError(vpcvpnconnections.ErrSummaryUnableToCreateVpnConn,
			fmt.Sprintf(vpcvpnconnections.ErrDetailWrongGateway, plan.VpnGatewayID.ValueString(), vpcID))
		return
	case err != nil:
		resp.Diagnostics.AddError(vpcvpnconnections.ErrSummaryUnableToCreateVpnConn, vpcvpngateways.ErrorDetail(err))
		return
	}

	body, diags := vpcvpnconnections.BuildCreateBody(ctx, plan, pskWo.ValueString())
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	requestStart := time.Now()
	connectionID, rowMayExist, createErr := vpcvpnconnections.CreateVpnConnection(r.client, ctx, vpcID, body)
	var lookupErr error
	if connectionID == "" && rowMayExist {
		// A 5xx answer or a lost answer hides the outcome of the POST. The row
		// can exist, and its name is unique within the VPC. The row counts only
		// when this request can have made it.
		var adopted *vpcvpnconnections.ApiVpnConnection
		adopted, lookupErr = vpcvpnconnections.FindAfterFailure(r.client, ctx, vpcID, plan.Name.ValueString())
		if lookupErr == nil && adopted != nil {
			if !vpcvpnconnections.MadeByRequest(adopted, plan.Name.ValueString(), plan.PeerAddress.ValueString(), requestStart) {
				tflog.Info(ctx, vpcvpnconnections.LogRowNotCreatedByThisRequest)
				resp.Diagnostics.AddError(vpcvpnconnections.ErrSummaryUnableToCreateVpnConn,
					fmt.Sprintf(vpcvpnconnections.ErrFmtNotOwnRow, adopted.Name, vpcID, adopted.ID))
				return
			}
			tflog.Info(ctx, vpcvpnconnections.LogAdoptedVpnConnectionByName)
			connectionID = adopted.ID
			vpcvpngateways.PollAdoptedJob(r.client, ctx, vpcvpnconnections.ActionCreateVpnConnection, adopted.ActiveJobID, requestStart)
		}
	}
	if connectionID == "" {
		failure := vpcvpngateways.ErrorDetail(createErr)
		if lookupErr != nil {
			failure += "\n\n" + vpcvpnconnections.ErrDetailLookupFailed + "\n\n" + vpcvpngateways.ErrorDetail(lookupErr)
		} else if rowMayExist {
			failure += "\n\n" + fmt.Sprintf(vpcvpnconnections.ErrFmtMayCommitLater, plan.Name.ValueString(), vpcID)
		}
		resp.Diagnostics.AddError(vpcvpnconnections.ErrSummaryUnableToCreateVpnConn, failure)
		return
	}

	// The API holds a row, even when its job failed. Terraform taints the
	// resource when state is saved with an error.
	var detail *vpcvpnconnections.ApiVpnConnectionDetail
	var getErr error
	if createErr != nil {
		detail, getErr = vpcvpnconnections.ReadAfterFailure(r.client, ctx, vpcID, connectionID)
	} else {
		detail, getErr = vpcvpnconnections.GetVpnConnection(r.client, ctx, vpcID, connectionID)
	}
	if getErr != nil {
		resp.Diagnostics.Append(resp.State.Set(ctx, vpcvpnconnections.ModelFromPlanWithID(plan, connectionID))...)
		failure := createErr
		if failure == nil {
			failure = getErr
		}
		resp.Diagnostics.AddError(vpcvpnconnections.ErrSummaryUnableToCreateVpnConn, vpcvpngateways.ErrorDetail(failure))
		return
	}

	r.saveCreated(ctx, resp, plan, detail)
	if resp.Diagnostics.HasError() {
		return
	}

	if createErr != nil || detail.State == vpcvpnconnections.StateFailed {
		resp.Diagnostics.AddError(vpcvpnconnections.ErrSummaryUnableToCreateVpnConn, connectionCreateFailureDetail(detail, createErr))
		return
	}

	tflog.Info(ctx, vpcvpnconnections.LogSuccessfullyFinishedCreateGPCNVpnConnection)
}

// connectionCreateFailureDetail prefers the failure reason of the row. The
// error text follows after a blank line when it differs.
func connectionCreateFailureDetail(detail *vpcvpnconnections.ApiVpnConnectionDetail, createErr error) string {
	reason := ""
	if detail.FailureReason != nil {
		reason = *detail.FailureReason
	}
	switch {
	case reason == "" && createErr == nil:
		return vpcvpnconnections.ErrDetailCreateFailedNoReason
	case reason == "":
		return vpcvpngateways.ErrorDetail(createErr)
	case createErr == nil || vpcvpngateways.ErrorDetail(createErr) == reason:
		return reason
	}
	return reason + "\n\n" + vpcvpngateways.ErrorDetail(createErr)
}

func (r *vpcVpnConnectionResource) saveCreated(ctx context.Context, resp *resource.CreateResponse, plan vpcvpnconnections.ResourceModel, detail *vpcvpnconnections.ApiVpnConnectionDetail) {
	model, diags := vpcvpnconnections.MapConnectionToModel(ctx, detail, plan, false)
	resp.Diagnostics.Append(diags...)
	if diags.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, model)...)
}

func (r *vpcVpnConnectionResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	ctx = client.WithCorrelationID(ctx)
	tflog.Info(ctx, vpcvpnconnections.LogStartingReadGPCNVpnConnection)

	var state vpcvpnconnections.ResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	vpcID := state.VpcID.ValueString()
	detail, err := vpcvpnconnections.GetVpnConnection(r.client, ctx, vpcID, state.ID.ValueString())
	if err != nil {
		if errors.Is(err, vpcvpnconnections.ErrVpnConnectionNotFound) {
			tflog.Info(ctx, vpcvpnconnections.LogVpnConnectionNotFoundRemoving)
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError(vpcvpnconnections.ErrSummaryUnableToGetVpnConn, vpcvpngateways.ErrorDetail(err))
		return
	}

	model, diags := vpcvpnconnections.MapConnectionToModel(ctx, detail, state, true)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	gateway, err := vpcvpngateways.GetVpnGateway(r.client, ctx, vpcID)
	switch {
	case err == nil:
		model.VpnGatewayID = types.StringValue(gateway.ID)
	case errors.Is(err, vpcvpngateways.ErrVpnGatewayNotFound):
		tflog.Info(ctx, vpcvpnconnections.LogVpnGatewayGoneKeepingID)
	default:
		resp.Diagnostics.AddError(vpcvpnconnections.ErrSummaryUnableToGetVpnConn, vpcvpngateways.ErrorDetail(err))
		return
	}

	updateRefused := detail.State == vpcvpnconnections.StateFailed && !slices.Contains(detail.AcceptedVerbs, vpcvpnconnections.VerbUpdate)
	jobActive := detail.ActiveJobID != nil
	if resp.Private != nil {
		resp.Diagnostics.Append(resp.Private.SetKey(ctx, vpcvpnconnections.PrivateKeyUpdateRefused, privateFlag(updateRefused))...)
		resp.Diagnostics.Append(resp.Private.SetKey(ctx, vpcvpnconnections.PrivateKeyJobActive, privateFlag(jobActive))...)
	}

	if vpcvpnconnections.RetryUnappliedDue(detail.DesiredApplied, detail.State, jobActive) {
		resp.Diagnostics.AddWarning(vpcvpnconnections.WarnSummaryDesiredNotApplied, vpcvpnconnections.WarnDetailDesiredNotApplied)
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, model)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Info(ctx, vpcvpnconnections.LogSuccessfullyFinishedReadGPCNVpnConnection)
}

// privateFlag is the private state value of a flag: "true" when set, and none
// when not, which removes the key.
func privateFlag(set bool) []byte {
	if set {
		return []byte("true")
	}
	return nil
}

func (r *vpcVpnConnectionResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	ctx = client.WithCorrelationID(ctx)
	tflog.Info(ctx, vpcvpnconnections.LogStartingUpdateGPCNVpnConnection)

	var plan, state vpcvpnconnections.ResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	var pskWo types.String
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("psk_wo"), &pskWo)...)
	if resp.Diagnostics.HasError() {
		return
	}

	vpcID := plan.VpcID.ValueString()
	connectionID := state.ID.ValueString()

	body, diags := vpcvpnconnections.BuildUpdateBody(ctx, plan, state, pskWo.ValueString(), !state.DesiredApplied.IsNull() && !state.DesiredApplied.ValueBool())
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	if len(body) == 0 {
		tflog.Info(ctx, vpcvpnconnections.LogUpdateSendsNoRequest)
	} else if err := vpcvpnconnections.UpdateVpnConnection(r.client, ctx, vpcID, connectionID, body); err != nil {
		// The state stays as it was, so the failed job does not change it.
		resp.Diagnostics.AddError(vpcvpnconnections.ErrSummaryUnableToUpdateVpnConn, vpcvpngateways.ErrorDetail(err))
		return
	}

	detail, err := vpcvpnconnections.GetVpnConnection(r.client, ctx, vpcID, connectionID)
	if err != nil {
		resp.Diagnostics.AddError(vpcvpnconnections.ErrSummaryUnableToUpdateVpnConn, vpcvpngateways.ErrorDetail(err))
		return
	}

	plan.ID = state.ID
	model, mapDiags := vpcvpnconnections.MapConnectionToModel(ctx, detail, plan, false)
	resp.Diagnostics.Append(mapDiags...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, model)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Info(ctx, vpcvpnconnections.LogSuccessfullyFinishedUpdateGPCNVpnConnection)
}

func (r *vpcVpnConnectionResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	ctx = client.WithCorrelationID(ctx)
	tflog.Info(ctx, vpcvpnconnections.LogStartingDeleteGPCNVpnConnection)

	var state vpcvpnconnections.ResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := vpcvpnconnections.DeleteVpnConnection(r.client, ctx, state.VpcID.ValueString(), state.ID.ValueString())
	if client.IsNotFound(err) {
		tflog.Info(ctx, vpcvpnconnections.LogVpnConnectionAlreadyDeleted)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError(vpcvpnconnections.ErrSummaryUnableToDeleteVpnConn, vpcvpngateways.ErrorDetail(err))
		return
	}

	tflog.Info(ctx, vpcvpnconnections.LogSuccessfullyFinishedDeleteGPCNVpnConnection)
}

// ImportState takes "<vpc_id>/<connection_id>". Read fills the rest. The key
// attributes stay null, because the API never returns the key.
func (r *vpcVpnConnectionResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts := strings.Split(req.ID, vpcvpnconnections.ImportIDSeparator)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		resp.Diagnostics.AddError(
			vpcvpnconnections.ErrSummaryInvalidImportID,
			fmt.Sprintf(vpcvpnconnections.ErrDetailImportIDFormat, req.ID),
		)
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("vpc_id"), types.StringValue(parts[0]))...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), types.StringValue(parts[1]))...)
}

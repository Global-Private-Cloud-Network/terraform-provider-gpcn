package provider

import (
	"context"
	"fmt"
	"strings"

	"terraform-provider-gpcn/internal/client"
	"terraform-provider-gpcn/internal/vpcpublicips"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

// Ensure the implementation satisfies the expected interfaces.
var (
	_ resource.Resource                = &vpcPublicIpResource{}
	_ resource.ResourceWithConfigure   = &vpcPublicIpResource{}
	_ resource.ResourceWithImportState = &vpcPublicIpResource{}
)

// NewVPCPublicIpResource is a helper function to simplify the provider implementation.
func NewVPCPublicIpResource() resource.Resource {
	return &vpcPublicIpResource{}
}

// vpcPublicIpResource is the resource implementation.
type vpcPublicIpResource struct {
	client *client.GpcnClient
}

// Metadata returns the resource type name.
func (r *vpcPublicIpResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_vpc_public_ip"
}

// Schema defines the schema for the resource.
func (r *vpcPublicIpResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Acquires an elastic public IP address from a VPC and holds it. The address stays with the VPC until it is released, so it survives the virtual machines it serves. Attach it to a network interface with gpcn_vpc_public_ip_attachment. Requires the API-key permissions vpc:read, vpc-public-ip:create and vpc-public-ip:delete.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "Unique identifier for the public IP address in UUID format",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"vpc_id": schema.StringAttribute{
				Description: "Unique identifier of the VPC the address is acquired from. Changing this value requires replacing the address",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"ip_address": schema.StringAttribute{
				Description: "The address itself. It is null until the platform allocates it, which happens shortly after the acquisition",
				Computed:    true,
			},
			"state": schema.StringAttribute{
				Description: "Lifecycle state of the address: 'acquiring', 'ready', 'failed' or 'removing'",
				Computed:    true,
			},
			"held": schema.BoolAttribute{
				Description: "Whether the address is held by the VPC rather than serving a virtual machine. It is true while virtual_machine_id is null",
				Computed:    true,
			},
			"virtual_machine_id": schema.StringAttribute{
				Description: "Unique identifier of the virtual machine the address serves, or null while the address is held",
				Computed:    true,
			},
			"failure_reason": schema.StringAttribute{
				Description: "Why the last operation on the address failed, or null when none did",
				Computed:    true,
			},
			"created_time": schema.StringAttribute{
				Description: "Timestamp when the address was acquired",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"last_updated": schema.StringAttribute{
				Description: "Timestamp when the address was last updated",
				Computed:    true,
			},
		},
	}
}

// Configure adds the provider configured client to the resource.
func (r *vpcPublicIpResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	gpcnClient, ok := req.ProviderData.(*client.GpcnClient)
	if !ok {
		resp.Diagnostics.AddError(
			vpcpublicips.ErrSummaryUnexpectedConfigureType,
			fmt.Sprintf(vpcpublicips.ErrDetailExpectedGpcnClient, req.ProviderData),
		)
		return
	}

	r.client = gpcnClient
}

// Create acquires the address and reads it back for its computed attributes.
func (r *vpcPublicIpResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	ctx = client.WithCorrelationID(ctx)
	tflog.Info(ctx, vpcpublicips.LogStartingCreateGPCNPublicIp)

	var plan vpcpublicips.ResourceModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	vpcID := plan.VpcID.ValueString()
	publicIpID, err := vpcpublicips.AcquirePublicIp(r.client, ctx, vpcID)
	if err != nil {
		if publicIpID == "" {
			resp.Diagnostics.AddError(vpcpublicips.ErrSummaryUnableToAcquirePublicIp, err.Error())
			return
		}
		// The address exists, so state must name it. Terraform taints a
		// resource whose Create reports an error beside state, and the next
		// apply then releases the address instead of orphaning it.
		resp.Diagnostics.Append(resp.State.Set(ctx, vpcpublicips.MapAcquiredIdToModel(publicIpID, plan))...)
		resp.Diagnostics.AddError(
			vpcpublicips.ErrSummaryUnableToAcquirePublicIp,
			fmt.Sprintf(vpcpublicips.ErrDetailAcquiredPublicIpJobFailed, publicIpID, err.Error()),
		)
		return
	}

	publicIp, err := vpcpublicips.GetPublicIp(r.client, ctx, vpcID, publicIpID)
	if err != nil {
		resp.Diagnostics.AddError(
			vpcpublicips.ErrSummaryUnableToGetPublicIp,
			fmt.Sprintf(vpcpublicips.ErrDetailUnableToGetPublicIpWithID, publicIpID, vpcID)+": "+err.Error(),
		)
		return
	}

	plan = vpcpublicips.MapPublicIpResponseToModel(publicIp, plan)

	diags = resp.State.Set(ctx, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Info(ctx, vpcpublicips.LogSuccessfullyFinishedCreateGPCNPublicIp)
}

// Read refreshes the Terraform state with the latest data.
func (r *vpcPublicIpResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	ctx = client.WithCorrelationID(ctx)
	tflog.Info(ctx, vpcpublicips.LogStartingReadGPCNPublicIp)

	var state vpcpublicips.ResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	vpcID := state.VpcID.ValueString()
	publicIpID := state.ID.ValueString()
	publicIp, err := vpcpublicips.GetPublicIp(r.client, ctx, vpcID, publicIpID)
	if err != nil {
		if client.IsNotFound(err) {
			tflog.Info(ctx, vpcpublicips.LogPublicIpNotFoundRemovingFromState)
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError(
			vpcpublicips.ErrSummaryUnableToGetPublicIp,
			fmt.Sprintf(vpcpublicips.ErrDetailUnableToGetPublicIpWithID, publicIpID, vpcID)+": "+err.Error(),
		)
		return
	}

	state = vpcpublicips.MapPublicIpResponseToModel(publicIp, state)

	diags = resp.State.Set(ctx, state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Info(ctx, vpcpublicips.LogSuccessfullyFinishedReadGPCNPublicIp)
}

func (r *vpcPublicIpResource) Update(_ context.Context, _ resource.UpdateRequest, _ *resource.UpdateResponse) {
	// The API has no update verb for an address, and vpc_id requires
	// replacement, so Update is never called.
}

// Delete releases the address back to the platform.
func (r *vpcPublicIpResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	ctx = client.WithCorrelationID(ctx)
	tflog.Info(ctx, vpcpublicips.LogStartingDeleteGPCNPublicIp)

	var state vpcpublicips.ResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	vpcID := state.VpcID.ValueString()
	publicIpID := state.ID.ValueString()
	if err := vpcpublicips.ReleasePublicIp(r.client, ctx, vpcID, publicIpID); err != nil {
		resp.Diagnostics.AddError(
			vpcpublicips.ErrSummaryUnableToReleasePublicIp,
			fmt.Sprintf(vpcpublicips.ErrDetailUnableToReleasePublicIpWithID, publicIpID, vpcID)+": "+err.Error(),
		)
		return
	}

	tflog.Info(ctx, vpcpublicips.LogSuccessfullyFinishedDeleteGPCNPublicIp)
}

// ImportState reads an address named by its VPC and its own id. The listing the
// read walks belongs to one VPC, so an id alone names nothing.
func (r *vpcPublicIpResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	vpcID, publicIpID, found := strings.Cut(req.ID, "/")
	if !found || vpcID == "" || publicIpID == "" {
		resp.Diagnostics.AddError(
			vpcpublicips.ErrSummaryUnexpectedImportID,
			fmt.Sprintf(vpcpublicips.ErrDetailPublicIpImportID, req.ID),
		)
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("vpc_id"), vpcID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), publicIpID)...)
}

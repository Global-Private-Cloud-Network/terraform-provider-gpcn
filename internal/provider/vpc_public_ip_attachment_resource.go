package provider

import (
	"context"
	"fmt"

	"terraform-provider-gpcn/internal/client"
	"terraform-provider-gpcn/internal/vpcpublicips"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

// Ensure the implementation satisfies the expected interfaces.
var (
	_ resource.Resource              = &vpcPublicIpAttachmentResource{}
	_ resource.ResourceWithConfigure = &vpcPublicIpAttachmentResource{}
)

// NewVPCPublicIpAttachmentResource is a helper function to simplify the provider implementation.
func NewVPCPublicIpAttachmentResource() resource.Resource {
	return &vpcPublicIpAttachmentResource{}
}

// vpcPublicIpAttachmentResource is the resource implementation.
type vpcPublicIpAttachmentResource struct {
	client *client.GpcnClient
}

// Metadata returns the resource type name.
func (r *vpcPublicIpAttachmentResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_vpc_public_ip_attachment"
}

// Schema defines the schema for the resource.
func (r *vpcPublicIpAttachmentResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Binds an elastic public IP address acquired with gpcn_vpc_public_ip to one VPC network interface. Destroying the attachment detaches the address and leaves it held by the VPC, so the address itself survives. GPCN allows one elastic address per machine. Requires the API-key permissions vpc:read and vpc-public-ip:update.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "Unique identifier for the attachment (equal to public_ip_id, because an address carries at most one attachment)",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"vpc_id": schema.StringAttribute{
				Description: "Unique identifier of the VPC that holds the address. Changing this value requires replacing the attachment",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"public_ip_id": schema.StringAttribute{
				Description: "Unique identifier of the address to attach. Changing this value requires replacing the attachment",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"nic_id": schema.StringAttribute{
				Description: "Unique identifier of the VPC network interface the address is bound to. GPCN has no verb that re-points a live address, so changing this value requires replacing the attachment, which detaches the address and attaches it again. The platform reports the machine an address serves but not the interface, so an interface changed outside Terraform is not detected",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"virtual_machine_id": schema.StringAttribute{
				Description: "Unique identifier of the virtual machine the attached interface belongs to",
				Computed:    true,
			},
		},
	}
}

// Configure adds the provider configured client to the resource.
func (r *vpcPublicIpAttachmentResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

// Create binds the address to the interface and reads back the machine it now serves.
func (r *vpcPublicIpAttachmentResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	ctx = client.WithCorrelationID(ctx)
	tflog.Info(ctx, vpcpublicips.LogStartingCreateGPCNPublicIpAttachment)

	var plan vpcpublicips.AttachmentResourceModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	vpcID := plan.VpcID.ValueString()
	publicIpID := plan.PublicIpID.ValueString()
	nicID := plan.NicID.ValueString()

	if err := vpcpublicips.AttachPublicIp(r.client, ctx, vpcID, publicIpID, nicID); err != nil {
		resp.Diagnostics.AddError(
			vpcpublicips.ErrSummaryUnableToAttachPublicIp,
			fmt.Sprintf(vpcpublicips.ErrDetailUnableToAttachPublicIpWithID, publicIpID, nicID)+": "+err.Error(),
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

	plan = vpcpublicips.MapPublicIpResponseToAttachmentModel(publicIp, plan)

	diags = resp.State.Set(ctx, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Info(ctx, vpcpublicips.LogSuccessfullyFinishedCreateGPCNPublicIpAttachment)
}

// Read refreshes the Terraform state with the latest data.
func (r *vpcPublicIpAttachmentResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	ctx = client.WithCorrelationID(ctx)
	tflog.Info(ctx, vpcpublicips.LogStartingReadGPCNPublicIpAttachment)

	var state vpcpublicips.AttachmentResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	vpcID := state.VpcID.ValueString()
	publicIpID := state.PublicIpID.ValueString()
	publicIp, err := vpcpublicips.GetPublicIp(r.client, ctx, vpcID, publicIpID)
	if err != nil {
		if client.IsNotFound(err) {
			tflog.Info(ctx, vpcpublicips.LogAttachmentNotFoundRemovingFromState)
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError(
			vpcpublicips.ErrSummaryUnableToGetPublicIp,
			fmt.Sprintf(vpcpublicips.ErrDetailUnableToGetPublicIpWithID, publicIpID, vpcID)+": "+err.Error(),
		)
		return
	}

	// The projection names the machine, not the interface. An address that
	// serves no machine therefore carries no attachment at all.
	if publicIp.VirtualMachineId == nil {
		tflog.Info(ctx, vpcpublicips.LogAttachmentNotFoundRemovingFromState)
		resp.State.RemoveResource(ctx)
		return
	}

	state = vpcpublicips.MapPublicIpResponseToAttachmentModel(publicIp, state)

	diags = resp.State.Set(ctx, state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Info(ctx, vpcpublicips.LogSuccessfullyFinishedReadGPCNPublicIpAttachment)
}

func (r *vpcPublicIpAttachmentResource) Update(_ context.Context, _ resource.UpdateRequest, _ *resource.UpdateResponse) {
	// All attributes require replacement; Update is never called.
}

// Delete detaches the address, which leaves it held by its VPC.
func (r *vpcPublicIpAttachmentResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	ctx = client.WithCorrelationID(ctx)
	tflog.Info(ctx, vpcpublicips.LogStartingDeleteGPCNPublicIpAttachment)

	var state vpcpublicips.AttachmentResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	vpcID := state.VpcID.ValueString()
	publicIpID := state.PublicIpID.ValueString()
	if err := vpcpublicips.DetachPublicIp(r.client, ctx, vpcID, publicIpID); err != nil {
		resp.Diagnostics.AddError(
			vpcpublicips.ErrSummaryUnableToDetachPublicIp,
			fmt.Sprintf(vpcpublicips.ErrDetailUnableToDetachPublicIpWithID, publicIpID)+": "+err.Error(),
		)
		return
	}

	tflog.Info(ctx, vpcpublicips.LogSuccessfullyFinishedDeleteGPCNPublicIpAttachment)
}

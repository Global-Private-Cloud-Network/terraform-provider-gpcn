package provider

import (
	"context"
	"fmt"

	"terraform-provider-gpcn/internal/client"
	"terraform-provider-gpcn/internal/volumeattachments"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

var (
	_ resource.Resource                = &volumeAttachmentResource{}
	_ resource.ResourceWithConfigure   = &volumeAttachmentResource{}
	_ resource.ResourceWithImportState = &volumeAttachmentResource{}
)

func NewVolumeAttachmentResource() resource.Resource {
	return &volumeAttachmentResource{}
}

type volumeAttachmentResource struct {
	client *client.GpcnClient
}

func (r *volumeAttachmentResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_volume_attachment"
}

func (r *volumeAttachmentResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages the attachment of a volume to a virtual machine. A volume can only be attached to one virtual machine at a time. If the virtual machine has network_hotplug disabled, the VM will be stopped and restarted around the attach/detach operation. When attaching multiple volumes to the same network_hotplug=false VM concurrently, use depends_on to serialize operations.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "Unique identifier for the attachment (equal to the volume ID).",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"virtual_machine_id": schema.StringAttribute{
				Description: "ID of the virtual machine to attach the volume to. Changing this value requires replacing the resource.",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"volume_id": schema.StringAttribute{
				Description: "ID of the volume to attach. Changing this value requires replacing the resource.",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
		},
	}
}

func (r *volumeAttachmentResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	gpcnClient, ok := req.ProviderData.(*client.GpcnClient)
	if !ok {
		resp.Diagnostics.AddError(
			volumeattachments.ErrSummaryUnexpectedConfigureType,
			fmt.Sprintf(volumeattachments.ErrDetailExpectedGpcnClient, req.ProviderData),
		)
		return
	}
	r.client = gpcnClient
}

func (r *volumeAttachmentResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	ctx = client.WithCorrelationID(ctx)
	tflog.Info(ctx, volumeattachments.LogStartingCreateVolumeAttachment)

	var plan volumeattachments.ResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	vmId := plan.VirtualMachineId.ValueString()
	volumeId := plan.VolumeId.ValueString()

	// Idempotency: check if already attached
	currentVMId, err := volumeattachments.GetAttachedVMId(r.client, ctx, volumeId)
	if err != nil {
		resp.Diagnostics.AddError(volumeattachments.ErrSummaryUnableToAttachVolume, err.Error())
		return
	}
	if currentVMId == vmId {
		tflog.Info(ctx, volumeattachments.LogVolumeAlreadyAttached)
	} else if currentVMId != "" {
		resp.Diagnostics.AddError(
			volumeattachments.ErrSummaryUnableToAttachVolume,
			fmt.Sprintf(volumeattachments.ErrDetailVolumeAlreadyAttached, volumeId, currentVMId, vmId),
		)
		return
	} else {
		if err = volumeattachments.AttachVolume(r.client, ctx, vmId, volumeId); err != nil {
			resp.Diagnostics.AddError(volumeattachments.ErrSummaryUnableToAttachVolume, err.Error())
			return
		}
	}

	plan.ID = types.StringValue(volumeId)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *volumeAttachmentResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	ctx = client.WithCorrelationID(ctx)
	tflog.Info(ctx, volumeattachments.LogStartingReadVolumeAttachment)

	var state volumeattachments.ResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	currentVMId, err := volumeattachments.GetAttachedVMId(r.client, ctx, state.VolumeId.ValueString())
	if client.IsNotFound(err) {
		// The backing volume is gone, so the attachment should be too
		tflog.Info(ctx, volumeattachments.LogVolumeAttachmentVolumeGone)
		resp.State.RemoveResource(ctx)
		return
	} else if err != nil {
		resp.Diagnostics.AddError(volumeattachments.ErrSummaryUnableToReadAttachment, err.Error())
		return
	}

	if currentVMId == "" {
		tflog.Info(ctx, volumeattachments.LogVolumeAttachmentNotFound)
		resp.State.RemoveResource(ctx)
		return
	}

	// On import, VirtualMachineId is null — populate it from the API response.
	if state.VirtualMachineId.IsNull() || state.VirtualMachineId.ValueString() == "" {
		state.VirtualMachineId = types.StringValue(currentVMId)
		state.ID = types.StringValue(state.VolumeId.ValueString())
	} else if currentVMId != state.VirtualMachineId.ValueString() {
		tflog.Info(ctx, volumeattachments.LogVolumeAttachmentNotFound)
		resp.State.RemoveResource(ctx)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *volumeAttachmentResource) Update(_ context.Context, _ resource.UpdateRequest, _ *resource.UpdateResponse) {
	// All attributes require replacement; Update is never called.
}

func (r *volumeAttachmentResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	ctx = client.WithCorrelationID(ctx)
	tflog.Info(ctx, volumeattachments.LogStartingDeleteVolumeAttachment)

	var state volumeattachments.ResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := volumeattachments.DetachVolume(r.client, ctx, state.VirtualMachineId.ValueString(), state.VolumeId.ValueString()); client.IsNotFound(err) {
		// The VM or volume is already deleted
		tflog.Info(ctx, volumeattachments.LogVolumeAttachmentAlreadyDetached)
	} else if err != nil {
		resp.Diagnostics.AddError(volumeattachments.ErrSummaryUnableToDetachVolume, err.Error())
		return
	}
}

func (r *volumeAttachmentResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	// Import by volume ID — Read derives virtual_machine_id from the volume GET response.
	resource.ImportStatePassthroughID(ctx, path.Root("volume_id"), req, resp)
}

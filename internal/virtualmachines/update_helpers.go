package virtualmachines

import (
	"context"
	"fmt"
	"slices"

	"terraform-provider-gpcn/internal/client"
	"terraform-provider-gpcn/internal/networks"
	"terraform-provider-gpcn/internal/vpcpublicips"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

// readSegmentIds reads a segment list that a null or unknown value leaves empty.
func readSegmentIds(ctx context.Context, list types.List) ([]string, diag.Diagnostics) {
	var diags diag.Diagnostics
	segmentIds := []string{}
	if list.IsNull() || list.IsUnknown() {
		return segmentIds, diags
	}
	diags.Append(list.ElementsAs(ctx, &segmentIds, true)...)
	return segmentIds, diags
}

// UpdateL2SegmentsIfChanged makes the machine carry the configured segments. It detaches
// first, because GPCN caps a machine at five interfaces and a swap would otherwise need
// a free slot it does not have. The birth subnet interface is the primary one and is
// never a candidate: GPCN refuses to detach a primary interface.
// Returns diagnostics if any errors occurred.
func UpdateL2SegmentsIfChanged(gpcnClient *client.GpcnClient, ctx context.Context, vmID string, state, plan ResourceModel) diag.Diagnostics {
	var diags diag.Diagnostics

	if plan.L2SegmentIds.Equal(state.L2SegmentIds) {
		return diags
	}

	planned, plannedDiags := readSegmentIds(ctx, plan.L2SegmentIds)
	diags.Append(plannedDiags...)
	current, currentDiags := readSegmentIds(ctx, state.L2SegmentIds)
	diags.Append(currentDiags...)
	if diags.HasError() {
		return diags
	}

	var removed, added []string
	for _, segmentId := range current {
		if !slices.Contains(planned, segmentId) {
			removed = append(removed, segmentId)
		}
	}
	for _, segmentId := range planned {
		if !slices.Contains(current, segmentId) {
			added = append(added, segmentId)
		}
	}
	if len(removed) == 0 && len(added) == 0 {
		return diags
	}

	networkInterfaces, err := networks.GetNetworkInterfaces(gpcnClient, ctx, vmID)
	if err != nil {
		diags.AddError(
			ErrSummaryErrorRetrievingNetworkIfaces,
			err.Error(),
		)
		return diags
	}

	for _, segmentId := range removed {
		interfaceIdx := slices.IndexFunc(networkInterfaces, func(inter networks.ReadVirtualMachineNetworkDataResponseTF) bool {
			return !inter.IsPrimary.ValueBool() &&
				inter.World.ValueString() == networks.NicWorldL2 &&
				inter.L2SegmentID.ValueString() == segmentId
		})
		// A segment the platform already detached leaves no interface to remove, and
		// the configuration asked for exactly that.
		if interfaceIdx < 0 {
			continue
		}
		err = networks.RemoveNetworkInterface(gpcnClient, ctx, vmID, networkInterfaces[interfaceIdx].ID.ValueString())
		if err != nil {
			diags.AddError(
				ErrSummaryErrorUpdatingNetworkInterfaces,
				err.Error(),
			)
			return diags
		}
	}

	for _, segmentId := range added {
		err = networks.AddL2SegmentInterface(gpcnClient, ctx, vmID, segmentId)
		if err != nil {
			diags.AddError(
				ErrSummaryErrorUpdatingNetworkInterfaces,
				err.Error(),
			)
			return diags
		}
	}

	return diags
}

// UpdatePublicIPIfChanged binds and unbinds the address on the birth interface through
// the VPC verbs. The legacy per-interface routes answer 409 on a VPC interface, so they
// have no part here. An address Terraform acquired is released when the machine gives it
// up; a held address only detaches, because the operator owns it.
// The interfaces are read here rather than passed in, because an earlier step may have
// changed the list and with it the interface ids.
// Returns diagnostics if any errors occurred.
func UpdatePublicIPIfChanged(gpcnClient *client.GpcnClient, ctx context.Context, vmID string, state, plan ResourceModel) diag.Diagnostics {
	var diags diag.Diagnostics

	acquireChanged := !plan.AllocatePublicIp.Equal(state.AllocatePublicIp)
	heldChanged := !plan.PublicIpId.Equal(state.PublicIpId)
	if !acquireChanged && !heldChanged {
		return diags
	}

	networkInterfaces, err := networks.GetNetworkInterfaces(gpcnClient, ctx, vmID)
	if err != nil {
		diags.AddError(
			ErrSummaryErrorRetrievingNetworkIfaces,
			err.Error(),
		)
		return diags
	}

	// Find the primary network interface
	interfaceIdx := slices.IndexFunc(networkInterfaces, func(inter networks.ReadVirtualMachineNetworkDataResponseTF) bool {
		return inter.IsPrimary.ValueBool()
	})

	// This means none are set to primary, which should be impossible
	if interfaceIdx < 0 {
		diags.AddError(
			ErrSummaryNoPrimaryNetworkInterface,
			fmt.Sprintf(ErrDetailNoPrimaryNetworkInterface, vmID),
		)
		return diags
	}

	primary := networkInterfaces[interfaceIdx]
	// The VM detail projection carries no VPC, so the interface row is the only place
	// that names the VPC the address is acquired on.
	if primary.World.ValueString() != networks.NicWorldVpc || primary.VpcID.IsNull() {
		diags.AddError(
			ErrSummaryUnableToUpdatePublicIPConfiguration,
			fmt.Sprintf(ErrDetailPrimaryInterfaceNotOnAVpc, vmID),
		)
		return diags
	}
	vpcID := primary.VpcID.ValueString()
	primaryNetworkInterfaceId := primary.ID.ValueString()

	// The machine gives up what it no longer asks for before it takes anything on: one
	// machine carries one address, so an exchange has to free the interface first.
	if acquireChanged && state.AllocatePublicIp.ValueBool() && !primary.PublicIPID.IsNull() {
		acquiredID := primary.PublicIPID.ValueString()
		if err := vpcpublicips.DetachPublicIp(gpcnClient, ctx, vpcID, acquiredID); err != nil {
			return publicIpFailure(diags, err)
		}
		if err := vpcpublicips.ReleasePublicIp(gpcnClient, ctx, vpcID, acquiredID); err != nil {
			return publicIpFailure(diags, err)
		}
	}
	if heldChanged && !state.PublicIpId.IsNull() {
		if err := vpcpublicips.DetachPublicIp(gpcnClient, ctx, vpcID, state.PublicIpId.ValueString()); err != nil {
			return publicIpFailure(diags, err)
		}
	}

	if acquireChanged && plan.AllocatePublicIp.ValueBool() {
		// The API inserts the address row before it dispatches the job, so the id comes
		// back even from a failed acquisition.
		acquiredID, err := vpcpublicips.AcquirePublicIp(gpcnClient, ctx, vpcID)
		if err != nil {
			return publicIpFailure(diags, err)
		}
		if err := vpcpublicips.AttachPublicIp(gpcnClient, ctx, vpcID, acquiredID, primaryNetworkInterfaceId); err != nil {
			return publicIpFailure(diags, err)
		}
	}
	if heldChanged && !plan.PublicIpId.IsNull() {
		if err := vpcpublicips.AttachPublicIp(gpcnClient, ctx, vpcID, plan.PublicIpId.ValueString(), primaryNetworkInterfaceId); err != nil {
			return publicIpFailure(diags, err)
		}
	}

	return diags
}

// publicIpFailure frames every refusal of an address verb under one summary, because
// each one leaves the machine's address exactly as the last successful verb left it.
func publicIpFailure(diags diag.Diagnostics, err error) diag.Diagnostics {
	diags.AddError(
		ErrSummaryUnableToUpdatePublicIPConfiguration,
		err.Error(),
	)
	return diags
}

// UpdateSizeIfChanged handles VM size updates during VM update.
// Returns diagnostics if any errors occurred.
func UpdateSizeIfChanged(gpcnClient *client.GpcnClient, ctx context.Context, vmID string, state, plan ResourceModel) diag.Diagnostics {
	var diags diag.Diagnostics

	if plan.SizeId.Equal(state.SizeId) {
		return diags
	}

	tflog.Info(ctx, LogPerformingVirtualMachineResize)
	err := UpdateVirtualMachineSize(gpcnClient, ctx, vmID, plan.SizeId.ValueString())
	if err != nil {
		diags.AddError(
			ErrSummaryErrorUpdatingVMSize,
			err.Error(),
		)
		return diags
	}

	return diags
}

// UpdateChangeableAttributesIfChanged handles VM name and resource group updates during VM update.
// Returns diagnostics if any errors occurred.
func UpdateChangeableAttributesIfChanged(gpcnClient *client.GpcnClient, ctx context.Context, vmID string, state, plan ResourceModel) diag.Diagnostics {
	var diags diag.Diagnostics

	if plan.Name == state.Name && plan.ResourceGroupId == state.ResourceGroupId {
		return diags
	}

	body := map[string]any{}

	// Check for Name change
	if plan.Name != state.Name {
		body["name"] = plan.Name.ValueString()
	}

	// Check for ResourceGroupId change
	if plan.ResourceGroupId != state.ResourceGroupId {
		if plan.ResourceGroupId.IsNull() {
			// Explicitly set to nil to force a disconnect of the resource group from the VM
			body["resourceGroupId"] = nil
		} else {
			body["resourceGroupId"] = plan.ResourceGroupId.ValueString()
		}
	}

	tflog.Info(ctx, LogAttributesChangedUpdatingVirtualMachine)
	err := UpdateVirtualMachine(gpcnClient, ctx, vmID, body)
	if err != nil {
		diags.AddError(
			ErrSummaryErrorUpdatingVMAttributes,
			err.Error(),
		)
		return diags
	}

	return diags
}

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
// first, because GPCN caps a machine at five interfaces. A swap would otherwise need a
// free slot the machine does not have. The birth subnet interface is the primary one and
// is never a candidate: GPCN refuses to detach a primary interface. Both halves of the
// change read the live interface list the caller passes in. State can lag the platform
// after a failed read-back, and GPCN refuses a second interface on one segment.
// Returns diagnostics if any errors occurred.
func UpdateL2SegmentsIfChanged(gpcnClient *client.GpcnClient, ctx context.Context, vmID string, state, plan ResourceModel, networkInterfaces []networks.ReadVirtualMachineNetworkDataResponseTF) diag.Diagnostics {
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
		if err := networks.RemoveNetworkInterface(gpcnClient, ctx, vmID, networkInterfaces[interfaceIdx].ID.ValueString()); err != nil {
			diags.AddError(
				ErrSummaryErrorUpdatingNetworkInterfaces,
				err.Error(),
			)
			return diags
		}
	}

	for _, segmentId := range added {
		if slices.ContainsFunc(networkInterfaces, func(inter networks.ReadVirtualMachineNetworkDataResponseTF) bool {
			return inter.World.ValueString() == networks.NicWorldL2 &&
				inter.L2SegmentID.ValueString() == segmentId
		}) {
			continue
		}
		if err := networks.AddL2SegmentInterface(gpcnClient, ctx, vmID, segmentId); err != nil {
			diags.AddError(
				ErrSummaryErrorUpdatingNetworkInterfaces,
				err.Error(),
			)
			return diags
		}
	}

	return diags
}

// AcquiredAddress names an address one update acquired, and the VPC that holds it. The
// release verb needs both ids. A zero value says the update acquired nothing.
type AcquiredAddress struct {
	ID    string
	VpcID string
}

// UpdatePublicIPIfChanged binds and unbinds the address on the birth interface through
// the VPC verbs. The legacy per-interface routes answer 409 on a VPC interface, so they
// have no part here. An address Terraform acquired is released when the machine gives it
// up. A held address only detaches, because the operator owns it. The interfaces are
// read here rather than passed in. An earlier step can change the list, and the
// interface ids with it.
// Returns the address it acquired, so a failure after it can unwind it.
// Returns diagnostics if any error occurs.
func UpdatePublicIPIfChanged(gpcnClient *client.GpcnClient, ctx context.Context, vmID string, state, plan ResourceModel) (AcquiredAddress, diag.Diagnostics) {
	var diags diag.Diagnostics
	var acquired AcquiredAddress

	acquireChanged := !plan.AllocatePublicIp.Equal(state.AllocatePublicIp)
	heldChanged := !plan.PublicIpId.Equal(state.PublicIpId)
	if !acquireChanged && !heldChanged {
		return AcquiredAddress{}, diags
	}

	networkInterfaces, err := networks.GetNetworkInterfaces(gpcnClient, ctx, vmID)
	if err != nil {
		diags.AddError(
			ErrSummaryErrorRetrievingNetworkIfaces,
			err.Error(),
		)
		return AcquiredAddress{}, diags
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
		return AcquiredAddress{}, diags
	}

	primary := networkInterfaces[interfaceIdx]
	// The VM detail projection carries no VPC. The interface row is the only place that
	// names the VPC the address is acquired on.
	if primary.World.ValueString() != networks.NicWorldVpc || primary.VpcID.IsNull() {
		diags.AddError(
			ErrSummaryUnableToUpdatePublicIPConfiguration,
			fmt.Sprintf(ErrDetailPrimaryInterfaceNotOnAVpc, vmID),
		)
		return AcquiredAddress{}, diags
	}
	vpcID := primary.VpcID.ValueString()
	primaryNetworkInterfaceId := primary.ID.ValueString()
	// This value names the address the interface carries now. The held detach below
	// clears it when that detach gives up the same address. The acquire then reads
	// what the interface holds at that moment.
	carriedID := primary.PublicIPID

	// The machine gives up what it no longer asks for before it takes anything on. One
	// machine carries one address, so an exchange must free the interface first. The
	// operator holds an address the plan names. GPCN releases an attached address, so a
	// release of that one destroys it.
	if acquireChanged && state.AllocatePublicIp.ValueBool() && !carriedID.IsNull() &&
		carriedID.ValueString() != plan.PublicIpId.ValueString() {
		releasedID := carriedID.ValueString()
		if err := vpcpublicips.DetachPublicIp(gpcnClient, ctx, vpcID, releasedID); err != nil {
			return AcquiredAddress{}, publicIpFailure(diags, err)
		}
		if err := vpcpublicips.ReleasePublicIp(gpcnClient, ctx, vpcID, releasedID); err != nil {
			// The detach already took the address off the interface. No later gate finds
			// it, so this diagnostic is the last record of it.
			diags.AddError(
				ErrSummaryUnableToUpdatePublicIPConfiguration,
				fmt.Sprintf(ErrDetailPublicIpOrphaned, releasedID, vmID, ErrPhrasePublicIpReleaseFailed, err.Error()),
			)
			return AcquiredAddress{}, diags
		}
	}
	if heldChanged && !state.PublicIpId.IsNull() {
		heldID := state.PublicIpId.ValueString()
		if err := vpcpublicips.DetachPublicIp(gpcnClient, ctx, vpcID, heldID); err != nil {
			return AcquiredAddress{}, publicIpFailure(diags, err)
		}
		if carriedID.ValueString() == heldID {
			carriedID = types.StringNull()
		}
	}

	if acquireChanged && plan.AllocatePublicIp.ValueBool() {
		// The detach above clears an address this call gives up. Anything the interface
		// still carries comes from somewhere else, and GPCN refuses a second address.
		if !carriedID.IsNull() {
			diags.AddError(
				ErrSummaryUnableToUpdatePublicIPConfiguration,
				fmt.Sprintf(ErrDetailPrimaryInterfaceCarriesAForeignAddress, vmID, carriedID.ValueString()),
			)
			return AcquiredAddress{}, diags
		}
		acquiredID, acquireDiags := acquireAndAttachPublicIp(gpcnClient, ctx, vmID, vpcID, primaryNetworkInterfaceId)
		diags.Append(acquireDiags...)
		if diags.HasError() {
			return AcquiredAddress{}, diags
		}
		acquired = AcquiredAddress{ID: acquiredID, VpcID: vpcID}
	}
	// GPCN answers 409 for a second attach of an address a machine carries. A retry of a
	// change whose read-back fails finds that address already in place.
	if heldChanged && !plan.PublicIpId.IsNull() &&
		carriedID.ValueString() != plan.PublicIpId.ValueString() {
		if err := vpcpublicips.AttachPublicIp(gpcnClient, ctx, vpcID, plan.PublicIpId.ValueString(), primaryNetworkInterfaceId); err != nil {
			// A validator that meets an unknown allocate_public_ip lets both inputs
			// through, and GPCN then refuses this attach. The acquire above already
			// happened, so the caller unwinds it.
			return acquired, publicIpFailure(diags, err)
		}
	}

	return acquired, diags
}

// acquireAndAttachPublicIp takes an address and binds it to the interface. The API
// inserts the address row before it dispatches the job. Every failure after the request
// therefore leaves a real address, and the function gives that address back. Only a
// release that fails too leaves one to find, and that report names it.
// Returns the id of the address it bound, and diagnostics if any errors occurred.
func acquireAndAttachPublicIp(gpcnClient *client.GpcnClient, ctx context.Context, vmID, vpcID, nicID string) (string, diag.Diagnostics) {
	var diags diag.Diagnostics

	acquiredID, err := vpcpublicips.AcquirePublicIp(gpcnClient, ctx, vpcID)
	if err != nil {
		// A request the platform refused outright inserts no row and names no address.
		if acquiredID == "" {
			return "", publicIpFailure(diags, err)
		}
		// A failed acquire parks the row and keeps its provider reference. The release
		// is the exit, and the platform takes one from that state.
		if releaseErr := vpcpublicips.ReleasePublicIp(gpcnClient, ctx, vpcID, acquiredID); releaseErr != nil {
			diags.AddError(
				ErrSummaryUnableToUpdatePublicIPConfiguration,
				fmt.Sprintf(ErrDetailPublicIpAcquireJobFailedReleaseFailed, acquiredID, vmID, err.Error(), releaseErr.Error()),
			)
			return "", diags
		}
		diags.AddError(
			ErrSummaryUnableToUpdatePublicIPConfiguration,
			fmt.Sprintf(ErrDetailPublicIpAcquireJobFailedReleased, acquiredID, vmID, err.Error()),
		)
		return "", diags
	}

	attachErr := vpcpublicips.AttachPublicIp(gpcnClient, ctx, vpcID, acquiredID, nicID)
	if attachErr == nil {
		return acquiredID, diags
	}

	// An address no interface carries costs the tenant money and hides from Terraform,
	// so it goes back before the report.
	if releaseErr := vpcpublicips.ReleasePublicIp(gpcnClient, ctx, vpcID, acquiredID); releaseErr != nil {
		diags.AddError(
			ErrSummaryUnableToUpdatePublicIPConfiguration,
			fmt.Sprintf(ErrDetailPublicIpAttachFailedReleaseFailed, acquiredID, vmID, attachErr.Error(), releaseErr.Error()),
		)
		return "", diags
	}
	diags.AddError(
		ErrSummaryUnableToUpdatePublicIPConfiguration,
		fmt.Sprintf(ErrDetailPublicIpAttachFailedReleased, acquiredID, vmID, attachErr.Error()),
	)
	return "", diags
}

// UnwindAcquiredAddress gives back an address the update took when a step after it
// fails. The machine then ends where it started, and the retry acquires afresh. GPCN
// releases an attached address, so no detach comes first. No attribute records the
// address, so a release that fails too names it.
// Returns the diagnostic that reports the unwind, and none when nothing was acquired.
func UnwindAcquiredAddress(gpcnClient *client.GpcnClient, ctx context.Context, vmID string, acquired AcquiredAddress, stepDiags diag.Diagnostics) diag.Diagnostics {
	var diags diag.Diagnostics
	if acquired.ID == "" {
		return diags
	}

	stepDetail := ""
	if stepErrors := stepDiags.Errors(); len(stepErrors) > 0 {
		stepDetail = stepErrors[0].Detail()
	}

	if releaseErr := vpcpublicips.ReleasePublicIp(gpcnClient, ctx, acquired.VpcID, acquired.ID); releaseErr != nil {
		diags.AddError(
			ErrSummaryUnableToUpdatePublicIPConfiguration,
			fmt.Sprintf(ErrDetailAcquiredAddressReleaseFailedAfterStepFailure,
				acquired.ID, vmID, stepDetail, releaseErr.Error()),
		)
		return diags
	}

	diags.AddError(
		ErrSummaryUnableToUpdatePublicIPConfiguration,
		fmt.Sprintf(ErrDetailAcquiredAddressReleasedAfterStepFailure, acquired.ID, vmID, stepDetail),
	)
	return diags
}

// publicIpFailure frames every refusal of an address verb under one summary. Each one
// leaves the machine's address exactly as the last successful verb left it.
func publicIpFailure(diags diag.Diagnostics, err error) diag.Diagnostics {
	diags.AddError(
		ErrSummaryUnableToUpdatePublicIPConfiguration,
		err.Error(),
	)
	return diags
}

// UpdateSizeIfChanged handles VM size updates during VM update. The caller passes the
// live detail.
// Returns diagnostics if any errors occurred.
func UpdateSizeIfChanged(gpcnClient *client.GpcnClient, ctx context.Context, vmID string, state, plan ResourceModel, live *ReadVirtualMachinesResponse) diag.Diagnostics {
	var diags diag.Diagnostics

	if plan.SizeId.Equal(state.SizeId) {
		return diags
	}

	// The machine can already carry the planned SKU when a read-back failed after an
	// earlier resize. GPCN answers 400 for a resize to the size the machine has.
	if live.Data.Configuration.SkuId == plan.SizeId.ValueString() {
		tflog.Info(ctx, LogVirtualMachineAlreadyCarriesTheSize)
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

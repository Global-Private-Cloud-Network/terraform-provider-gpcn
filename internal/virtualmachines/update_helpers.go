package virtualmachines

import (
	"context"
	"fmt"
	"slices"

	"terraform-provider-gpcn/internal/client"
	"terraform-provider-gpcn/internal/networks"

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

// UpdatePublicIPIfChanged handles public IP allocation/release during VM update.
// Returns diagnostics if any errors occurred.
//
// NOTE: This function always fetches fresh network interfaces rather than accepting
// them as a parameter. This is intentional because UpdateNetworkInterfacesIfChanged
// may have modified the interfaces (added/removed), potentially changing interface IDs.
// The slight overhead of an extra API call is acceptable to ensure correctness.
func UpdatePublicIPIfChanged(gpcnClient *client.GpcnClient, ctx context.Context, vmID string, state, plan ResourceModel) diag.Diagnostics {
	var diags diag.Diagnostics

	if plan.AllocatePublicIp == state.AllocatePublicIp {
		return diags
	}

	// Fetch fresh network interfaces - required because UpdateNetworkInterfacesIfChanged
	// may have modified the interface list
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

	primaryNetworkInterfaceId := networkInterfaces[interfaceIdx].ID.ValueString()

	if plan.AllocatePublicIp.ValueBool() {
		err = networks.AllocatePublicIp(gpcnClient, ctx, vmID, primaryNetworkInterfaceId)
	} else {
		err = networks.ReleasePublicIp(gpcnClient, ctx, vmID, primaryNetworkInterfaceId)
	}

	if err != nil {
		diags.AddError(
			ErrSummaryUnableToUpdatePublicIPConfiguration,
			err.Error(),
		)
		return diags
	}

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

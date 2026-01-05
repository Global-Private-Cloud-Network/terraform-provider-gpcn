package virtualmachines

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"terraform-provider-gpcn/internal/helpers"
	"terraform-provider-gpcn/internal/networks"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

// Validates that the planned virtual machine size is larger than the current. Returns the new sizeId if so
func ValidatePlanSizeLargerThanStateSize(httpClient *http.Client, ctx context.Context, state, plan ResourceModel) (int64, error) {
	tflog.Info(ctx, LogSizeChangedVerifyingLarger)
	var planSize ResourceModelSize
	plan.Size.As(ctx, &planSize, basetypes.ObjectAsOptions{})
	var stateSize ResourceModelSize
	state.Size.As(ctx, &stateSize, basetypes.ObjectAsOptions{})
	// This had preliminary validation, but verify it's up-to-date
	_, sizes, err := GetVirtualMachineSizeConfigurationId(httpClient, ctx, plan.DatacenterId.ValueString(), planSize.Tier.ValueString())
	if err != nil {
		return -1, err
	}

	// Verify the new size is larger than the old
	stateSizeIdx := slices.IndexFunc(sizes, func(virtualMachineSize VirtualMachineConfigurationsTF) bool {
		return strings.EqualFold(virtualMachineSize.Code.ValueString(), stateSize.Tier.ValueString())
	})
	planSizeIdx := slices.IndexFunc(sizes, func(virtualMachineSize VirtualMachineConfigurationsTF) bool {
		return strings.EqualFold(virtualMachineSize.Code.ValueString(), planSize.Tier.ValueString())
	})

	if sizes[stateSizeIdx].CPUCores.ValueInt64() > sizes[planSizeIdx].CPUCores.ValueInt64() {
		return -1, errors.New("the target virtual machine size is smaller than the old size. This shouldn't happen, since the validation should check for a smaller size (and force a replace), but in case it does, make sure the target size is LARGER than the current")
	}

	return sizes[planSizeIdx].ID.ValueInt64(), nil
}

func ValidateAllNetworksAreNotRemoved(oldNetworksList, newNetworksList types.List) error {
	if oldNetworksList.IsNull() {
		return nil
	}
	// If old networks is not nil and new networks is, that's a problem
	if newNetworksList.IsNull() {
		return errors.New(ErrDetailCannotRemoveLastNetwork)
	}
	return nil
}

// Determines if the new networks to be added will cause the network interfaces size to exceed its cap
func ValidateNetworkInterfacesDoesNotExceedCap(oldNetworksList, newNetworksList []string, networkInterfaces []networks.ReadVirtualMachineNetworkDataResponseTF) error {
	// Get newly added values
	addedValues, _ := helpers.CheckListForDifferences(oldNetworksList, newNetworksList)
	if len(addedValues)+len(networkInterfaces) > MAX_NETWORKS_ATTACHED_ALLOWED {
		return fmt.Errorf(ErrDetailAddedNetworksExceedsMax, MAX_NETWORKS_ATTACHED_ALLOWED)
	}
	return nil
}

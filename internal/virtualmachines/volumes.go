package virtualmachines

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"terraform-provider-gpcn/internal/helpers"
	"terraform-provider-gpcn/internal/volumes"

	"github.com/hashicorp/terraform-plugin-log/tflog"
)

// UpdateVolumes handles attaching and detaching volumes for a virtual machine
func UpdateVolumes(httpClient *http.Client, ctx context.Context, vmId string, oldVolumesList, newVolumesList []string) error {
	tflog.Info(ctx, "VolumeIds have changed, performing detaches and attaches in that order")

	addedValues, removedValues := helpers.CheckListForDifferences(oldVolumesList, newVolumesList)
	tflog.Info(ctx, fmt.Sprintf("VolumeIds to be removed are: [%s]", helpers.JoinStrings(removedValues)))
	tflog.Info(ctx, fmt.Sprintf("VolumeIds to be added are: [%s]", helpers.JoinStrings(addedValues)))

	// Do removals first, since there is a cap of 5 volumes
	for _, val := range removedValues {
		tflog.Info(ctx, fmt.Sprintf("Removing volume for ID: %s", val))

		// Make sure volume actually needs to be removed
		// If the volume was deleted outside of terraform, it would've detached first and the volumeIds wouldn't be updated
		_, err := volumes.GetVolume(httpClient, ctx, val)
		if err != nil && strings.Contains(err.Error(), "404") {
			// If we are unable to get the volume, this is likely due to it already being deleted. Skip past it
			continue
		}
		err = volumes.RemoveVolumeFromVirtualMachine(httpClient, ctx, val)
		if err != nil {
			return fmt.Errorf("error removing volume with ID %s: %w", val, err)
		}
	}

	// Add new volumes
	for _, val := range addedValues {
		tflog.Info(ctx, fmt.Sprintf("Adding volume for ID: %s", val))
		err := volumes.AddVolumeToVirtualMachine(httpClient, ctx, vmId, val)
		if err != nil {
			return fmt.Errorf("error adding volume with ID %s: %w", val, err)
		}
	}

	return nil
}

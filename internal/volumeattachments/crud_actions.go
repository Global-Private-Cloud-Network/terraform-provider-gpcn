package volumeattachments

import (
	"context"

	"terraform-provider-gpcn/internal/client"
	"terraform-provider-gpcn/internal/volumes"
)

// GetAttachedVMId returns the VM ID that the given volume is currently attached to.
// Returns an empty string if the volume is not attached.
func GetAttachedVMId(gpcnClient *client.GpcnClient, ctx context.Context, volumeId string) (string, error) {
	vol, err := volumes.GetVolume(gpcnClient, ctx, volumeId)
	if err != nil {
		return "", err
	}
	return vol.Data.VirtualMachineId, nil
}

// GPCN accepts an attach to a Running, Stopped or Shutoff machine, so the
// provider does not stop it. GPCN refuses any other status, and that refusal
// reaches the operator.
func AttachVolume(gpcnClient *client.GpcnClient, ctx context.Context, vmId, volumeId string) error {
	return volumes.AddVolumeToVirtualMachine(gpcnClient, ctx, vmId, volumeId)
}

// GPCN reads the machine from the volume row, so the detach names only the volume.
func DetachVolume(gpcnClient *client.GpcnClient, ctx context.Context, volumeId string) error {
	return volumes.RemoveVolumeFromVirtualMachine(gpcnClient, ctx, volumeId)
}

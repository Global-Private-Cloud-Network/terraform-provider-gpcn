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

// AttachVolume attaches the volume to the virtual machine.
// GPCN accepts an attach to a running machine, so the provider does not stop it.
// GPCN refuses a machine that is not settled, and that refusal reaches the operator.
func AttachVolume(gpcnClient *client.GpcnClient, ctx context.Context, vmId, volumeId string) error {
	return volumes.AddVolumeToVirtualMachine(gpcnClient, ctx, vmId, volumeId)
}

// DetachVolume detaches the volume from the virtual machine it is attached to.
// GPCN reads the machine from the volume row, so the detach names only the volume.
func DetachVolume(gpcnClient *client.GpcnClient, ctx context.Context, volumeId string) error {
	return volumes.RemoveVolumeFromVirtualMachine(gpcnClient, ctx, volumeId)
}

package volumeattachments

import (
	"context"
	"fmt"
	"strings"

	"terraform-provider-gpcn/internal/client"
	"terraform-provider-gpcn/internal/virtualmachines"
	"terraform-provider-gpcn/internal/volumes"

	"github.com/hashicorp/terraform-plugin-log/tflog"
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

// AttachVolume stops the VM if required by network_hotplug, attaches the volume, then restarts.
func AttachVolume(gpcnClient *client.GpcnClient, ctx context.Context, vmId, volumeId string) error {
	stopped, err := conditionallyStopVM(gpcnClient, ctx, vmId)
	if err != nil {
		return fmt.Errorf(ErrDetailVMStopFailed, vmId, err)
	}

	if err = volumes.AddVolumeToVirtualMachine(gpcnClient, ctx, vmId, volumeId); err != nil {
		// Best-effort restart even if attach fails
		if stopped {
			if startErr := conditionallyStartVM(gpcnClient, ctx, vmId); startErr != nil {
				tflog.Warn(ctx, LogBestEffortStartFailed, map[string]any{"error": startErr.Error()})
			}
		}
		return err
	}

	if stopped {
		if err = conditionallyStartVM(gpcnClient, ctx, vmId); err != nil {
			return fmt.Errorf(ErrDetailVMStartFailed, vmId, err)
		}
	}
	return nil
}

// DetachVolume stops the VM if required by network_hotplug, detaches the volume, then restarts.
func DetachVolume(gpcnClient *client.GpcnClient, ctx context.Context, vmId, volumeId string) error {
	stopped, err := conditionallyStopVM(gpcnClient, ctx, vmId)
	if err != nil {
		return fmt.Errorf(ErrDetailVMStopFailed, vmId, err)
	}

	if err = volumes.RemoveVolumeFromVirtualMachine(gpcnClient, ctx, volumeId); err != nil {
		if stopped {
			if startErr := conditionallyStartVM(gpcnClient, ctx, vmId); startErr != nil {
				tflog.Warn(ctx, LogBestEffortStartFailed, map[string]any{"error": startErr.Error()})
			}
		}
		return err
	}

	if stopped {
		if err = conditionallyStartVM(gpcnClient, ctx, vmId); err != nil {
			return fmt.Errorf(ErrDetailVMStartFailed, vmId, err)
		}
	}
	return nil
}

// conditionallyStopVM stops the VM only if network_hotplug is disabled and the VM is running.
// Returns true if this call actually stopped the VM (meaning the caller should restart it).
// Handles the concurrent-stop race: if a stop call fails because another process already
// stopped the VM, we verify the status and return (false, nil) so the caller does NOT restart.
func conditionallyStopVM(gpcnClient *client.GpcnClient, ctx context.Context, vmId string) (bool, error) {
	vmResp, err := virtualmachines.GetVirtualMachine(gpcnClient, ctx, vmId)
	if err != nil {
		return false, err
	}

	if vmResp.Data.NetworkHotplug == 1 {
		return false, nil
	}

	if !strings.EqualFold(vmResp.Data.Status, virtualmachines.VMStatusRunning.String()) {
		tflog.Info(ctx, LogSkippingStopVMAlreadyStopped)
		return false, nil
	}

	tflog.Info(ctx, LogStoppingVMBeforeAttach)
	err = virtualmachines.StopVirtualMachine(gpcnClient, ctx, vmId)
	if err != nil {
		// Stop failed — check if a concurrent operation already stopped the VM
		checkResp, checkErr := virtualmachines.GetVirtualMachine(gpcnClient, ctx, vmId)
		if checkErr != nil {
			return false, err // return original error
		}
		if strings.EqualFold(checkResp.Data.Status, virtualmachines.VMStatusShutoff.String()) {
			tflog.Info(ctx, LogSkippingStopVMAlreadyStopped)
			return false, nil // someone else stopped it; we should not start it
		}
		return false, err
	}

	return true, nil
}

// conditionallyStartVM starts the VM. If a concurrent operation already started it, treats as success.
func conditionallyStartVM(gpcnClient *client.GpcnClient, ctx context.Context, vmId string) error {
	tflog.Info(ctx, LogStartingVMAfterAttach)
	err := virtualmachines.StartVirtualMachine(gpcnClient, ctx, vmId)
	if err != nil {
		checkResp, checkErr := virtualmachines.GetVirtualMachine(gpcnClient, ctx, vmId)
		if checkErr != nil {
			return err
		}
		if strings.EqualFold(checkResp.Data.Status, virtualmachines.VMStatusRunning.String()) {
			tflog.Info(ctx, LogSkippingStartVMAlreadyRunning)
			return nil
		}
		return err
	}
	return nil
}

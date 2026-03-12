package virtualmachines

import (
	"context"
	"fmt"
	"net/http"

	"terraform-provider-gpcn/internal/client"

	"github.com/hashicorp/terraform-plugin-log/tflog"
)

// Issues a Start command to the virtual machine and then optionally polls until it is verified started
func StartVirtualMachine(gpcnClient *client.GpcnClient, ctx context.Context, virtualMachineId string, waitForStatusUpdate bool) error {
	tflog.Info(ctx, fmt.Sprintf(LogStartingStartVMWithID, virtualMachineId))
	// Start is sometimes finnicky. Give it 3 tries of 2min each to be in Running status, kicking off a new request each time. No harm in doing so
	for idx := range 3 {
		tflog.Info(ctx, fmt.Sprintf(LogStartingIteration, idx))
		request, err := http.NewRequestWithContext(ctx, "POST", BASE_URL_V1+virtualMachineId+"/start", nil)
		if err != nil {
			return err
		}

		response, err := gpcnClient.DoWithRetry(request)
		if err != nil {
			return err
		}
		defer response.Body.Close()

		if waitForStatusUpdate {
			readVirtualMachinesResponse, err := PollForVirtualMachineStatus(gpcnClient, ctx, virtualMachineId, []string{VMStatusRunning.String()}, 120, 0)
			if err != nil {
				return err
			}
			if readVirtualMachinesResponse.Data.Status == VMStatusRunning.String() {
				// If it started running, break a bit earlier
				break
			}
		} else {
			break
		}
	}
	tflog.Info(ctx, fmt.Sprintf(LogSuccessfullyStartedVMWithID, virtualMachineId))
	return nil
}

// Issues a Stop command to the virtual machine and then polls until it is verified stopped
func StopVirtualMachine(gpcnClient *client.GpcnClient, ctx context.Context, virtualMachineId string) error {
	tflog.Info(ctx, fmt.Sprintf(LogStartingStopVMWithID, virtualMachineId))
	request, err := http.NewRequestWithContext(ctx, "POST", BASE_URL_V1+virtualMachineId+"/stop", nil)
	if err != nil {
		return err
	}

	response, err := gpcnClient.DoWithRetry(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	_, err = PollForVirtualMachineStatus(gpcnClient, ctx, virtualMachineId, []string{VMStatusShutoff.String()}, DEFAULT_NETWORK_TIMEOUT_SECONDS, 0)
	if err != nil {
		return err
	}
	tflog.Info(ctx, fmt.Sprintf(LogSuccessfullyStoppedVMWithID, virtualMachineId))
	return nil
}

package virtualmachines

import (
	"context"
	"fmt"
	"net/http"

	"github.com/hashicorp/terraform-plugin-log/tflog"
)

// Issues a Start command to the virtual machine and then optionally polls until it is verified started
func StartVirtualMachine(httpClient *http.Client, ctx context.Context, virtualMachineId string, waitForStatusUpdate bool) error {
	tflog.Info(ctx, fmt.Sprintf(LogStartingStartVMWithID, virtualMachineId))
	// Start is sometimes finnicky. Give it 3 tries of 2min each to be in Running status, kicking off a new request each time. No harm in doing so
	for idx := range 3 {
		tflog.Info(ctx, fmt.Sprintf(LogStartingIteration, idx))
		request, err := http.NewRequest("POST", BASE_URL+virtualMachineId+"/start", nil)
		if err != nil {
			return err
		}

		_, err = httpClient.Do(request)
		if err != nil {
			return err
		}

		if waitForStatusUpdate {
			readVirtualMachinesResponse, err := PollForVirtualMachineStatus(httpClient, ctx, virtualMachineId, []string{Running}, 120)
			if err != nil {
				return err
			}
			if readVirtualMachinesResponse.Data.Status == Running {
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
func StopVirtualMachine(httpClient *http.Client, ctx context.Context, virtualMachineId string) error {
	tflog.Info(ctx, fmt.Sprintf(LogStartingStopVMWithID, virtualMachineId))
	request, err := http.NewRequest("POST", BASE_URL+virtualMachineId+"/stop", nil)
	if err != nil {
		return err
	}

	_, err = httpClient.Do(request)
	if err != nil {
		return err
	}
	_, err = PollForVirtualMachineStatus(httpClient, ctx, virtualMachineId, []string{Shutoff}, DEFAULT_NETWORK_TIMEOUT_SECONDS)
	if err != nil {
		return err
	}
	tflog.Info(ctx, fmt.Sprintf(LogSuccessfullyStoppedVMWithID, virtualMachineId))
	return nil
}

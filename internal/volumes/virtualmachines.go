package volumes

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"terraform-provider-gpcn/internal/client"
)

// Attach a volume to the virtual machine
func AddVolumeToVirtualMachine(gpcnClient *client.GpcnClient, ctx context.Context, virtualMachineId, volumeId string) error {
	attachVolumeRequestBody := map[string]string{
		"virtualMachineId": virtualMachineId,
	}

	jsonAttachVolumeRequestBody, err := json.Marshal(attachVolumeRequestBody)
	if err != nil {
		return errors.New("error marshaling the json request body GPCN Virtual Machines - Attach Volume")
	}
	// The holder can have made this attach, so each send reads the volume again.
	response, err := client.SendWaitingForHolder(gpcnClient, ctx, BusyRetryInterval, func() (*http.Response, error) {
		volume, err := GetVolume(gpcnClient, ctx, volumeId)
		if err != nil {
			return nil, err
		}
		if volume.Data.VirtualMachineId == virtualMachineId {
			return nil, nil
		}

		request, err := http.NewRequestWithContext(ctx, "PUT", BASE_URL_V1+volumeId+"/attach", bytes.NewBuffer(jsonAttachVolumeRequestBody))
		if err != nil {
			return nil, err
		}
		return gpcnClient.DoWithRetry(request)
	})
	if err != nil {
		return err
	}
	if response == nil {
		return nil
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return err
	}

	var attachVolumeResponse client.JobStatusSingularResponse
	err = json.Unmarshal(body, &attachVolumeResponse)

	if err != nil {
		return err
	}

	_, err = client.PerformLongPolling(gpcnClient, ctx, "Attach GPCN Volume to VM", attachVolumeResponse.Data.JobID)

	if err != nil {
		return err
	}

	return nil
}

// RemoveVolumeFromVirtualMachine sends no detach when the volume is not on that
// machine. The detach names only the volume, so it can act on another machine.
func RemoveVolumeFromVirtualMachine(gpcnClient *client.GpcnClient, ctx context.Context, virtualMachineId, volumeId string) error {
	response, err := client.SendWaitingForHolder(gpcnClient, ctx, BusyRetryInterval, func() (*http.Response, error) {
		volume, err := GetVolume(gpcnClient, ctx, volumeId)
		if err != nil {
			return nil, err
		}
		if volume.Data.VirtualMachineId == "" || volume.Data.VirtualMachineId != virtualMachineId {
			return nil, nil
		}

		request, err := http.NewRequestWithContext(ctx, "PUT", BASE_URL_V1+volumeId+"/detach", nil)
		if err != nil {
			return nil, err
		}
		return gpcnClient.DoWithRetry(request)
	})
	if err != nil {
		return err
	}
	if response == nil {
		return nil
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return err
	}

	var detachVolumeResponse client.JobStatusSingularResponse
	err = json.Unmarshal(body, &detachVolumeResponse)

	if err != nil {
		return err
	}

	_, err = client.PerformLongPolling(gpcnClient, ctx, "Detach GPCN Volume from VM", detachVolumeResponse.Data.JobID)

	if err != nil {
		return err
	}

	return nil
}

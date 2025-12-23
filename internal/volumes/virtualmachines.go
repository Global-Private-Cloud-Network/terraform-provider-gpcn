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
func AddVolumeToVirtualMachine(httpClient *http.Client, ctx context.Context, virtualMachineId, volumeId string) error {
	attachVolumeRequestBody := map[string]string{
		"virtualMachineId": virtualMachineId,
	}

	jsonAttachVolumeRequestBody, err := json.Marshal(attachVolumeRequestBody)
	if err != nil {
		return errors.New("error marshaling the json request body GPCN Virtual Machines - Attach Volume")
	}
	request, err := http.NewRequest("PUT", BASE_URL_V1+volumeId+"/attach", bytes.NewBuffer(jsonAttachVolumeRequestBody))
	if err != nil {
		return err
	}

	response, err := httpClient.Do(request)
	if err != nil {
		return err
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

	_, err = client.PerformLongPolling(httpClient, ctx, "Attach GPCN Volume to VM", attachVolumeResponse.Data.JobID)

	if err != nil {
		return err
	}

	return nil
}

// Remove a volume from the virtual machine
func RemoveVolumeFromVirtualMachine(httpClient *http.Client, ctx context.Context, volumeId string) error {
	request, err := http.NewRequest("PUT", BASE_URL_V1+volumeId+"/detach", nil)
	if err != nil {
		return err
	}

	response, err := httpClient.Do(request)
	if err != nil {
		return err
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

	_, err = client.PerformLongPolling(httpClient, ctx, "Detach GPCN Volume from VM", detachVolumeResponse.Data.JobID)

	if err != nil {
		return err
	}

	return nil
}

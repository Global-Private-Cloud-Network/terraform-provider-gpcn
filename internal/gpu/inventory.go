package gpu

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"

	"github.com/hashicorp/terraform-plugin-log/tflog"
)

type gpuInventoryResponse struct {
	Data []struct {
		ID           string `json:"id"`
		Name         string `json:"name"`
		Code         string `json:"code"`
		Description  string `json:"description"`
		Availability []struct {
			DatacenterId   string `json:"datacenterId"`
			DatacenterName string `json:"datacenterName"`
			DatacenterCode string `json:"datacenterCode"`
			GPUCounts      []struct {
				Count     int64 `json:"count"`
				Available int64 `json:"available"`
				Specs     struct {
					GPUDescription string `json:"gpuDescription"`
					VCPU           int64  `json:"vcpu"`
					Memory         int64  `json:"memoryGiB"`
					Storage        int64  `json:"storageGB"`
				} `json:"specs"`
			} `json:"gpuCounts"`
		} `json:"availability"`
	} `json:"data"`
}

func CheckInventory(httpClient *http.Client, ctx context.Context, model ResourceModel) (*gpuInventoryResponse, error) {
	seriesCode := model.SeriesCode.ValueString()
	datacenterId := model.DatacenterId.ValueString()
	gpuCount := model.GPUCount.ValueInt64()

	tflog.Info(ctx, fmt.Sprintf(LogStartingCheckInventory, seriesCode, datacenterId, gpuCount))

	// Format URL
	u, _ := url.Parse(BASE_URL_V1 + "inventory")
	q := u.Query()
	q.Add("code", seriesCode)
	q.Add("datacenterId", datacenterId)
	q.Add("count", strconv.FormatInt(gpuCount, 10))
	u.RawQuery = q.Encode()

	tflog.Info(ctx, LogConstructedInventoryRequestURL)

	request, err := http.NewRequest("GET", u.String(), nil)
	if err != nil {
		return nil, err
	}

	response, err := httpClient.Do(request)
	if err != nil {
		return nil, err
	}

	// Read the response body and process it as gpuInventoryResponse
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}
	_ = response.Body.Close()

	var gpuInventoryResponse gpuInventoryResponse
	err = json.Unmarshal(body, &gpuInventoryResponse)

	if err != nil {
		return nil, err
	}

	tflog.Info(ctx, LogSuccessfullyRetrievedInventory)

	// Check availability by Data[0], Availability[0] and GPUCounts[0]
	// Data list will always be of size one since we are filtering by series
	// Availability list will always be of size one since we are filtering by datacenterId
	// GPUCounts list will always be of size one since we are filtering by gpu_count

	tflog.Info(ctx, LogValidatingInventoryResponseStructure)

	// Validate response structure
	if len(gpuInventoryResponse.Data) == 0 {
		return nil, fmt.Errorf(ErrDetailMalformedResponseMissingData)
	}
	if len(gpuInventoryResponse.Data[0].Availability) == 0 {
		return nil, fmt.Errorf(ErrDetailMalformedResponseMissingAvailability)
	}
	if len(gpuInventoryResponse.Data[0].Availability[0].GPUCounts) == 0 {
		return nil, fmt.Errorf(ErrDetailMalformedResponseMissingGPUCounts)
	}

	// Check if inventory is available
	if gpuInventoryResponse.Data[0].Availability[0].GPUCounts[0].Available <= 0 {
		return nil, fmt.Errorf(ErrDetailNoInventoryAvailable, seriesCode, datacenterId, gpuCount)
	}

	tflog.Info(ctx, fmt.Sprintf(LogInventoryAvailable, seriesCode, datacenterId, gpuCount))
	return &gpuInventoryResponse, nil
}

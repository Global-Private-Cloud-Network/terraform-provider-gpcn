package gpu

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"

	"terraform-provider-gpcn/internal/client"

	"github.com/hashicorp/terraform-plugin-log/tflog"
)

type inventoryResp struct {
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
				Count         int64      `json:"count"`
				AvailableSkus []struct{} `json:"availableSkus"`
				Specs         struct {
					GPUDescription string `json:"gpuDescription"`
					VCPU           int64  `json:"vcpu"`
					Memory         int64  `json:"memoryGiB"`
					Storage        int64  `json:"storageGB"`
				} `json:"specs"`
			} `json:"gpuCounts"`
		} `json:"availability"`
	} `json:"data"`
}

func CheckInventory(gpcnClient *client.GpcnClient, ctx context.Context, model ResourceModel) (*inventoryResp, error) {
	seriesCode := model.SeriesCode.ValueString()
	datacenterId := model.DatacenterId.ValueString()
	gpuCount := model.GPUCount.ValueInt64()

	tflog.Info(ctx, fmt.Sprintf(LogStartingCheckInventory, seriesCode, datacenterId, gpuCount))

	// Format URL
	u, err := url.Parse(BASE_URL_V1 + "inventory")
	if err != nil {
		return nil, fmt.Errorf("failed to parse inventory URL: %w", err)
	}
	q := u.Query()
	q.Add("code", seriesCode)
	q.Add("datacenterId", datacenterId)
	q.Add("count", strconv.FormatInt(gpuCount, 10))
	u.RawQuery = q.Encode()

	tflog.Info(ctx, LogConstructedInventoryRequestURL)

	request, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return nil, err
	}

	response, err := gpcnClient.DoWithRetry(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	// Read the response body and process it as inventoryResp
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}

	var invResp inventoryResp
	err = json.Unmarshal(body, &invResp)

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
	if len(invResp.Data) == 0 {
		return nil, errors.New(ErrDetailMalformedResponseMissingData)
	}
	if len(invResp.Data[0].Availability) == 0 {
		return nil, fmt.Errorf(ErrDetailNoInventoryAvailable, seriesCode, datacenterId, gpuCount)
	}
	if len(invResp.Data[0].Availability[0].GPUCounts) == 0 {
		return nil, fmt.Errorf(ErrDetailNoInventoryAvailable, seriesCode, datacenterId, gpuCount)
	}

	// Check if inventory is available
	if len(invResp.Data[0].Availability[0].GPUCounts[0].AvailableSkus) <= 0 {
		return nil, fmt.Errorf(ErrDetailNoInventoryAvailable, seriesCode, datacenterId, gpuCount)
	}

	tflog.Info(ctx, fmt.Sprintf(LogInventoryAvailable, seriesCode, datacenterId, gpuCount))
	return &invResp, nil
}

package gpu

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"

	"terraform-provider-gpcn/internal/client"

	"github.com/hashicorp/terraform-plugin-log/tflog"
)

type inventoryResp struct {
	Data struct {
		Series []struct {
			ID           string `json:"id"`
			Name         string `json:"name"`
			Code         string `json:"code"`
			Availability []struct {
				DatacenterId   string `json:"datacenterId"`
				DatacenterName string `json:"datacenterName"`
				DatacenterCode string `json:"datacenterCode"`
				GPUCounts      []struct {
					Count         int64 `json:"count"`
					AvailableSkus []struct {
						SkuCode     string `json:"skuCode"`
						Description string `json:"description"`
						Specs       struct {
							GPUDescription string `json:"gpuDescription"`
							VCPU           int64  `json:"vcpu"`
							Memory         int64  `json:"memoryGiB"`
							Storage        int64  `json:"storageGB"`
						} `json:"specs"`
					} `json:"availableSkus"`
					Specs struct {
						GPUDescription string `json:"gpuDescription"`
						VCPU           int64  `json:"vcpu"`
						Memory         int64  `json:"memoryGiB"`
						Storage        int64  `json:"storageGB"`
					} `json:"specs"`
				} `json:"gpuCounts"`
			} `json:"availability"`
		} `json:"series"`
	} `json:"data"`
}

// FlatInventory is one available SKU with its series and specs flattened out.
type FlatInventory struct {
	SeriesID   string
	SeriesName string
	SeriesCode string
	GPUCount   int64
	VCPU       int64
	MemoryGiB  int64
	StorageGB  int64
	SkuCode    string
}

// flattenInventory returns one entry per available SKU in the datacenter. When
// gpuCount is not zero it keeps only the SKUs with that count.
func flattenInventory(invResp *inventoryResp, datacenterId string, gpuCount int64) []FlatInventory {
	var inventory []FlatInventory
	for _, series := range invResp.Data.Series {
		for _, availability := range series.Availability {
			if availability.DatacenterId != datacenterId {
				continue
			}
			for _, count := range availability.GPUCounts {
				if gpuCount != 0 && count.Count != gpuCount {
					continue
				}
				for _, sku := range count.AvailableSkus {
					inventory = append(inventory, FlatInventory{
						SeriesID:   series.ID,
						SeriesName: series.Name,
						SeriesCode: series.Code,
						GPUCount:   count.Count,
						VCPU:       sku.Specs.VCPU,
						MemoryGiB:  sku.Specs.Memory,
						StorageGB:  sku.Specs.Storage,
						SkuCode:    sku.SkuCode,
					})
				}
			}
		}
	}
	return inventory
}

// getInventory sends the inventory GET and returns the parsed response. It adds
// only the filter query parameters that are set, so callers can omit any of them.
func getInventory(gpcnClient *client.GpcnClient, ctx context.Context, datacenterId, seriesCode string, gpuCount int64) (*inventoryResp, error) {
	u, err := url.Parse(BASE_URL_V1 + "inventory")
	if err != nil {
		return nil, fmt.Errorf("failed to parse inventory URL: %w", err)
	}
	q := u.Query()
	q.Add("datacenterId", datacenterId)
	if seriesCode != "" {
		q.Add("code", seriesCode)
	}
	if gpuCount != 0 {
		q.Add("count", strconv.FormatInt(gpuCount, 10))
	}
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

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}

	var invResp inventoryResp
	if err := json.Unmarshal(body, &invResp); err != nil {
		return nil, err
	}

	tflog.Info(ctx, LogSuccessfullyRetrievedInventory)
	return &invResp, nil
}

// CheckInventory confirms that at least one SKU is available for the series,
// datacenter, and GPU count in the model. It returns the flattened SKUs, which
// all share the series ID the caller needs.
func CheckInventory(gpcnClient *client.GpcnClient, ctx context.Context, model ResourceModel) ([]FlatInventory, error) {
	seriesCode := model.SeriesCode.ValueString()
	datacenterId := model.DatacenterId.ValueString()
	gpuCount := model.GPUCount.ValueInt64()

	tflog.Info(ctx, fmt.Sprintf(LogStartingCheckInventory, seriesCode, datacenterId, gpuCount))

	invResp, err := getInventory(gpcnClient, ctx, datacenterId, seriesCode, gpuCount)
	if err != nil {
		return nil, err
	}

	tflog.Info(ctx, LogValidatingInventoryResponseStructure)

	inventory := flattenInventory(invResp, datacenterId, gpuCount)
	if len(inventory) == 0 {
		return nil, fmt.Errorf(ErrDetailNoInventoryAvailable, seriesCode, datacenterId, gpuCount)
	}

	tflog.Info(ctx, fmt.Sprintf(LogInventoryAvailable, seriesCode, datacenterId, gpuCount))
	return inventory, nil
}

// FetchInventory lists every available SKU for the datacenter, filtered by the
// optional series code and GPU count. An empty result is not an error.
func FetchInventory(gpcnClient *client.GpcnClient, ctx context.Context, datacenterId, seriesCode string, gpuCount int64) ([]FlatInventory, error) {
	tflog.Info(ctx, fmt.Sprintf(LogStartingFetchInventory, datacenterId, seriesCode, gpuCount))

	invResp, err := getInventory(gpcnClient, ctx, datacenterId, seriesCode, gpuCount)
	if err != nil {
		return nil, err
	}

	inventory := flattenInventory(invResp, datacenterId, gpuCount)

	tflog.Info(ctx, fmt.Sprintf(LogSuccessfullyFetchedInventory, len(inventory), datacenterId))
	return inventory, nil
}

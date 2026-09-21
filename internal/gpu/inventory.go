package gpu

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

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
// seriesCode is not empty it keeps only that series, and when gpuCount is not
// zero it keeps only the SKUs with that count.
func flattenInventory(invResp *inventoryResp, datacenterId, seriesCode string, gpuCount int64) []FlatInventory {
	var inventory []FlatInventory
	for _, series := range invResp.Data.Series {
		if seriesCode != "" && series.Code != seriesCode {
			continue
		}
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

// getInventory sends the inventory GET and returns the parsed response. It asks
// for the whole datacenter. The series and count filters both prune the series
// list, and a refusal must tell the user which series the datacenter offers and
// whether the count is what is missing.
func getInventory(gpcnClient *client.GpcnClient, ctx context.Context, datacenterId string) (*inventoryResp, error) {
	u, err := url.Parse(BASE_URL_V1 + "inventory")
	if err != nil {
		return nil, fmt.Errorf("failed to parse inventory URL: %w", err)
	}
	q := u.Query()
	q.Add("datacenterId", datacenterId)
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

// resolveSeriesCodeFromInventory matches the requested series against the series
// the response lists. A configured code must be one of them, and a configured
// name resolves to that entry's code. The name comparison ignores case and
// surrounding space, because the catalog name is prose. A response with no
// series at all means the catalog is unreachable, so the name map answers
// instead and the caller reports the availability error.
func resolveSeriesCodeFromInventory(invResp *inventoryResp, datacenterId, seriesCode, seriesName string) (string, error) {
	if len(invResp.Data.Series) == 0 {
		if seriesCode != "" {
			return seriesCode, nil
		}
		return GPUSeriesNameToCode[strings.TrimSpace(seriesName)], nil
	}

	if seriesCode != "" {
		for _, series := range invResp.Data.Series {
			if series.Code == seriesCode {
				return series.Code, nil
			}
		}
		return "", fmt.Errorf(ErrDetailUnknownGPUSeries, seriesCode, datacenterId, offeredSeries(invResp))
	}

	if seriesName == "" {
		return "", nil
	}

	wanted := strings.TrimSpace(seriesName)
	for _, series := range invResp.Data.Series {
		if strings.EqualFold(strings.TrimSpace(series.Name), wanted) {
			return series.Code, nil
		}
	}
	return "", fmt.Errorf(ErrDetailUnknownGPUSeries, seriesName, datacenterId, offeredSeries(invResp))
}

// offeredSeries renders the series in the response as "code (name)" pairs.
func offeredSeries(invResp *inventoryResp) string {
	pairs := make([]string, 0, len(invResp.Data.Series))
	for _, series := range invResp.Data.Series {
		pairs = append(pairs, fmt.Sprintf("%s (%s)", series.Code, series.Name))
	}
	return strings.Join(pairs, ", ")
}

// CheckInventory confirms that at least one SKU is available for the series,
// datacenter, and GPU count in the model. It returns the flattened SKUs, which
// all share the series ID the caller needs, and the resolved series code.
func CheckInventory(gpcnClient *client.GpcnClient, ctx context.Context, model ResourceModel) ([]FlatInventory, string, error) {
	datacenterId := model.DatacenterId.ValueString()
	gpuCount := model.GPUCount.ValueInt64()

	tflog.Info(ctx, fmt.Sprintf(LogStartingCheckInventory, model.SeriesCode.ValueString(), datacenterId, gpuCount))

	invResp, err := getInventory(gpcnClient, ctx, datacenterId)
	if err != nil {
		return nil, "", err
	}

	tflog.Info(ctx, LogValidatingInventoryResponseStructure)

	seriesCode, err := resolveSeriesCodeFromInventory(invResp, datacenterId, model.SeriesCode.ValueString(), model.SeriesName.ValueString())
	if err != nil {
		return nil, "", err
	}

	inventory := flattenInventory(invResp, datacenterId, seriesCode, gpuCount)
	if len(inventory) == 0 {
		return nil, "", fmt.Errorf(ErrDetailNoInventoryAvailable, seriesCode, datacenterId, gpuCount)
	}

	tflog.Info(ctx, fmt.Sprintf(LogInventoryAvailable, seriesCode, datacenterId, gpuCount))
	return inventory, seriesCode, nil
}

// FetchInventory lists every available SKU for the datacenter, filtered by the
// optional series and GPU count. A series the datacenter does not offer is an
// error, but an offered series with nothing available is an empty result.
func FetchInventory(gpcnClient *client.GpcnClient, ctx context.Context, datacenterId, seriesCode, seriesName string, gpuCount int64) ([]FlatInventory, error) {
	tflog.Info(ctx, fmt.Sprintf(LogStartingFetchInventory, datacenterId, seriesCode, gpuCount))

	invResp, err := getInventory(gpcnClient, ctx, datacenterId)
	if err != nil {
		return nil, err
	}

	resolvedCode, err := resolveSeriesCodeFromInventory(invResp, datacenterId, seriesCode, seriesName)
	if err != nil {
		return nil, err
	}

	inventory := flattenInventory(invResp, datacenterId, resolvedCode, gpuCount)

	tflog.Info(ctx, fmt.Sprintf(LogSuccessfullyFetchedInventory, len(inventory), datacenterId))
	return inventory, nil
}

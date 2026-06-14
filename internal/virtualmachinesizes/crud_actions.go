package virtualmachinesizes

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"terraform-provider-gpcn/internal/client"

	"github.com/hashicorp/terraform-plugin-log/tflog"
)

type sizesAPIResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Data    struct {
		DatacenterId string                `json:"datacenterId"`
		Categories   []categoryAPIResponse `json:"categories"`
	} `json:"data"`
}

type categoryAPIResponse struct {
	Code  string            `json:"code"`
	Name  string            `json:"name"`
	Sizes []sizeAPIResponse `json:"sizes"`
}

type sizeAPIResponse struct {
	SkuId   string `json:"skuId"`
	Name    string `json:"name"`
	SkuCode string `json:"skuCode"`
	CPU     int64  `json:"cpu"`
	RAM     int64  `json:"ram"`
	Disk    int64  `json:"disk"`
}

// FlatSize is a single size with its parent category included.
type FlatSize struct {
	ID        string
	Name      string
	SkuCode   string
	Category  string
	CPU       int64
	MemoryGB  int64
	StorageGB int64
}

// FetchSizes retrieves all virtual machine sizes for a datacenter. When vmId is
// non-empty the API returns only sizes that are valid in-place upgrade targets for
// that VM.
func FetchSizes(gpcnClient *client.GpcnClient, ctx context.Context, datacenterId, vmId string) ([]FlatSize, error) {
	tflog.Info(ctx, fmt.Sprintf(LogStartingFetchVMSizes, datacenterId))

	url := DATA_CENTERS_BASE_URL_V1 + datacenterId + "/virtual-machine-sizes"
	request, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	if vmId != "" {
		q := request.URL.Query()
		q.Set("vmId", vmId)
		request.URL.RawQuery = q.Encode()
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

	var apiResp sizesAPIResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return nil, err
	}

	var sizes []FlatSize
	for _, cat := range apiResp.Data.Categories {
		for _, s := range cat.Sizes {
			sizes = append(sizes, FlatSize{
				ID:        s.SkuId,
				Name:      s.Name,
				SkuCode:   s.SkuCode,
				Category:  cat.Code,
				CPU:       s.CPU,
				MemoryGB:  s.RAM,
				StorageGB: s.Disk,
			})
		}
	}

	tflog.Info(ctx, fmt.Sprintf(LogSuccessfullyFetchedSizes, len(sizes), datacenterId))
	return sizes, nil
}

// FilterSizes applies optional category (exact code match) and min-spec filters.
func FilterSizes(ctx context.Context, sizes []FlatSize, category string, minCPU, minMemoryGB, minStorageGB int64) []FlatSize {
	result := sizes

	if category != "" {
		tflog.Info(ctx, fmt.Sprintf(LogApplyingCategoryFilter, category))
		var filtered []FlatSize
		for _, s := range result {
			if s.Category == category {
				filtered = append(filtered, s)
			}
		}
		result = filtered
	}

	result = filterByMinCPU(ctx, result, minCPU)
	result = filterByMinMemoryGB(ctx, result, minMemoryGB)
	result = filterByMinStorageGB(ctx, result, minStorageGB)

	tflog.Info(ctx, fmt.Sprintf(LogSizesAfterFiltering, len(result)))
	return result
}

func filterByMinCPU(ctx context.Context, sizes []FlatSize, minCPU int64) []FlatSize {
	if minCPU == 0 {
		return sizes
	}
	tflog.Info(ctx, fmt.Sprintf(LogApplyingMinCPUFilter, minCPU))
	var result []FlatSize
	for _, s := range sizes {
		if s.CPU >= minCPU {
			result = append(result, s)
		}
	}
	return result
}

func filterByMinMemoryGB(ctx context.Context, sizes []FlatSize, minMemoryGB int64) []FlatSize {
	if minMemoryGB == 0 {
		return sizes
	}
	tflog.Info(ctx, fmt.Sprintf(LogApplyingMinMemoryFilter, minMemoryGB))
	var result []FlatSize
	for _, s := range sizes {
		if s.MemoryGB >= minMemoryGB {
			result = append(result, s)
		}
	}
	return result
}

func filterByMinStorageGB(ctx context.Context, sizes []FlatSize, minStorageGB int64) []FlatSize {
	if minStorageGB == 0 {
		return sizes
	}
	tflog.Info(ctx, fmt.Sprintf(LogApplyingMinStorageFilter, minStorageGB))
	var result []FlatSize
	for _, s := range sizes {
		if s.StorageGB >= minStorageGB {
			result = append(result, s)
		}
	}
	return result
}

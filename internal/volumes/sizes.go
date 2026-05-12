package volumes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"terraform-provider-gpcn/internal/client"

	"github.com/hashicorp/terraform-plugin-log/tflog"
)

type volumeSizesResponse struct {
	Success bool                    `json:"success"`
	Message string                  `json:"message"`
	Data    volumeSizesDataResponse `json:"data"`
}
type volumeSizesDataResponse struct {
	DatacenterId string                               `json:"datacenterId"`
	VolumeTypes  []volumeSizesDataVolumeTypesResponse `json:"volumeTypes"`
}
type volumeSizesDataVolumeTypesResponse struct {
	ComponentCode  string                                             `json:"componentCode"`
	AvailableSizes []volumeSizesDataVolumeTypesAvailableSizesResponse `json:"availableSizes"`
}
type volumeSizesDataVolumeTypesAvailableSizesResponse struct {
	SkuId  string `json:"skuId"`
	SizeGb int64  `json:"sizeGb"`
}

// GetVolumeSkuId looks up the skuId for a given datacenterId, componentCode, and sizeGb
func GetVolumeSkuId(gpcnClient *client.GpcnClient, ctx context.Context, datacenterId, componentCode string, sizeGb int64) (string, error) {
	tflog.Info(ctx, fmt.Sprintf(LogStartingGetVolumeSkuIdWithParams, componentCode, strconv.FormatInt(sizeGb, 10)))
	request, err := http.NewRequestWithContext(ctx, "GET", DATA_CENTERS_BASE_URL_V1+datacenterId+"/volume-sizes", nil)
	if err != nil {
		return "", err
	}

	response, err := gpcnClient.DoWithRetry(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return "", err
	}

	var volSizesResp volumeSizesResponse
	err = json.Unmarshal(body, &volSizesResp)

	if err != nil {
		return "", err
	}

	tflog.Info(ctx, LogValidatingVolumeTypeAvailable)
	// Verify the volumeType specified is available
	typeIdx := slices.IndexFunc(volSizesResp.Data.VolumeTypes, func(volumeType volumeSizesDataVolumeTypesResponse) bool {
		return volumeType.ComponentCode == componentCode
	})
	if typeIdx < 0 {
		var codes []string
		for _, volumeType := range volSizesResp.Data.VolumeTypes {
			codes = append(codes, volumeType.ComponentCode)
		}
		codesFormatted := strings.Join(codes, ", ")
		return "", errors.New("the specified volume type is not available for this datacenter. Valid component codes are: " + codesFormatted)
	}

	tflog.Info(ctx, LogValidatingVolumeSizeAvailable)
	// Verify the size is available
	sizeIdx := slices.IndexFunc(volSizesResp.Data.VolumeTypes[typeIdx].AvailableSizes, func(availableSize volumeSizesDataVolumeTypesAvailableSizesResponse) bool {
		return availableSize.SizeGb == sizeGb
	})
	if sizeIdx < 0 {
		var sizes []string
		for _, size := range volSizesResp.Data.VolumeTypes[typeIdx].AvailableSizes {
			sizes = append(sizes, strconv.FormatInt(size.SizeGb, 10))
		}
		sizesFormatted := strings.Join(sizes, ", ")
		return "", errors.New("the specified volume size is not available for this datacenter. Valid sizes are (in GB): " + sizesFormatted)
	}

	tflog.Info(ctx, fmt.Sprintf(LogSuccessfullyRetrievedVolumeSkuIdWithParams, componentCode, strconv.FormatInt(sizeGb, 10)))
	return volSizesResp.Data.VolumeTypes[typeIdx].AvailableSizes[sizeIdx].SkuId, nil
}

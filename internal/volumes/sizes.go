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

	"github.com/hashicorp/terraform-plugin-log/tflog"
)

type volumeSizesResponse struct {
	Success bool                    `json:"success"`
	Message string                  `json:"message"`
	Data    volumeSizesDataResponse `json:"data"`
}
type volumeSizesDataResponse struct {
	DatacenterId string                               `json:"datacenterid"`
	VolumeTypes  []volumeSizesDataVolumeTypesResponse `json:"volumeTypes"`
}
type volumeSizesDataVolumeTypesResponse struct {
	ID             int64                                              `json:"id"`
	Name           string                                             `json:"name"`
	Description    string                                             `json:"description"`
	AvailableSizes []volumeSizesDataVolumeTypesAvailableSizesResponse `json:"availableSizes"`
}
type volumeSizesDataVolumeTypesAvailableSizesResponse struct {
	ID     int64 `json:"id"`
	SizeGb int64 `json:"sizeGb"`
}

// Get volume size Id for a given datacenterId and volume type and verify typeId and sizeGb are valid
func GetVolumeSizeId(httpClient *http.Client, ctx context.Context, datacenterId string, volumeTypeId, sizeGb int64) (int64, error) {
	tflog.Info(ctx, fmt.Sprintf(LogStartingGetVolumeSizeIDWithParams, strconv.FormatInt(volumeTypeId, 10), strconv.FormatInt(sizeGb, 10)))
	request, err := http.NewRequest("GET", "/resource/data-centers/"+datacenterId+"/volume-sizes", nil)
	if err != nil {
		return -1, err
	}

	response, err := httpClient.Do(request)
	if err != nil {
		return -1, err
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return -1, err
	}

	var volumeSizesResponse volumeSizesResponse
	err = json.Unmarshal(body, &volumeSizesResponse)

	if err != nil {
		return -1, err
	}

	tflog.Info(ctx, LogValidatingVolumeTypeAvailable)
	// Verify the volumeType specified is available
	typeIdx := slices.IndexFunc(volumeSizesResponse.Data.VolumeTypes, func(volumeType volumeSizesDataVolumeTypesResponse) bool {
		return volumeType.ID == volumeTypeId
	})
	if typeIdx < 0 {
		var volumeTypes []string
		for _, volumeType := range volumeSizesResponse.Data.VolumeTypes {
			volumeTypes = append(volumeTypes, volumeType.Name)
		}
		volumeTypesFormatted := strings.Join(volumeTypes, ", ")
		return -1, errors.New("the specified volume type is not available for this datacenter. Valid types are: " + volumeTypesFormatted)
	}

	tflog.Info(ctx, LogValidatingVolumeSizeAvailable)
	// Verify the size is available
	sizeIdx := slices.IndexFunc(volumeSizesResponse.Data.VolumeTypes[typeIdx].AvailableSizes, func(availableSize volumeSizesDataVolumeTypesAvailableSizesResponse) bool {
		return availableSize.SizeGb == sizeGb
	})
	if sizeIdx < 0 {
		var sizes []string
		for _, size := range volumeSizesResponse.Data.VolumeTypes[typeIdx].AvailableSizes {
			sizes = append(sizes, strconv.FormatInt(size.SizeGb, 10))
		}
		sizesFormatted := strings.Join(sizes, ", ")
		return -1, errors.New("the specified volume size is not available for this datacenter. Valid sizes are (in GB): " + sizesFormatted)
	}

	// If both are available, we can use the Id
	tflog.Info(ctx, fmt.Sprintf(LogSuccessfullyRetrievedVolumeSizeIDWithParams, strconv.FormatInt(volumeTypeId, 10), strconv.FormatInt(sizeGb, 10)))
	return volumeSizesResponse.Data.VolumeTypes[typeIdx].AvailableSizes[sizeIdx].ID, nil
}

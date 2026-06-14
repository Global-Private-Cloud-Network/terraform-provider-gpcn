package virtualmachineimages

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"terraform-provider-gpcn/internal/client"

	"github.com/hashicorp/terraform-plugin-log/tflog"
)

type VMImagesAPIResponse struct {
	Success bool                          `json:"success"`
	Message string                        `json:"message"`
	Data    []VMImagesCategoryAPIResponse `json:"data"`
}

type VMImagesCategoryAPIResponse struct {
	ID        int64                `json:"id"`
	Name      string               `json:"name"`
	SortOrder int                  `json:"sortOrder"`
	Images    []VMImageAPIResponse `json:"images"`
}

type VMImageAPIResponse struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// FlatImage is a single image with its parent category name included.
type FlatImage struct {
	ID        string
	Name      string
	ImageType string
}

// Retrieve all virtual machine images for a given datacenter and flattens to include category
func FetchImages(gpcnClient *client.GpcnClient, ctx context.Context, datacenterId string) ([]FlatImage, error) {
	tflog.Info(ctx, fmt.Sprintf(LogStartingFetchVMImages, datacenterId))

	request, err := http.NewRequestWithContext(ctx, "GET", DATA_CENTERS_IMAGES_URL_V1+datacenterId+"/virtual-machine-images", nil)
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

	var apiResp VMImagesAPIResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return nil, err
	}

	var images []FlatImage
	for _, category := range apiResp.Data {
		for _, img := range category.Images {
			images = append(images, FlatImage{
				ID:        img.ID,
				Name:      img.Name,
				ImageType: category.Name,
			})
		}
	}

	tflog.Info(ctx, fmt.Sprintf(LogSuccessfullyFetchedImages, len(images), datacenterId))
	return images, nil
}

// FilterImages applies optional image_type (exact) and image_name (case-insensitive contains) filters.
func FilterImages(ctx context.Context, images []FlatImage, imageType, imageName string) []FlatImage {
	result := images

	if imageType != "" {
		tflog.Info(ctx, fmt.Sprintf(LogApplyingImageTypeFilter, imageType))
		var filtered []FlatImage
		for _, img := range result {
			if img.ImageType == imageType {
				filtered = append(filtered, img)
			}
		}
		result = filtered
	}

	if imageName != "" {
		tflog.Info(ctx, fmt.Sprintf(LogApplyingImageNameFilter, imageName))
		lower := strings.ToLower(imageName)
		var filtered []FlatImage
		for _, img := range result {
			if strings.Contains(strings.ToLower(img.Name), lower) {
				filtered = append(filtered, img)
			}
		}
		result = filtered
	}

	tflog.Info(ctx, fmt.Sprintf(LogImagesAfterFiltering, len(result)))
	return result
}

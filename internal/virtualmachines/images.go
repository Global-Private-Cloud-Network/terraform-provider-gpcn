package virtualmachines

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"

	"terraform-provider-gpcn/internal/client"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

type virtualMachineImagesResponse struct {
	Success bool                                       `json:"success"`
	Message string                                     `json:"message"`
	Data    []virtualMachineImagesCategoryDataResponse `json:"data"`
}
type virtualMachineImagesCategoryDataResponse struct {
	ID        int64                              `json:"id"`
	Name      string                             `json:"name"`
	SortOrder int                                `json:"sortOrder"`
	Images    []virtualMachineImagesDataResponse `json:"images"`
}
type virtualMachineImagesDataResponse struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
type VirtualMachineImagesDataResponseTF struct {
	ID   types.String `tfsdk:"id"`
	Name types.String `tfsdk:"name"`
}

func (o VirtualMachineImagesDataResponseTF) AttrTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"id":   types.StringType,
		"name": types.StringType,
	}
}

// Get virtual machine image ID for a given datacenterId and virtual machine image name
func GetVirtualMachineImageId(gpcnClient *client.GpcnClient, ctx context.Context, datacenterId, virtualMachineImageName string) (string, []VirtualMachineImagesDataResponseTF, error) {
	tflog.Info(ctx, fmt.Sprintf(LogStartingGetVMImageIDWithName, virtualMachineImageName))
	request, err := http.NewRequestWithContext(ctx, "GET", DATA_CENTERS_BASE_URL_V1+datacenterId+"/virtual-machine-images", nil)

	var images []VirtualMachineImagesDataResponseTF
	if err != nil {
		return "", images, err
	}

	response, err := gpcnClient.DoWithRetry(request)
	if err != nil {
		return "", images, err
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return "", images, err
	}

	var imagesResp virtualMachineImagesResponse
	err = json.Unmarshal(body, &imagesResp)

	if err != nil {
		return "", images, err
	}

	// Flatten all images across categories for name lookup
	var allImages []virtualMachineImagesDataResponse
	for _, category := range imagesResp.Data {
		allImages = append(allImages, category.Images...)
	}

	var names []string
	for _, image := range allImages {
		images = append(images, VirtualMachineImagesDataResponseTF{
			ID:   types.StringValue(image.ID),
			Name: types.StringValue(image.Name),
		})
		names = append(names, image.Name)
	}

	imageIdx := slices.IndexFunc(allImages, func(image virtualMachineImagesDataResponse) bool {
		return strings.EqualFold(image.Name, virtualMachineImageName)
	})

	if imageIdx < 0 {
		return "", images, errors.New("the image '" + virtualMachineImageName + "' is not available for this datacenter. Valid images are: " + strings.Join(names, ", "))
	}

	tflog.Info(ctx, fmt.Sprintf(LogSuccessfullyRetrievedVMImageIDWithName, virtualMachineImageName))
	return allImages[imageIdx].ID, images, nil
}

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

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

type virtualMachineImagesResponse struct {
	Success bool                               `json:"success"`
	Message string                             `json:"message"`
	Data    []virtualMachineImagesDataResponse `json:"data"`
}
type virtualMachineImagesDataResponse struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}
type VirtualMachineImagesDataResponseTF struct {
	ID   types.Int64  `tfsdk:"id"`
	Name types.String `tfsdk:"name"`
}

func (o VirtualMachineImagesDataResponseTF) AttrTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"id":   types.Int64Type,
		"name": types.StringType,
	}
}

// Get virtual machine image Id for a given datacenterId and virtual machine image name
func GetVirtualMachineImageId(client *http.Client, ctx context.Context, datacenterId, virtualMachineImageName string) (int64, []VirtualMachineImagesDataResponseTF, error) {
	tflog.Info(ctx, fmt.Sprintf(LogStartingGetVMImageIDWithName, virtualMachineImageName))
	request, err := http.NewRequest("GET", DATA_CENTERS_BASE_URL_V1+datacenterId+"/virtual-machine-images", nil)

	var images []VirtualMachineImagesDataResponseTF
	if err != nil {
		return -1, images, err
	}

	response, err := client.Do(request)
	if err != nil {
		return -1, images, err
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return -1, images, err
	}

	var virtualMachineImagesResponse virtualMachineImagesResponse
	err = json.Unmarshal(body, &virtualMachineImagesResponse)

	if err != nil {
		return -1, images, err
	}

	// Verify the image name specified is available
	imageIdx := slices.IndexFunc(virtualMachineImagesResponse.Data, func(virtualMachineImage virtualMachineImagesDataResponse) bool {
		return strings.EqualFold(virtualMachineImage.Name, virtualMachineImageName)
	})

	var names []string
	for _, image := range virtualMachineImagesResponse.Data {
		// Used for actual data
		images = append(images, VirtualMachineImagesDataResponseTF{
			ID:   types.Int64Value(image.ID),
			Name: types.StringValue(image.Name),
		})
		// Used for helpful error function if needed
		names = append(names, image.Name)
	}

	imageNamesFormatted := strings.Join(names, ", ")

	if imageIdx < 0 {
		return -1, images, errors.New("the image '" + virtualMachineImageName + "' is not available for this datacenter. Valid images are: " + imageNamesFormatted)
	}

	tflog.Info(ctx, fmt.Sprintf(LogSuccessfullyRetrievedVMImageIDWithName, virtualMachineImageName))
	return virtualMachineImagesResponse.Data[imageIdx].ID, images, nil
}

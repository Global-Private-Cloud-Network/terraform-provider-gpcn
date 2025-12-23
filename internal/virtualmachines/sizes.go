package virtualmachines

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

type virtualMachineSizesResponse struct {
	Success bool                              `json:"success"`
	Message string                            `json:"message"`
	Data    []virtualMachineSizesDataResponse `json:"data"`
}
type virtualMachineSizesDataResponse struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	CPU  int64  `json:"cpu"`
	RAM  int64  `json:"ram"`
	Disk int64  `json:"disk"`
}
type VirtualMachineSizesDataResponseTF struct {
	ID   types.Int64  `tfsdk:"id"`
	Name types.String `tfsdk:"name"`
	CPU  types.Int64  `tfsdk:"cpu"`
	RAM  types.Int64  `tfsdk:"ram"`
	Disk types.Int64  `tfsdk:"disk"`
}

func (o VirtualMachineSizesDataResponseTF) AttrTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"id":   types.Int64Type,
		"name": types.StringType,
		"cpu":  types.Int64Type,
		"ram":  types.Int64Type,
		"disk": types.Int64Type,
	}
}

// Get virtual machine size Id for a given datacenterId and virtual machine image name
func GetVirtualMachineSizeId(client *http.Client, ctx context.Context, imageId int64, datacenterId, virtualMachineSizeName string) (int64, []VirtualMachineSizesDataResponseTF, error) {
	tflog.Info(ctx, fmt.Sprintf(LogStartingGetVMSizeIDWithName, virtualMachineSizeName))
	request, err := http.NewRequest("GET", DATA_CENTERS_BASE_URL_V1+datacenterId+"/virtual-machine-sizes?imageId="+strconv.FormatInt(imageId, 10), nil)
	var sizes []VirtualMachineSizesDataResponseTF
	if err != nil {
		return -1, sizes, err
	}

	response, err := client.Do(request)
	if err != nil {
		return -1, sizes, err
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return -1, sizes, err
	}

	var virtualMachineSizesResponse virtualMachineSizesResponse
	err = json.Unmarshal(body, &virtualMachineSizesResponse)

	if err != nil {
		return -1, sizes, err
	}

	// Verify the size specified is available
	sizeIdx := slices.IndexFunc(virtualMachineSizesResponse.Data, func(virtualMachineSize virtualMachineSizesDataResponse) bool {
		return strings.EqualFold(virtualMachineSize.Name, virtualMachineSizeName)
	})

	var names []string
	for _, size := range virtualMachineSizesResponse.Data {
		sizes = append(sizes, VirtualMachineSizesDataResponseTF{
			ID:   types.Int64Value(size.ID),
			Name: types.StringValue(size.Name),
			CPU:  types.Int64Value(size.CPU),
			RAM:  types.Int64Value(size.RAM),
			Disk: types.Int64Value(size.Disk),
		})
		names = append(names, size.Name)
	}
	sizesFormatted := strings.Join(names, ", ")

	if sizeIdx < 0 {
		return -1, sizes, errors.New("the size '" + virtualMachineSizeName + "' is not available for this datacenter. Valid sizes are: " + sizesFormatted)
	}

	tflog.Info(ctx, fmt.Sprintf(LogSuccessfullyRetrievedVMSizeIDWithName, virtualMachineSizeName))
	return virtualMachineSizesResponse.Data[sizeIdx].ID, sizes, nil
}

// Helper function to update a VM by Id
func UpdateVirtualMachineSize(client *http.Client, ctx context.Context, virtualMachineId string, sizeId int64) error {
	tflog.Info(ctx, fmt.Sprintf(LogStartingUpdateVMSizeWithID, virtualMachineId))
	// Create a new request from the plan
	updateVMRequestBody := map[string]any{
		"configurationId": sizeId,
	}

	jsonUpdateVMRequestBody, err := json.Marshal(updateVMRequestBody)
	if err != nil {
		return err
	}
	request, err := http.NewRequest("PUT", BASE_URL_V1+virtualMachineId+"/size", bytes.NewBuffer(jsonUpdateVMRequestBody))
	if err != nil {
		return err
	}

	_, err = client.Do(request)
	if err != nil {
		return err
	}

	tflog.Info(ctx, LogSuccessfullyUpdatedVMSize)
	return nil
}

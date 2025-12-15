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
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

type virtualMachineSizesResponse struct {
	Success bool                            `json:"success"`
	Message string                          `json:"message"`
	Data    virtualMachineSizesDataResponse `json:"data"`
}
type virtualMachineSizesDataResponse struct {
	DatacenterId string                                      `json:"datacenterId"`
	Categories   []virtualMachineSizesDataCategoriesResponse `json:"categories"`
}
type virtualMachineSizesDataCategoriesResponse struct {
	ID    int64                                            `json:"id"`
	Name  string                                           `json:"name"`
	Code  string                                           `json:"code"`
	Tiers []virtualMachineSizesDataCategoriesTiersResponse `json:"tiers"`
}
type virtualMachineSizesDataCategoriesTiersResponse struct {
	ID             int64                                                          `json:"id"`
	Name           string                                                         `json:"name"`
	Code           string                                                         `json:"code"`
	Configurations []virtualMachineSizesDataCategoriesTiersConfigurationsResponse `json:"configurations"`
}
type virtualMachineSizesDataCategoriesTiersConfigurationsResponse struct {
	ConfigurationID int64                                                              `json:"configurationId"`
	CPU             virtualMachineSizesDataCategoriesTiersConfigurationsCPUResponse    `json:"cpu"`
	Memory          virtualMachineSizesDataCategoriesTiersConfigurationsMemoryResponse `json:"memory"`
	Disk            virtualMachineSizesDataCategoriesTiersConfigurationsDiskResponse   `json:"disk"`
}
type virtualMachineSizesDataCategoriesTiersConfigurationsCPUResponse struct {
	ID          int64  `json:"id"`
	Cores       int64  `json:"cores"`
	DisplayName string `json:"displayName"`
}
type virtualMachineSizesDataCategoriesTiersConfigurationsMemoryResponse struct {
	ID          int64  `json:"id"`
	SizeGb      int64  `json:"sizeGb"`
	DisplayName string `json:"displayName"`
}
type virtualMachineSizesDataCategoriesTiersConfigurationsDiskResponse struct {
	ID          int64  `json:"id"`
	SizeGb      int64  `json:"sizeGb"`
	DisplayName string `json:"displayName"`
}
type VirtualMachineConfigurationsTF struct {
	ID           types.Int64  `tfsdk:"id"`
	Category     types.String `tfsdk:"category"`
	CategoryCode types.String `tfsdk:"category_code"`
	Name         types.String `tfsdk:"name"`
	Code         types.String `tfsdk:"code"`
	CPUCores     types.Int64  `tfsdk:"cpu"`
	MemorySizeGB types.Int64  `tfsdk:"memory"`
	DiskSizeGB   types.Int64  `tfsdk:"disk"`
}

func (o VirtualMachineConfigurationsTF) AttrTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"id":            types.Int64Type,
		"category":      types.StringType,
		"category_code": types.StringType,
		"name":          types.StringType,
		"code":          types.StringType,
		"cpu":           types.Int64Type,
		"memory":        types.Int64Type,
		"disk":          types.Int64Type,
	}
}

// Get virtual machine size Id for a given datacenterId
func GetVirtualMachineSizeConfigurationId(client *http.Client, ctx context.Context, datacenterId, virtualMachineSizeName string) (int64, []VirtualMachineConfigurationsTF, error) {
	tflog.Info(ctx, fmt.Sprintf(LogStartingGetVMSizeIDWithName, virtualMachineSizeName))
	request, err := http.NewRequest("GET", "/resource/data-centers/"+datacenterId+"/virtual-machine-sizes", nil)
	var sizes []VirtualMachineConfigurationsTF
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
	// TODO: Get questions answered to also iterate through categories or use an input param
	tierIdx := slices.IndexFunc(virtualMachineSizesResponse.Data.Categories[0].Tiers, func(tier virtualMachineSizesDataCategoriesTiersResponse) bool {
		return strings.EqualFold(tier.Name, virtualMachineSizeName)
	})

	var names []string
	for _, category := range virtualMachineSizesResponse.Data.Categories {
		for _, tier := range category.Tiers {
			for _, configuration := range tier.Configurations {
				sizes = append(sizes, VirtualMachineConfigurationsTF{
					ID:           types.Int64Value(configuration.ConfigurationID),
					Category:     types.StringValue(category.Name),
					CategoryCode: types.StringValue(category.Code),
					Name:         types.StringValue(tier.Name),
					Code:         types.StringValue(tier.Code),
					CPUCores:     types.Int64Value(configuration.CPU.Cores),
					MemorySizeGB: types.Int64Value(configuration.Memory.SizeGb),
					DiskSizeGB:   types.Int64Value(configuration.Disk.SizeGb),
				})
				names = append(names, category.Name+" - "+tier.Name)
			}
		}
	}
	sizesFormatted := strings.Join(names, ", ")

	if tierIdx < 0 {
		return -1, sizes, errors.New("the size '" + virtualMachineSizeName + "' is not available for this datacenter. Valid sizes are: " + sizesFormatted)
	}

	tflog.Info(ctx, fmt.Sprintf(LogSuccessfullyRetrievedVMSizeIDWithName, virtualMachineSizeName))
	return virtualMachineSizesResponse.Data.Categories[0].Tiers[tierIdx].Configurations[0].ConfigurationID, sizes, nil
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
	request, err := http.NewRequest("PUT", BASE_URL+virtualMachineId+"/size", bytes.NewBuffer(jsonUpdateVMRequestBody))
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

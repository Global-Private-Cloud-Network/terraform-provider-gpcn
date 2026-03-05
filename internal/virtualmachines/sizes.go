package virtualmachines

import (
	"bytes"
	"context"
	"encoding/json"
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
func GetVirtualMachineSizeConfigurationId(gpcnClient *client.GpcnClient, ctx context.Context, datacenterId, virtualMachineSizeTierCode string) (int64, []VirtualMachineConfigurationsTF, error) {
	tflog.Info(ctx, fmt.Sprintf(LogStartingGetVMSizeIDWithName, virtualMachineSizeTierCode))
	request, err := http.NewRequestWithContext(ctx, "GET", DATA_CENTERS_BASE_URL_V1+datacenterId+"/virtual-machine-sizes", nil)
	var sizes []VirtualMachineConfigurationsTF
	if err != nil {
		return -1, sizes, err
	}

	response, err := gpcnClient.DoWithRetry(request)
	if err != nil {
		return -1, sizes, err
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return -1, sizes, err
	}
	_ = response.Body.Close()

	var sizesResp virtualMachineSizesResponse
	err = json.Unmarshal(body, &sizesResp)

	if err != nil {
		return -1, sizes, err
	}

	// Collect all names for error handling later
	var names []string
	for _, category := range sizesResp.Data.Categories {
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
				names = append(names, category.Code+" - "+tier.Code)
			}
		}
	}
	sizesFormatted := strings.Join(names, ", ")

	// Verify the size specified is available
	tierIdx := slices.IndexFunc(sizes, func(size VirtualMachineConfigurationsTF) bool {
		return strings.EqualFold(size.Code.ValueString(), virtualMachineSizeTierCode)
	})

	if tierIdx < 0 {
		return -1, sizes, fmt.Errorf(ErrDetailSizeNotAvailableForDatacenterNoCategory, virtualMachineSizeTierCode, sizesFormatted)
	}

	tflog.Info(ctx, fmt.Sprintf(LogSuccessfullyRetrievedVMSizeIDWithName, virtualMachineSizeTierCode))
	return sizes[tierIdx].ID.ValueInt64(), sizes, nil
}

// Helper function to update a VM by ID
func UpdateVirtualMachineSize(gpcnClient *client.GpcnClient, ctx context.Context, virtualMachineId string, sizeId int64) error {
	tflog.Info(ctx, fmt.Sprintf(LogStartingUpdateVMSizeWithID, virtualMachineId))
	// Create a new request from the plan
	updateVMRequestBody := map[string]any{
		"configurationId": sizeId,
	}

	jsonUpdateVMRequestBody, err := json.Marshal(updateVMRequestBody)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, "PUT", BASE_URL_V1+virtualMachineId+"/size", bytes.NewBuffer(jsonUpdateVMRequestBody))
	if err != nil {
		return err
	}

	response, err := gpcnClient.DoWithRetry(request)
	if err != nil {
		return err
	}

	// Read the response body and process it as updateVirtualMachineSizeResponse
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return err
	}
	_ = response.Body.Close()

	var updateVirtualMachineSizeResponse client.JobStatusSingularResponse
	err = json.Unmarshal(body, &updateVirtualMachineSizeResponse)

	if err != nil {
		return err
	}

	_, err = client.PerformLongPolling(gpcnClient, ctx, "Update Virtual Machine Size", updateVirtualMachineSizeResponse.Data.JobID)

	if err != nil {
		return err
	}

	tflog.Info(ctx, LogSuccessfullyUpdatedVMSize)
	return nil
}

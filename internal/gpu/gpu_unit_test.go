package gpu

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"terraform-provider-gpcn/internal/client"
	"terraform-provider-gpcn/internal/testutil"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

const testDatacenterID = "datacenter-123"
const testImageName = "ubuntu-22.04"

const testSSHKeyID = "ssh-key-abc123"

func createTestGPUModel(name, seriesName, seriesCode, imageName string, gpuCount int64) ResourceModel {
	model := ResourceModel{
		Name:         types.StringValue(name),
		DatacenterId: types.StringValue(testDatacenterID),
		GPUCount:     types.Int64Value(gpuCount),
		ImageName:    types.StringValue(imageName),
	}
	if seriesName != "" {
		model.SeriesName = types.StringValue(seriesName)
	} else {
		model.SeriesName = types.StringNull()
	}
	if seriesCode != "" {
		model.SeriesCode = types.StringValue(seriesCode)
	} else {
		model.SeriesCode = types.StringNull()
	}
	auth := ResourceModelAuth{SshKeyId: types.StringValue(testSSHKeyID)}
	var diags diag.Diagnostics
	model.Auth, diags = types.ObjectValueFrom(context.Background(), auth.AttrTypes(), auth)
	if diags.HasError() {
		panic("failed to create auth object in test helper")
	}
	return model
}

func newGPUResponse(id, name string) *readGPUResponse {
	resp := &readGPUResponse{}
	resp.Data.ID = id
	resp.Data.Name = name
	resp.Data.CreatedAt = time.Now().Format(time.RFC3339)
	resp.Data.UpdatedAt = time.Now().Format(time.RFC3339)
	resp.Data.Status = "Running"
	resp.Data.IP = "192.168.1.100"
	resp.Data.Datacenter.ID = testDatacenterID
	resp.Data.Datacenter.Name = "US-East-1"
	resp.Data.Datacenter.Region = "East"
	resp.Data.Datacenter.CountryAbbr = "US"
	resp.Data.Datacenter.Country = "United States"
	return resp
}

func newInventoryResponse(seriesID, seriesCode, datacenterID string, gpuCount, availableCount int64) inventoryResp {
	// Create a slice of empty structs with length equal to availableCount
	availableSkus := make([]struct{}, availableCount)

	return inventoryResp{
		Data: []struct {
			ID           string `json:"id"`
			Name         string `json:"name"`
			Code         string `json:"code"`
			Description  string `json:"description"`
			Availability []struct {
				DatacenterId   string `json:"datacenterId"`
				DatacenterName string `json:"datacenterName"`
				DatacenterCode string `json:"datacenterCode"`
				GPUCounts      []struct {
					Count         int64      `json:"count"`
					AvailableSkus []struct{} `json:"availableSkus"`
					Specs         struct {
						GPUDescription string `json:"gpuDescription"`
						VCPU           int64  `json:"vcpu"`
						Memory         int64  `json:"memoryGiB"`
						Storage        int64  `json:"storageGB"`
					} `json:"specs"`
				} `json:"gpuCounts"`
			} `json:"availability"`
		}{
			{
				ID:          seriesID,
				Name:        "H100 Series",
				Code:        seriesCode,
				Description: "NVIDIA H100 GPU Series",
				Availability: []struct {
					DatacenterId   string `json:"datacenterId"`
					DatacenterName string `json:"datacenterName"`
					DatacenterCode string `json:"datacenterCode"`
					GPUCounts      []struct {
						Count         int64      `json:"count"`
						AvailableSkus []struct{} `json:"availableSkus"`
						Specs         struct {
							GPUDescription string `json:"gpuDescription"`
							VCPU           int64  `json:"vcpu"`
							Memory         int64  `json:"memoryGiB"`
							Storage        int64  `json:"storageGB"`
						} `json:"specs"`
					} `json:"gpuCounts"`
				}{
					{
						DatacenterId:   datacenterID,
						DatacenterName: "US-East-1",
						DatacenterCode: "us-east-1",
						GPUCounts: []struct {
							Count         int64      `json:"count"`
							AvailableSkus []struct{} `json:"availableSkus"`
							Specs         struct {
								GPUDescription string `json:"gpuDescription"`
								VCPU           int64  `json:"vcpu"`
								Memory         int64  `json:"memoryGiB"`
								Storage        int64  `json:"storageGB"`
							} `json:"specs"`
						}{
							{
								Count:         gpuCount,
								AvailableSkus: availableSkus,
								Specs: struct {
									GPUDescription string `json:"gpuDescription"`
									VCPU           int64  `json:"vcpu"`
									Memory         int64  `json:"memoryGiB"`
									Storage        int64  `json:"storageGB"`
								}{
									GPUDescription: "NVIDIA H100 80GB",
									VCPU:           32,
									Memory:         256,
									Storage:        1000,
								},
							},
						},
					},
				},
			},
		},
	}
}

func TestMapGPUResponseToModelUnit(t *testing.T) {
	response := newGPUResponse("gpu-123", "test-gpu")
	model := createTestGPUModel("test-gpu", "H100 Series", "h100_series", testImageName, 2)

	result := MapGPUResponseToModel(context.Background(), response, model)

	if result.ID.ValueString() != "gpu-123" {
		t.Errorf("Expected ID 'gpu-123', got '%s'", result.ID.ValueString())
	}
	if result.Name.ValueString() != "test-gpu" {
		t.Errorf("Expected Name 'test-gpu', got '%s'", result.Name.ValueString())
	}
	if result.CreatedTime.IsNull() || result.CreatedTime.ValueString() == "unknown" {
		t.Errorf("Expected CreatedTime to be set, got '%s'", result.CreatedTime.ValueString())
	}
	if result.LastUpdated.IsNull() || result.LastUpdated.ValueString() == "unknown" {
		t.Errorf("Expected LastUpdated to be set, got '%s'", result.LastUpdated.ValueString())
	}
	if result.Location.IsNull() {
		t.Error("Expected Location to be set")
	}

	locationMap := make(map[string]string)
	_ = result.Location.ElementsAs(context.Background(), &locationMap, false)
	if locationMap["country"] != "United States" {
		t.Errorf("Expected country 'United States', got '%s'", locationMap["country"])
	}
	if locationMap["region"] != "East" {
		t.Errorf("Expected region 'East', got '%s'", locationMap["region"])
	}
	if locationMap["datacenter"] != "US-East-1" {
		t.Errorf("Expected datacenter 'US-East-1', got '%s'", locationMap["datacenter"])
	}
}

func TestCheckInventoryMockHTTP(t *testing.T) {
	const (
		seriesCode = "h100_series"
		gpuCount   = int64(2)
	)

	var inventoryCalled bool

	server, gpcnClient := testutil.SetupMockServerWithGpcnClient(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "GET" && strings.Contains(r.URL.Path, "/gpu/inventory") {
				inventoryCalled = true
				query := r.URL.Query()
				if query.Get("code") != seriesCode {
					t.Errorf("Expected code '%s', got '%s'", seriesCode, query.Get("code"))
				}
				if query.Get("datacenterId") != testDatacenterID {
					t.Errorf("Expected datacenterId '%s', got '%s'", testDatacenterID, query.Get("datacenterId"))
				}
				if query.Get("count") != "2" {
					t.Errorf("Expected count '2', got '%s'", query.Get("count"))
				}
				testutil.WriteJSONResponse(w, newInventoryResponse("series-123", seriesCode, testDatacenterID, gpuCount, 5))
			} else {
				testutil.LogUnexpectedRequest(t, w, r)
			}
		},
	})
	defer server.Close()

	model := createTestGPUModel("test-gpu", "", seriesCode, testImageName, gpuCount)

	response, err := CheckInventory(gpcnClient, context.Background(), model)
	if err != nil {
		t.Fatalf("CheckInventory failed: %v", err)
	}
	if response == nil {
		t.Fatal("Expected response, got nil")
		return
	}
	if !inventoryCalled {
		t.Error("Expected inventory endpoint to be called")
	}
	if len(response.Data) == 0 {
		t.Fatal("Expected inventory data, got empty array")
	}
	if response.Data[0].ID != "series-123" {
		t.Errorf("Expected series ID 'series-123', got '%s'", response.Data[0].ID)
	}
	if len(response.Data[0].Availability[0].GPUCounts[0].AvailableSkus) != 5 {
		t.Errorf("Expected 5 available SKUs, got %d", len(response.Data[0].Availability[0].GPUCounts[0].AvailableSkus))
	}
}

func TestCheckInventoryNoAvailabilityMockHTTP(t *testing.T) {
	const (
		seriesCode = "h100_series"
		gpuCount   = int64(2)
	)

	server, gpcnClient := testutil.SetupMockServerWithGpcnClient(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "GET" && strings.Contains(r.URL.Path, "/gpu/inventory") {
				testutil.WriteJSONResponse(w, newInventoryResponse("series-123", seriesCode, testDatacenterID, gpuCount, 0))
			}
		},
	})
	defer server.Close()

	model := createTestGPUModel("test-gpu", "", seriesCode, testImageName, gpuCount)

	_, err := CheckInventory(gpcnClient, context.Background(), model)
	if err == nil {
		t.Fatal("Expected error for no availability, got nil")
	}
	if !strings.Contains(err.Error(), "no GPU availability") {
		t.Errorf("Expected error to contain 'no GPU availability', got '%s'", err.Error())
	}
}

func TestCreateGPUMockHTTP(t *testing.T) {
	const (
		jobID    = "job-123"
		gpuID    = "gpu-456"
		seriesID = "series-789"
	)

	var createCalled, jobStatusCalled, gpuStatusCalled bool

	server, gpcnClient := testutil.SetupMockServerWithGpcnClient(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/gpu/"):
				createCalled = true
				body, _ := io.ReadAll(r.Body)
				var req map[string]any
				_ = json.Unmarshal(body, &req)

				if req["name"] != "test-gpu" {
					t.Errorf("Expected name 'test-gpu', got '%v'", req["name"])
				}
				if req["seriesId"] != seriesID {
					t.Errorf("Expected seriesId '%s', got '%v'", seriesID, req["seriesId"])
				}
				if int64(req["gpuCount"].(float64)) != 2 {
					t.Errorf("Expected gpuCount 2, got '%v'", req["gpuCount"])
				}
				if req["datacenterId"] != testDatacenterID {
					t.Errorf("Expected datacenterId '%s', got '%v'", testDatacenterID, req["datacenterId"])
				}
				if req["imageName"] != testImageName {
					t.Errorf("Expected imageName '%s', got '%v'", testImageName, req["imageName"])
				}
				if req["sshKeyId"] != testSSHKeyID {
					t.Errorf("Expected sshKeyId '%s', got '%v'", testSSHKeyID, req["sshKeyId"])
				}

				testutil.WriteJSONResponse(w, client.JobStatusMultiResponse{
					Success: true,
					Message: "GPU creation job started",
					Data: client.JobStatusDataResponse{
						Jobs: []client.JobResponse{{JobID: jobID, ResourceId: gpuID}},
					},
				})

			case r.Method == "POST" && strings.Contains(r.URL.Path, "/jobs"):
				jobStatusCalled = true
				testutil.HandleJobResponse(w, jobID, gpuID, true)

			case r.Method == "GET" && strings.Contains(r.URL.Path, "/gpu/"+gpuID):
				gpuStatusCalled = true
				testutil.WriteJSONResponse(w, newGPUResponse(gpuID, "test-gpu"))

			default:
				testutil.LogUnexpectedRequest(t, w, r)
			}
		},
	})
	defer server.Close()

	model := createTestGPUModel("test-gpu", "H100 Series", "h100_series", testImageName, 2)

	response, err := CreateGPU(gpcnClient, context.Background(), seriesID, model)
	if err != nil {
		t.Fatalf("CreateGPU failed: %v", err)
	}
	if response == nil {
		t.Fatal("Expected response, got nil")
		return
	}
	if response.Data.ID != gpuID {
		t.Errorf("Expected GPU ID '%s', got '%s'", gpuID, response.Data.ID)
	}
	if !createCalled {
		t.Error("Expected create endpoint to be called")
	}
	if !jobStatusCalled {
		t.Error("Expected job status endpoint to be called")
	}
	if !gpuStatusCalled {
		t.Error("Expected GPU status endpoint to be called")
	}
}

func TestGetGPUMockHTTP(t *testing.T) {
	const gpuID = "gpu-789"

	server, gpcnClient := testutil.SetupMockServerWithGpcnClient(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "GET" && strings.Contains(r.URL.Path, "/gpu/"+gpuID) {
				testutil.WriteJSONResponse(w, newGPUResponse(gpuID, "test-gpu"))
			} else {
				testutil.LogUnexpectedRequest(t, w, r)
			}
		},
	})
	defer server.Close()

	response, err := GetGPU(gpcnClient, context.Background(), gpuID)
	if err != nil {
		t.Fatalf("GetGPU failed: %v", err)
	}
	if response == nil {
		t.Fatal("Expected response, got nil")
		return
	}
	if response.Data.ID != gpuID {
		t.Errorf("Expected GPU ID '%s', got '%s'", gpuID, response.Data.ID)
	}
	if response.Data.Status != "Running" {
		t.Errorf("Expected status 'Running', got '%s'", response.Data.Status)
	}
	if response.Data.Name != "test-gpu" {
		t.Errorf("Expected name 'test-gpu', got '%s'", response.Data.Name)
	}
}

func TestUpdateGPUMockHTTP(t *testing.T) {
	const (
		gpuID   = "gpu-update-123"
		newName = "updated-gpu"
	)

	var updateCalled bool

	server, gpcnClient := testutil.SetupMockServerWithGpcnClient(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "PUT" && strings.Contains(r.URL.Path, "/gpu/"+gpuID) {
				updateCalled = true
				body, _ := io.ReadAll(r.Body)
				var req map[string]any
				_ = json.Unmarshal(body, &req)
				if req["name"] != newName {
					t.Errorf("Expected name '%s', got '%v'", newName, req["name"])
				}
				testutil.WriteJSONResponse(w, map[string]bool{"success": true})
			} else {
				testutil.LogUnexpectedRequest(t, w, r)
			}
		},
	})
	defer server.Close()

	err := UpdateGPU(gpcnClient, context.Background(), gpuID, newName)
	if err != nil {
		t.Fatalf("UpdateGPU failed: %v", err)
	}
	if !updateCalled {
		t.Error("Expected update endpoint to be called")
	}
}

func TestDeleteGPUMockHTTP(t *testing.T) {
	const (
		gpuID = "gpu-delete-123"
		jobID = "job-delete-456"
	)

	var deleteCalled, jobStatusCalled bool

	server, gpcnClient := testutil.SetupMockServerWithGpcnClient(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.Method == "DELETE" && strings.Contains(r.URL.Path, "/gpu/"+gpuID):
				deleteCalled = true
				testutil.WriteJSONResponse(w, client.JobStatusSingularResponse{
					Success: true,
					Message: "GPU deletion job started",
					Data:    client.JobResponse{JobID: jobID, ResourceId: gpuID},
				})

			case r.Method == "POST" && strings.Contains(r.URL.Path, "/jobs"):
				jobStatusCalled = true
				testutil.HandleJobResponse(w, jobID, gpuID, true)

			default:
				testutil.LogUnexpectedRequest(t, w, r)
			}
		},
	})
	defer server.Close()

	err := DeleteGPU(gpcnClient, context.Background(), gpuID)
	if err != nil {
		t.Fatalf("DeleteGPU failed: %v", err)
	}
	if !deleteCalled {
		t.Error("Expected delete endpoint to be called")
	}
	if !jobStatusCalled {
		t.Error("Expected job status endpoint to be called")
	}
}

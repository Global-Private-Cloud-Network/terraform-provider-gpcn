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
	auth := ResourceModelInitialAuth{SshKeyId: types.StringValue(testSSHKeyID)}
	var diags diag.Diagnostics
	model.InitialAuth, diags = types.ObjectValueFrom(context.Background(), auth.AttrTypes(), auth)
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
	availableSkus := make([]struct {
		SkuCode     string `json:"skuCode"`
		Description string `json:"description"`
		Specs       struct {
			GPUDescription string `json:"gpuDescription"`
			VCPU           int64  `json:"vcpu"`
			Memory         int64  `json:"memoryGiB"`
			Storage        int64  `json:"storageGB"`
		} `json:"specs"`
	}, availableCount)

	series := struct {
		ID           string `json:"id"`
		Name         string `json:"name"`
		Code         string `json:"code"`
		Availability []struct {
			DatacenterId   string `json:"datacenterId"`
			DatacenterName string `json:"datacenterName"`
			DatacenterCode string `json:"datacenterCode"`
			GPUCounts      []struct {
				Count         int64 `json:"count"`
				AvailableSkus []struct {
					SkuCode     string `json:"skuCode"`
					Description string `json:"description"`
					Specs       struct {
						GPUDescription string `json:"gpuDescription"`
						VCPU           int64  `json:"vcpu"`
						Memory         int64  `json:"memoryGiB"`
						Storage        int64  `json:"storageGB"`
					} `json:"specs"`
				} `json:"availableSkus"`
				Specs struct {
					GPUDescription string `json:"gpuDescription"`
					VCPU           int64  `json:"vcpu"`
					Memory         int64  `json:"memoryGiB"`
					Storage        int64  `json:"storageGB"`
				} `json:"specs"`
			} `json:"gpuCounts"`
		} `json:"availability"`
	}{
		ID:   seriesID,
		Name: "NVIDIA H100 Series",
		Code: seriesCode,
		Availability: []struct {
			DatacenterId   string `json:"datacenterId"`
			DatacenterName string `json:"datacenterName"`
			DatacenterCode string `json:"datacenterCode"`
			GPUCounts      []struct {
				Count         int64 `json:"count"`
				AvailableSkus []struct {
					SkuCode     string `json:"skuCode"`
					Description string `json:"description"`
					Specs       struct {
						GPUDescription string `json:"gpuDescription"`
						VCPU           int64  `json:"vcpu"`
						Memory         int64  `json:"memoryGiB"`
						Storage        int64  `json:"storageGB"`
					} `json:"specs"`
				} `json:"availableSkus"`
				Specs struct {
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
					Count         int64 `json:"count"`
					AvailableSkus []struct {
						SkuCode     string `json:"skuCode"`
						Description string `json:"description"`
						Specs       struct {
							GPUDescription string `json:"gpuDescription"`
							VCPU           int64  `json:"vcpu"`
							Memory         int64  `json:"memoryGiB"`
							Storage        int64  `json:"storageGB"`
						} `json:"specs"`
					} `json:"availableSkus"`
					Specs struct {
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
	}

	return inventoryResp{
		Data: struct {
			Series []struct {
				ID           string `json:"id"`
				Name         string `json:"name"`
				Code         string `json:"code"`
				Availability []struct {
					DatacenterId   string `json:"datacenterId"`
					DatacenterName string `json:"datacenterName"`
					DatacenterCode string `json:"datacenterCode"`
					GPUCounts      []struct {
						Count         int64 `json:"count"`
						AvailableSkus []struct {
							SkuCode     string `json:"skuCode"`
							Description string `json:"description"`
							Specs       struct {
								GPUDescription string `json:"gpuDescription"`
								VCPU           int64  `json:"vcpu"`
								Memory         int64  `json:"memoryGiB"`
								Storage        int64  `json:"storageGB"`
							} `json:"specs"`
						} `json:"availableSkus"`
						Specs struct {
							GPUDescription string `json:"gpuDescription"`
							VCPU           int64  `json:"vcpu"`
							Memory         int64  `json:"memoryGiB"`
							Storage        int64  `json:"storageGB"`
						} `json:"specs"`
					} `json:"gpuCounts"`
				} `json:"availability"`
			} `json:"series"`
		}{
			Series: []struct {
				ID           string `json:"id"`
				Name         string `json:"name"`
				Code         string `json:"code"`
				Availability []struct {
					DatacenterId   string `json:"datacenterId"`
					DatacenterName string `json:"datacenterName"`
					DatacenterCode string `json:"datacenterCode"`
					GPUCounts      []struct {
						Count         int64 `json:"count"`
						AvailableSkus []struct {
							SkuCode     string `json:"skuCode"`
							Description string `json:"description"`
							Specs       struct {
								GPUDescription string `json:"gpuDescription"`
								VCPU           int64  `json:"vcpu"`
								Memory         int64  `json:"memoryGiB"`
								Storage        int64  `json:"storageGB"`
							} `json:"specs"`
						} `json:"availableSkus"`
						Specs struct {
							GPUDescription string `json:"gpuDescription"`
							VCPU           int64  `json:"vcpu"`
							Memory         int64  `json:"memoryGiB"`
							Storage        int64  `json:"storageGB"`
						} `json:"specs"`
					} `json:"gpuCounts"`
				} `json:"availability"`
			}{series},
		},
	}
}

func TestMapGPUResponseToModelUnit(t *testing.T) {
	response := newGPUResponse("gpu-123", "test-gpu")
	model := createTestGPUModel("test-gpu", "NVIDIA H100 Series", "nvidia-h100_series", testImageName, 2)

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

const inventoryJSONA6000 = `{
  "success": true,
  "message": "ok",
  "data": {
    "series": [
      {
        "id": "series-a6000",
        "name": "NVIDIA RTX A6000 Series",
        "code": "nvidia-rtx_a6000-series",
        "availability": [
          {
            "datacenterId": "datacenter-123",
            "datacenterName": "US-East-1",
            "datacenterCode": "us-east-1",
            "gpuCounts": [
              {
                "count": 1,
                "specs": {"gpuDescription": "1x A6000", "vcpu": 6, "memoryGiB": 24, "storageGB": 256},
                "availableSkus": [
                  {"skuCode": "gpu_1x_a6000_low_ram", "description": "low", "specs": {"gpuDescription": "1x A6000 low", "vcpu": 6, "memoryGiB": 24, "storageGB": 256}},
                  {"skuCode": "gpu_1x_a6000", "description": "std", "specs": {"gpuDescription": "1x A6000", "vcpu": 6, "memoryGiB": 48, "storageGB": 256}}
                ]
              },
              {
                "count": 2,
                "specs": {"gpuDescription": "2x A6000", "vcpu": 14, "memoryGiB": 96, "storageGB": 512},
                "availableSkus": [
                  {"skuCode": "gpu_2x_a6000", "description": "std", "specs": {"gpuDescription": "2x A6000", "vcpu": 14, "memoryGiB": 96, "storageGB": 512}}
                ]
              }
            ]
          },
          {
            "datacenterId": "other-dc",
            "datacenterName": "Other",
            "datacenterCode": "other",
            "gpuCounts": [
              {
                "count": 1,
                "specs": {"gpuDescription": "1x A6000", "vcpu": 6, "memoryGiB": 48, "storageGB": 256},
                "availableSkus": [
                  {"skuCode": "gpu_1x_a6000_other", "description": "std", "specs": {"gpuDescription": "1x A6000", "vcpu": 6, "memoryGiB": 48, "storageGB": 256}}
                ]
              }
            ]
          }
        ]
      }
    ]
  }
}`

func TestFetchInventoryMockHTTP(t *testing.T) {
	const seriesCode = "nvidia-rtx_a6000-series"

	server, gpcnClient := testutil.SetupMockServerWithGpcnClient(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "GET" && strings.Contains(r.URL.Path, "/gpu/inventory") {
				query := r.URL.Query()
				if query.Get("datacenterId") != testDatacenterID {
					t.Errorf("Expected datacenterId '%s', got '%s'", testDatacenterID, query.Get("datacenterId"))
				}
				if query.Get("code") != seriesCode {
					t.Errorf("Expected code '%s', got '%s'", seriesCode, query.Get("code"))
				}
				if query.Has("count") {
					t.Errorf("Expected no count query param, got '%s'", query.Get("count"))
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(inventoryJSONA6000))
			} else {
				testutil.LogUnexpectedRequest(t, w, r)
			}
		},
	})
	defer server.Close()

	items, err := FetchInventory(gpcnClient, context.Background(), testDatacenterID, seriesCode, 0)
	if err != nil {
		t.Fatalf("FetchInventory failed: %v", err)
	}

	// Three SKUs in the matching datacenter (2 at count 1, 1 at count 2); the
	// "other-dc" SKU is filtered out.
	if len(items) != 3 {
		t.Fatalf("Expected 3 SKUs, got %d", len(items))
	}

	var found gpuFound
	for _, item := range items {
		if item.SkuCode == "gpu_1x_a6000_other" {
			t.Error("Expected SKU from a different datacenter to be filtered out")
		}
		if item.SkuCode == "gpu_1x_a6000" {
			found.item = item
			found.ok = true
		}
	}
	if !found.ok {
		t.Fatal("Expected to find SKU 'gpu_1x_a6000'")
	}
	if found.item.SeriesName != "NVIDIA RTX A6000 Series" || found.item.SeriesCode != seriesCode {
		t.Errorf("Unexpected series on SKU: %+v", found.item)
	}
	if found.item.GPUCount != 1 || found.item.VCPU != 6 || found.item.MemoryGiB != 48 || found.item.StorageGB != 256 {
		t.Errorf("Unexpected per-SKU specs: %+v", found.item)
	}
}

type gpuFound struct {
	item FlatInventory
	ok   bool
}

func TestFetchInventoryCountFilterMockHTTP(t *testing.T) {
	server, gpcnClient := testutil.SetupMockServerWithGpcnClient(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "GET" && strings.Contains(r.URL.Path, "/gpu/inventory") {
				if r.URL.Query().Get("count") != "1" {
					t.Errorf("Expected count '1', got '%s'", r.URL.Query().Get("count"))
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(inventoryJSONA6000))
			} else {
				testutil.LogUnexpectedRequest(t, w, r)
			}
		},
	})
	defer server.Close()

	items, err := FetchInventory(gpcnClient, context.Background(), testDatacenterID, "nvidia-rtx_a6000-series", 1)
	if err != nil {
		t.Fatalf("FetchInventory failed: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("Expected 2 SKUs at count 1, got %d", len(items))
	}
	for _, item := range items {
		if item.GPUCount != 1 {
			t.Errorf("Expected only count-1 SKUs, got count %d", item.GPUCount)
		}
	}
}

func TestFetchInventoryEmptyMockHTTP(t *testing.T) {
	server, gpcnClient := testutil.SetupMockServerWithGpcnClient(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "GET" && strings.Contains(r.URL.Path, "/gpu/inventory") {
				testutil.WriteJSONResponse(w, newInventoryResponse("series-123", "nvidia-h100_series", testDatacenterID, 2, 0))
			} else {
				testutil.LogUnexpectedRequest(t, w, r)
			}
		},
	})
	defer server.Close()

	items, err := FetchInventory(gpcnClient, context.Background(), testDatacenterID, "nvidia-h100_series", 2)
	if err != nil {
		t.Fatalf("Expected no error for empty inventory, got: %v", err)
	}
	if len(items) != 0 {
		t.Errorf("Expected 0 SKUs, got %d", len(items))
	}
}

func TestCreateGPUWithSkuCodeMockHTTP(t *testing.T) {
	const (
		jobID    = "job-sku-1"
		gpuID    = "gpu-sku-1"
		seriesID = "series-sku-1"
		skuCode  = "gpu_1x_a6000"
	)

	var createCalled bool

	server, gpcnClient := testutil.SetupMockServerWithGpcnClient(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/gpu/"):
				createCalled = true
				req := testutil.ReadRequestBody(r)
				// sku_code is additive: the API still needs seriesId and gpuCount.
				if req["skuCode"] != skuCode {
					t.Errorf("Expected skuCode '%s', got '%v'", skuCode, req["skuCode"])
				}
				if req["seriesId"] != seriesID {
					t.Errorf("Expected seriesId '%s', got '%v'", seriesID, req["seriesId"])
				}
				if int64(req["gpuCount"].(float64)) != 2 {
					t.Errorf("Expected gpuCount 2, got '%v'", req["gpuCount"])
				}
				testutil.WriteJSONResponse(w, client.JobStatusMultiResponse{
					Success: true,
					Message: "GPU creation job started",
					Data: client.JobStatusDataResponse{
						Jobs: []client.JobResponse{{JobID: jobID, ResourceId: gpuID}},
					},
				})

			case r.Method == "POST" && strings.Contains(r.URL.Path, "/jobs"):
				testutil.HandleJobResponse(w, jobID, gpuID, true)

			case r.Method == "GET" && strings.Contains(r.URL.Path, "/gpu/"+gpuID):
				testutil.WriteJSONResponse(w, newGPUResponse(gpuID, "test-gpu"))

			default:
				testutil.LogUnexpectedRequest(t, w, r)
			}
		},
	})
	defer server.Close()

	model := createTestGPUModel("test-gpu", "", "nvidia-rtx_a6000-series", testImageName, 2)
	model.SkuCode = types.StringValue(skuCode)

	response, err := CreateGPU(gpcnClient, context.Background(), seriesID, model)
	if err != nil {
		t.Fatalf("CreateGPU failed: %v", err)
	}
	if response == nil {
		t.Fatal("Expected response, got nil")
		return
	}
	if !createCalled {
		t.Error("Expected create endpoint to be called")
	}
}

func TestMapGPUResponseToModelSkuCodeUnit(t *testing.T) {
	response := newGPUResponse("gpu-123", "test-gpu")
	response.Data.Configuration.SkuCode = "gpu_1x_a6000"

	model := createTestGPUModel("test-gpu", "", "", testImageName, 0)
	model.GPUCount = types.Int64Null()
	model.SkuCode = types.StringNull()

	result := MapGPUResponseToModel(context.Background(), response, model)
	if result.SkuCode.ValueString() != "gpu_1x_a6000" {
		t.Errorf("Expected sku_code back-filled to 'gpu_1x_a6000', got '%s'", result.SkuCode.ValueString())
	}
}

func TestCheckInventoryMockHTTP(t *testing.T) {
	const (
		seriesCode = "nvidia-h100_series"
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

	inventory, err := CheckInventory(gpcnClient, context.Background(), model)
	if err != nil {
		t.Fatalf("CheckInventory failed: %v", err)
	}
	if !inventoryCalled {
		t.Error("Expected inventory endpoint to be called")
	}
	if len(inventory) != 5 {
		t.Fatalf("Expected 5 available SKUs, got %d", len(inventory))
	}
	if inventory[0].SeriesID != "series-123" {
		t.Errorf("Expected series ID 'series-123', got '%s'", inventory[0].SeriesID)
	}
}

func TestCheckInventoryNoAvailabilityMockHTTP(t *testing.T) {
	const (
		seriesCode = "nvidia-h100_series"
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

	model := createTestGPUModel("test-gpu", "NVIDIA H100 Series", "nvidia-h100_series", testImageName, 2)

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

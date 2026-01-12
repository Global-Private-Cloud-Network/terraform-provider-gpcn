package gpu

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"terraform-provider-gpcn/internal/client"
	"terraform-provider-gpcn/internal/testutil"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

// Helper function to create a mock ResourceModel for testing
func createTestGPUModel(name, seriesName, seriesCode string, gpuCount int64) ResourceModel {
	model := ResourceModel{
		Name:         types.StringValue(name),
		DatacenterId: types.StringValue("datacenter-123"),
		GPUCount:     types.Int64Value(gpuCount),
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

	return model
}

// TestMapGPUResponseToModelUnit tests the mapping function
func TestMapGPUResponseToModelUnit(t *testing.T) {
	ctx := context.Background()

	createdAt := time.Now().Format(time.RFC3339)
	updatedAt := time.Now().Add(1 * time.Hour).Format(time.RFC3339)

	response := &readGPUResponse{}
	response.Data.ID = "gpu-123"
	response.Data.Name = "test-gpu"
	response.Data.CreatedAt = createdAt
	response.Data.UpdatedAt = updatedAt
	response.Data.Status = "Running"
	response.Data.IP = "192.168.1.100"
	response.Data.Datacenter.ID = "dc-123"
	response.Data.Datacenter.Name = "US-East-1"
	response.Data.Datacenter.Region = "US-East"
	response.Data.Datacenter.CountryAbbr = "US"
	response.Data.Datacenter.Country = "United States"

	model := createTestGPUModel("test-gpu", "H100 Series", "h100_series", 2)

	result := MapGPUResponseToModel(ctx, response, model)

	// Verify all fields are mapped correctly
	if result.ID.ValueString() != "gpu-123" {
		t.Errorf("Expected ID 'gpu-123', got '%s'", result.ID.ValueString())
	}

	if result.Name.ValueString() != "test-gpu" {
		t.Errorf("Expected Name 'test-gpu', got '%s'", result.Name.ValueString())
	}

	// Verify times are set
	if result.CreatedTime.IsNull() || result.CreatedTime.ValueString() == "unknown" {
		t.Errorf("Expected CreatedTime to be set, got '%s'", result.CreatedTime.ValueString())
	}

	if result.LastUpdated.IsNull() || result.LastUpdated.ValueString() == "unknown" {
		t.Errorf("Expected LastUpdated to be set, got '%s'", result.LastUpdated.ValueString())
	}

	// Verify location map is set
	if result.Location.IsNull() {
		t.Error("Expected Location to be set")
	}

	// Verify location contains expected values
	locationMap := make(map[string]string)
	_ = result.Location.ElementsAs(ctx, &locationMap, false)

	if locationMap["country"] != "United States" {
		t.Errorf("Expected country 'United States', got '%s'", locationMap["country"])
	}
	if locationMap["region"] != "US-East" {
		t.Errorf("Expected region 'US-East', got '%s'", locationMap["region"])
	}
	if locationMap["datacenter"] != "US-East-1" {
		t.Errorf("Expected datacenter 'US-East-1', got '%s'", locationMap["datacenter"])
	}
}

// TestCheckInventoryMockHTTP tests CheckInventory with a mock HTTP server
func TestCheckInventoryMockHTTP(t *testing.T) {
	seriesCode := "h100_series"
	datacenterId := "datacenter-123"
	gpuCount := int64(2)

	var inventoryCalled bool

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" && strings.Contains(r.URL.Path, "/gpu/inventory") {
			inventoryCalled = true

			// Verify query parameters
			query := r.URL.Query()
			if query.Get("code") != seriesCode {
				t.Errorf("Expected code '%s', got '%s'", seriesCode, query.Get("code"))
			}
			if query.Get("datacenterId") != datacenterId {
				t.Errorf("Expected datacenterId '%s', got '%s'", datacenterId, query.Get("datacenterId"))
			}
			if query.Get("count") != "2" {
				t.Errorf("Expected count '2', got '%s'", query.Get("count"))
			}

			// Return inventory response with availability
			response := gpuInventoryResponse{
				Data: []struct {
					ID           string "json:\"id\""
					Name         string "json:\"name\""
					Code         string "json:\"code\""
					Description  string "json:\"description\""
					Availability []struct {
						DatacenterId   string "json:\"datacenterId\""
						DatacenterName string "json:\"datacenterName\""
						DatacenterCode string "json:\"datacenterCode\""
						GPUCounts      []struct {
							Count     int64 "json:\"count\""
							Available int64 "json:\"available\""
							Specs     struct {
								GPUDescription string "json:\"gpuDescription\""
								VCPU           int64  "json:\"vcpu\""
								Memory         int64  "json:\"memoryGiB\""
								Storage        int64  "json:\"storageGB\""
							} "json:\"specs\""
						} "json:\"gpuCounts\""
					} "json:\"availability\""
				}{
					{
						ID:          "series-123",
						Name:        "H100 Series",
						Code:        seriesCode,
						Description: "NVIDIA H100 GPU Series",
						Availability: []struct {
							DatacenterId   string "json:\"datacenterId\""
							DatacenterName string "json:\"datacenterName\""
							DatacenterCode string "json:\"datacenterCode\""
							GPUCounts      []struct {
								Count     int64 "json:\"count\""
								Available int64 "json:\"available\""
								Specs     struct {
									GPUDescription string "json:\"gpuDescription\""
									VCPU           int64  "json:\"vcpu\""
									Memory         int64  "json:\"memoryGiB\""
									Storage        int64  "json:\"storageGB\""
								} "json:\"specs\""
							} "json:\"gpuCounts\""
						}{
							{
								DatacenterId:   datacenterId,
								DatacenterName: "US-East-1",
								DatacenterCode: "us-east-1",
								GPUCounts: []struct {
									Count     int64 "json:\"count\""
									Available int64 "json:\"available\""
									Specs     struct {
										GPUDescription string "json:\"gpuDescription\""
										VCPU           int64  "json:\"vcpu\""
										Memory         int64  "json:\"memoryGiB\""
										Storage        int64  "json:\"storageGB\""
									} "json:\"specs\""
								}{
									{
										Count:     gpuCount,
										Available: 5,
										Specs: struct {
											GPUDescription string "json:\"gpuDescription\""
											VCPU           int64  "json:\"vcpu\""
											Memory         int64  "json:\"memoryGiB\""
											Storage        int64  "json:\"storageGB\""
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
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(response)
		} else {
			t.Logf("Unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	httpClient := &http.Client{
		Transport: &testutil.MockTransport{
			BaseURL: server.URL,
		},
	}

	ctx := context.Background()
	model := createTestGPUModel("test-gpu", "", seriesCode, gpuCount)
	model.DatacenterId = types.StringValue(datacenterId)

	response, err := CheckInventory(httpClient, ctx, model)

	if err != nil {
		t.Fatalf("CheckInventory failed: %v", err)
	}

	if response == nil {
		t.Fatal("Expected response, got nil")
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

	if response.Data[0].Availability[0].GPUCounts[0].Available != 5 {
		t.Errorf("Expected 5 available GPUs, got %d", response.Data[0].Availability[0].GPUCounts[0].Available)
	}
}

// TestCheckInventoryNoAvailabilityMockHTTP tests CheckInventory when no inventory is available
func TestCheckInventoryNoAvailabilityMockHTTP(t *testing.T) {
	seriesCode := "h100_series"
	datacenterId := "datacenter-123"
	gpuCount := int64(2)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" && strings.Contains(r.URL.Path, "/gpu/inventory") {
			// Return inventory response with zero availability
			response := gpuInventoryResponse{
				Data: []struct {
					ID           string "json:\"id\""
					Name         string "json:\"name\""
					Code         string "json:\"code\""
					Description  string "json:\"description\""
					Availability []struct {
						DatacenterId   string "json:\"datacenterId\""
						DatacenterName string "json:\"datacenterName\""
						DatacenterCode string "json:\"datacenterCode\""
						GPUCounts      []struct {
							Count     int64 "json:\"count\""
							Available int64 "json:\"available\""
							Specs     struct {
								GPUDescription string "json:\"gpuDescription\""
								VCPU           int64  "json:\"vcpu\""
								Memory         int64  "json:\"memoryGiB\""
								Storage        int64  "json:\"storageGB\""
							} "json:\"specs\""
						} "json:\"gpuCounts\""
					} "json:\"availability\""
				}{
					{
						ID:          "series-123",
						Name:        "H100 Series",
						Code:        seriesCode,
						Description: "NVIDIA H100 GPU Series",
						Availability: []struct {
							DatacenterId   string "json:\"datacenterId\""
							DatacenterName string "json:\"datacenterName\""
							DatacenterCode string "json:\"datacenterCode\""
							GPUCounts      []struct {
								Count     int64 "json:\"count\""
								Available int64 "json:\"available\""
								Specs     struct {
									GPUDescription string "json:\"gpuDescription\""
									VCPU           int64  "json:\"vcpu\""
									Memory         int64  "json:\"memoryGiB\""
									Storage        int64  "json:\"storageGB\""
								} "json:\"specs\""
							} "json:\"gpuCounts\""
						}{
							{
								DatacenterId:   datacenterId,
								DatacenterName: "US-East-1",
								DatacenterCode: "us-east-1",
								GPUCounts: []struct {
									Count     int64 "json:\"count\""
									Available int64 "json:\"available\""
									Specs     struct {
										GPUDescription string "json:\"gpuDescription\""
										VCPU           int64  "json:\"vcpu\""
										Memory         int64  "json:\"memoryGiB\""
										Storage        int64  "json:\"storageGB\""
									} "json:\"specs\""
								}{
									{
										Count:     gpuCount,
										Available: 0, // No availability
									},
								},
							},
						},
					},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(response)
		}
	}))
	defer server.Close()

	httpClient := &http.Client{
		Transport: &testutil.MockTransport{
			BaseURL: server.URL,
		},
	}

	ctx := context.Background()
	model := createTestGPUModel("test-gpu", "", seriesCode, gpuCount)
	model.DatacenterId = types.StringValue(datacenterId)

	_, err := CheckInventory(httpClient, ctx, model)

	if err == nil {
		t.Fatal("Expected error for no availability, got nil")
	}

	expectedError := "no GPU availability"
	if !strings.Contains(err.Error(), expectedError) {
		t.Errorf("Expected error to contain '%s', got '%s'", expectedError, err.Error())
	}
}

// TestCreateGPUMockHTTP tests CreateGPU with a mock HTTP server
func TestCreateGPUMockHTTP(t *testing.T) {
	jobID := "job-123"
	gpuID := "gpu-456"
	seriesID := "series-789"

	// Track which endpoints were called
	var createCalled, jobStatusCalled, gpuStatusCalled bool

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/gpu/") {
			createCalled = true

			// Verify request body
			body, _ := io.ReadAll(r.Body)
			var requestBody map[string]any
			_ = json.Unmarshal(body, &requestBody)

			if requestBody["name"] != "test-gpu" {
				t.Errorf("Expected name 'test-gpu', got '%v'", requestBody["name"])
			}
			if requestBody["seriesId"] != seriesID {
				t.Errorf("Expected seriesId '%s', got '%v'", seriesID, requestBody["seriesId"])
			}
			if int64(requestBody["gpuCount"].(float64)) != 2 {
				t.Errorf("Expected gpuCount 2, got '%v'", requestBody["gpuCount"])
			}
			if requestBody["datacenterId"] != "datacenter-123" {
				t.Errorf("Expected datacenterId 'datacenter-123', got '%v'", requestBody["datacenterId"])
			}

			// Return job response
			response := client.JobStatusMultiResponse{
				Success: true,
				Message: "GPU creation job started",
				Data: client.JobStatusDataResponse{
					Jobs: []client.JobResponse{
						{
							JobID:       jobID,
							ResourceId:  gpuID,
							IsCompleted: false,
							HasFailed:   false,
						},
					},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(response)
		} else if r.Method == "POST" && strings.Contains(r.URL.Path, "/jobs") {
			jobStatusCalled = true
			// Return completed job
			response := client.JobStatusMultiResponse{
				Success: true,
				Message: "Job completed",
				Data: client.JobStatusDataResponse{
					Jobs: []client.JobResponse{
						{
							JobID:       jobID,
							ResourceId:  gpuID,
							IsCompleted: true,
							HasFailed:   false,
						},
					},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(response)
		} else if r.Method == "GET" && strings.Contains(r.URL.Path, "/gpu/"+gpuID) {
			gpuStatusCalled = true
			// Return GPU details
			response := readGPUResponse{}
			response.Data.ID = gpuID
			response.Data.Name = "test-gpu"
			response.Data.CreatedAt = time.Now().Format(time.RFC3339)
			response.Data.UpdatedAt = time.Now().Format(time.RFC3339)
			response.Data.Status = "Running"
			response.Data.IP = "192.168.1.100"
			response.Data.Datacenter.ID = "datacenter-123"
			response.Data.Datacenter.Name = "US-East-1"
			response.Data.Datacenter.Region = "US-East"
			response.Data.Datacenter.CountryAbbr = "US"
			response.Data.Datacenter.Country = "United States"
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(response)
		} else {
			t.Logf("Unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	httpClient := &http.Client{
		Transport: &testutil.MockTransport{
			BaseURL: server.URL,
		},
	}

	ctx := context.Background()
	model := createTestGPUModel("test-gpu", "H100 Series", "h100_series", 2)

	// Call CreateGPU
	response, err := CreateGPU(httpClient, ctx, seriesID, model)

	if err != nil {
		t.Fatalf("CreateGPU failed: %v", err)
	}

	if response == nil {
		t.Fatal("Expected response, got nil")
	}

	if response.Data.ID != gpuID {
		t.Errorf("Expected GPU ID '%s', got '%s'", gpuID, response.Data.ID)
	}

	// Verify all endpoints were called
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

// TestGetGPUMockHTTP tests GetGPU with a mock HTTP server
func TestGetGPUMockHTTP(t *testing.T) {
	gpuID := "gpu-789"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" && strings.Contains(r.URL.Path, "/gpu/"+gpuID) {
			response := readGPUResponse{}
			response.Data.ID = gpuID
			response.Data.Name = "test-gpu"
			response.Data.CreatedAt = time.Now().Format(time.RFC3339)
			response.Data.UpdatedAt = time.Now().Format(time.RFC3339)
			response.Data.Status = "Running"
			response.Data.IP = "192.168.1.100"
			response.Data.Datacenter.ID = "datacenter-123"
			response.Data.Datacenter.Name = "US-East-1"
			response.Data.Datacenter.Region = "US-East"
			response.Data.Datacenter.CountryAbbr = "US"
			response.Data.Datacenter.Country = "United States"
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(response)
		} else {
			t.Logf("Unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	httpClient := &http.Client{
		Transport: &testutil.MockTransport{
			BaseURL: server.URL,
		},
	}

	ctx := context.Background()

	response, err := GetGPU(httpClient, ctx, gpuID)

	if err != nil {
		t.Fatalf("GetGPU failed: %v", err)
	}

	if response == nil {
		t.Fatal("Expected response, got nil")
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

// TestUpdateGPUMockHTTP tests UpdateGPU with a mock HTTP server
func TestUpdateGPUMockHTTP(t *testing.T) {
	gpuID := "gpu-update-123"
	newName := "updated-gpu"

	var updateCalled bool

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "PUT" && strings.Contains(r.URL.Path, "/gpu/"+gpuID) {
			updateCalled = true

			// Verify request body contains expected fields
			body, _ := io.ReadAll(r.Body)
			var requestBody map[string]any
			_ = json.Unmarshal(body, &requestBody)

			if requestBody["name"] != newName {
				t.Errorf("Expected name '%s', got '%v'", newName, requestBody["name"])
			}

			// UpdateGPU returns success directly
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]bool{"success": true})
		} else {
			t.Logf("Unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	httpClient := &http.Client{
		Transport: &testutil.MockTransport{
			BaseURL: server.URL,
		},
	}

	ctx := context.Background()

	err := UpdateGPU(httpClient, ctx, gpuID, newName)

	if err != nil {
		t.Fatalf("UpdateGPU failed: %v", err)
	}

	if !updateCalled {
		t.Error("Expected update endpoint to be called")
	}
}

// TestDeleteGPUMockHTTP tests DeleteGPU with a mock HTTP server
func TestDeleteGPUMockHTTP(t *testing.T) {
	gpuID := "gpu-delete-123"
	jobID := "job-delete-456"

	var deleteCalled, jobStatusCalled bool

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "DELETE" && strings.Contains(r.URL.Path, "/gpu/"+gpuID) {
			deleteCalled = true

			// Return job response
			response := client.JobStatusSingularResponse{
				Success: true,
				Message: "GPU deletion job started",
				Data: client.JobResponse{
					JobID:       jobID,
					ResourceId:  gpuID,
					IsCompleted: false,
					HasFailed:   false,
				},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(response)
		} else if r.Method == "POST" && strings.Contains(r.URL.Path, "/jobs") {
			jobStatusCalled = true
			// Return completed job
			response := client.JobStatusMultiResponse{
				Success: true,
				Message: "Job completed",
				Data: client.JobStatusDataResponse{
					Jobs: []client.JobResponse{
						{
							JobID:       jobID,
							ResourceId:  gpuID,
							IsCompleted: true,
							HasFailed:   false,
						},
					},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(response)
		} else {
			t.Logf("Unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	httpClient := &http.Client{
		Transport: &testutil.MockTransport{
			BaseURL: server.URL,
		},
	}

	ctx := context.Background()

	err := DeleteGPU(httpClient, ctx, gpuID)

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

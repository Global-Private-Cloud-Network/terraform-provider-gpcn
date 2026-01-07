package volumes

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
func createTestVolumeModel(name, volumeType string, sizeGb int64) ResourceModel {
	return ResourceModel{
		Name:         types.StringValue(name),
		DatacenterId: types.StringValue("datacenter-123"),
		VolumeType:   types.StringValue(volumeType),
		SizeGb:       types.Int64Value(sizeGb),
	}
}

// TestMapVolumeResponseToModel tests the mapping function
func TestMapVolumeResponseToModel_Unit(t *testing.T) {
	ctx := context.Background()

	createdAt := time.Now().Format(time.RFC3339)
	updatedAt := time.Now().Add(1 * time.Hour).Format(time.RFC3339)

	response := &readVolumesResponse{
		Success: true,
		Message: "Volume retrieved successfully",
	}
	response.Data.ID = "volume-123"
	response.Data.Name = "test-volume"
	response.Data.SizeGb = 256
	response.Data.VolumeType.ID = 1
	response.Data.VolumeType.Name = "SSD"
	response.Data.VolumeType.Description = "Solid State Drive"
	response.Data.VolumeSizeId = 10
	response.Data.Datacenter.ID = "dc-123"
	response.Data.Datacenter.Name = "US-East-1"
	response.Data.Datacenter.Region = "US-East"
	response.Data.Datacenter.Country = "US"
	response.Data.VirtualMachineId = ""
	response.Data.VirtualMachineName = ""
	response.Data.CreatedAt = createdAt
	response.Data.UpdatedAt = updatedAt

	model := createTestVolumeModel("test-volume", "SSD", 256)

	result := MapVolumeResponseToModel(ctx, response, model)

	// Verify all fields are mapped correctly
	if result.ID.ValueString() != "volume-123" {
		t.Errorf("Expected ID 'volume-123', got '%s'", result.ID.ValueString())
	}

	if result.VolumeTypeId.ValueInt64() != 1 {
		t.Errorf("Expected VolumeTypeId 1, got %d", result.VolumeTypeId.ValueInt64())
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
}

// TestCreateVolume_MockHTTP tests CreateVolume with a mock HTTP server
func TestCreateVolume_MockHTTP(t *testing.T) {
	jobID := "job-123"
	volumeID := "volume-456"
	volumeSizeID := int64(10)

	// Track which endpoints were called
	var volumeSizesCalled, createCalled, jobStatusCalled, getCalled bool

	// Create a mock HTTP server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" && strings.Contains(r.URL.Path, "/data-centers/") && strings.HasSuffix(r.URL.Path, "/volume-sizes") {
			volumeSizesCalled = true
			// Return volume sizes response
			response := volumeSizesResponse{
				Success: true,
				Message: "Volume sizes retrieved",
				Data: volumeSizesDataResponse{
					DatacenterId: "datacenter-123",
					VolumeTypes: []volumeSizesDataVolumeTypesResponse{
						{
							ID:          1,
							Name:        "SSD",
							Description: "Solid State Drive",
							AvailableSizes: []volumeSizesDataVolumeTypesAvailableSizesResponse{
								{ID: volumeSizeID, SizeGb: 256},
								{ID: 11, SizeGb: 512},
							},
						},
					},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(response)
		} else if r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/volumes/") {
			createCalled = true
			// Verify request body
			body, _ := io.ReadAll(r.Body)
			var requestBody map[string]any
			_ = json.Unmarshal(body, &requestBody)

			if requestBody["name"] != "test-volume" {
				t.Errorf("Expected name 'test-volume', got '%v'", requestBody["name"])
			}
			if int64(requestBody["sizeGb"].(float64)) != 256 {
				t.Errorf("Expected sizeGb 256, got '%v'", requestBody["sizeGb"])
			}

			// Return job response
			response := client.JobStatusSingularResponse{
				Success: true,
				Message: "Volume creation job started",
				Data: client.JobResponse{
					JobID: jobID,
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
							ResourceId:  volumeID,
							IsCompleted: true,
							HasFailed:   false,
						},
					},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(response)
		} else if r.Method == "GET" && strings.Contains(r.URL.Path, "/volumes/"+volumeID) {
			getCalled = true
			// Return volume details
			response := readVolumesResponse{
				Success: true,
				Message: "Volume retrieved",
			}
			response.Data.ID = volumeID
			response.Data.Name = "test-volume"
			response.Data.SizeGb = 256
			response.Data.VolumeType.ID = 1
			response.Data.VolumeType.Name = "SSD"
			response.Data.VolumeType.Description = "Solid State Drive"
			response.Data.VolumeSizeId = volumeSizeID
			response.Data.Datacenter.ID = "datacenter-123"
			response.Data.Datacenter.Name = "US-East-1"
			response.Data.Datacenter.Region = "US-East"
			response.Data.Datacenter.Country = "US"
			response.Data.VirtualMachineId = ""
			response.Data.VirtualMachineName = ""
			response.Data.CreatedAt = time.Now().Format(time.RFC3339)
			response.Data.UpdatedAt = time.Now().Format(time.RFC3339)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(response)
		} else {
			t.Logf("Unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	// Create a custom HTTP client that points to our mock server
	httpClient := &http.Client{
		Transport: &testutil.MockTransport{
			BaseURL: server.URL,
		},
	}

	ctx := context.Background()
	model := createTestVolumeModel("test-volume", "SSD", 256)

	// Call CreateVolume
	response, err := CreateVolume(httpClient, ctx, model)

	if err != nil {
		t.Fatalf("CreateVolume failed: %v", err)
	}

	if response == nil {
		t.Fatal("Expected response, got nil")
	}

	if response.Data.ID != volumeID {
		t.Errorf("Expected volume ID '%s', got '%s'", volumeID, response.Data.ID)
	}

	// Verify all endpoints were called
	if !volumeSizesCalled {
		t.Error("Expected volume sizes endpoint to be called")
	}
	if !createCalled {
		t.Error("Expected create endpoint to be called")
	}
	if !jobStatusCalled {
		t.Error("Expected job status endpoint to be called")
	}
	if !getCalled {
		t.Error("Expected get volume endpoint to be called")
	}
}

// TestGetVolume_MockHTTP tests GetVolume with a mock HTTP server
func TestGetVolume_MockHTTP(t *testing.T) {
	volumeID := "volume-789"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" && strings.Contains(r.URL.Path, "/volumes/"+volumeID) {
			response := readVolumesResponse{
				Success: true,
				Message: "Volume retrieved",
			}
			response.Data.ID = volumeID
			response.Data.Name = "test-volume"
			response.Data.SizeGb = 256
			response.Data.VolumeType.ID = 1
			response.Data.VolumeType.Name = "SSD"
			response.Data.VolumeType.Description = "Solid State Drive"
			response.Data.VolumeSizeId = 10
			response.Data.Datacenter.ID = "datacenter-123"
			response.Data.Datacenter.Name = "US-East-1"
			response.Data.Datacenter.Region = "US-East"
			response.Data.Datacenter.Country = "US"
			response.Data.VirtualMachineId = ""
			response.Data.VirtualMachineName = ""
			response.Data.CreatedAt = time.Now().Format(time.RFC3339)
			response.Data.UpdatedAt = time.Now().Format(time.RFC3339)
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

	response, err := GetVolume(httpClient, ctx, volumeID)

	if err != nil {
		t.Fatalf("GetVolume failed: %v", err)
	}

	if response == nil {
		t.Fatal("Expected response, got nil")
	}

	if response.Data.ID != volumeID {
		t.Errorf("Expected volume ID '%s', got '%s'", volumeID, response.Data.ID)
	}
}

// TestUpdateVolume_MockHTTP tests UpdateVolume with a mock HTTP server
func TestUpdateVolume_MockHTTP(t *testing.T) {
	volumeID := "volume-update-123"
	newSizeGb := int64(512)
	volumeSizeID := int64(11)

	var volumeSizesCalled, updateCalled, jobStatusCalled, getCalled bool

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" && strings.Contains(r.URL.Path, "/data-centers/") && strings.HasSuffix(r.URL.Path, "/volume-sizes") {
			volumeSizesCalled = true
			// Return volume sizes response
			response := volumeSizesResponse{
				Success: true,
				Message: "Volume sizes retrieved",
				Data: volumeSizesDataResponse{
					DatacenterId: "datacenter-123",
					VolumeTypes: []volumeSizesDataVolumeTypesResponse{
						{
							ID:          1,
							Name:        "SSD",
							Description: "Solid State Drive",
							AvailableSizes: []volumeSizesDataVolumeTypesAvailableSizesResponse{
								{ID: 10, SizeGb: 256},
								{ID: volumeSizeID, SizeGb: newSizeGb},
							},
						},
					},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(response)
		} else if r.Method == "PUT" && strings.Contains(r.URL.Path, "/volumes/"+volumeID+"/resize") {
			updateCalled = true

			// Verify request body contains expected fields
			body, _ := io.ReadAll(r.Body)
			var requestBody map[string]any
			_ = json.Unmarshal(body, &requestBody)

			if int64(requestBody["newSizeGb"].(float64)) != newSizeGb {
				t.Errorf("Expected newSizeGb %d, got '%v'", newSizeGb, requestBody["newSizeGb"])
			}

			// Return job response
			response := client.JobStatusSingularResponse{
				Success: true,
				Message: "Volume resize job started",
				Data: client.JobResponse{
					JobID: "job-456",
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
							JobID:       "job-456",
							ResourceId:  volumeID,
							IsCompleted: true,
							HasFailed:   false,
						},
					},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(response)
		} else if r.Method == "GET" && strings.Contains(r.URL.Path, "/volumes/"+volumeID) {
			getCalled = true
			response := readVolumesResponse{
				Success: true,
				Message: "Volume retrieved",
			}
			response.Data.ID = volumeID
			response.Data.Name = "test-volume"
			response.Data.SizeGb = newSizeGb
			response.Data.VolumeType.ID = 1
			response.Data.VolumeType.Name = "SSD"
			response.Data.VolumeType.Description = "Solid State Drive"
			response.Data.VolumeSizeId = volumeSizeID
			response.Data.Datacenter.ID = "datacenter-123"
			response.Data.Datacenter.Name = "US-East-1"
			response.Data.Datacenter.Region = "US-East"
			response.Data.Datacenter.Country = "US"
			response.Data.VirtualMachineId = ""
			response.Data.VirtualMachineName = ""
			response.Data.CreatedAt = time.Now().Add(-24 * time.Hour).Format(time.RFC3339)
			response.Data.UpdatedAt = time.Now().Format(time.RFC3339)
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
	model := createTestVolumeModel("test-volume", "SSD", newSizeGb)
	model.ID = types.StringValue(volumeID)

	response, err := UpdateVolume(httpClient, ctx, volumeID, model)

	if err != nil {
		t.Fatalf("UpdateVolume failed: %v", err)
	}

	if response == nil {
		t.Fatal("Expected response, got nil")
	}

	if response.Data.SizeGb != newSizeGb {
		t.Errorf("Expected volume size %d, got %d", newSizeGb, response.Data.SizeGb)
	}

	if !volumeSizesCalled {
		t.Error("Expected volume sizes endpoint to be called")
	}
	if !updateCalled {
		t.Error("Expected update endpoint to be called")
	}
	if !jobStatusCalled {
		t.Error("Expected job status endpoint to be called")
	}
	if !getCalled {
		t.Error("Expected get volume endpoint to be called")
	}
}

// TestGetVolumeSizeId_MockHTTP tests GetVolumeSizeId with a mock HTTP server
func TestGetVolumeSizeId_MockHTTP(t *testing.T) {
	datacenterID := "datacenter-123"
	volumeTypeID := int64(1)
	sizeGb := int64(256)
	expectedVolumeSizeID := int64(10)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" && strings.Contains(r.URL.Path, "/data-centers/") && strings.HasSuffix(r.URL.Path, "/volume-sizes") {
			response := volumeSizesResponse{
				Success: true,
				Message: "Volume sizes retrieved",
				Data: volumeSizesDataResponse{
					DatacenterId: datacenterID,
					VolumeTypes: []volumeSizesDataVolumeTypesResponse{
						{
							ID:          volumeTypeID,
							Name:        "SSD",
							Description: "Solid State Drive",
							AvailableSizes: []volumeSizesDataVolumeTypesAvailableSizesResponse{
								{ID: expectedVolumeSizeID, SizeGb: sizeGb},
								{ID: 11, SizeGb: 512},
								{ID: 12, SizeGb: 1024},
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

	volumeSizeID, err := GetVolumeSizeId(httpClient, ctx, datacenterID, volumeTypeID, sizeGb)

	if err != nil {
		t.Fatalf("GetVolumeSizeId failed: %v", err)
	}

	if volumeSizeID != expectedVolumeSizeID {
		t.Errorf("Expected volume size ID %d, got %d", expectedVolumeSizeID, volumeSizeID)
	}
}

// TestGetVolumeSizeId_InvalidSize tests that GetVolumeSizeId returns an error for invalid size
func TestGetVolumeSizeId_InvalidSize_MockHTTP(t *testing.T) {
	datacenterID := "datacenter-123"
	volumeTypeID := int64(1)
	invalidSizeGb := int64(555) // Not in available sizes

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" && strings.Contains(r.URL.Path, "/data-centers/") && strings.HasSuffix(r.URL.Path, "/volume-sizes") {
			response := volumeSizesResponse{
				Success: true,
				Message: "Volume sizes retrieved",
				Data: volumeSizesDataResponse{
					DatacenterId: datacenterID,
					VolumeTypes: []volumeSizesDataVolumeTypesResponse{
						{
							ID:          volumeTypeID,
							Name:        "SSD",
							Description: "Solid State Drive",
							AvailableSizes: []volumeSizesDataVolumeTypesAvailableSizesResponse{
								{ID: 10, SizeGb: 256},
								{ID: 11, SizeGb: 512},
								{ID: 12, SizeGb: 1024},
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

	_, err := GetVolumeSizeId(httpClient, ctx, datacenterID, volumeTypeID, invalidSizeGb)

	if err == nil {
		t.Fatal("Expected error for invalid size, got nil")
	}

	expectedErrorMsg := "the specified volume size is not available for this datacenter"
	if !strings.Contains(err.Error(), expectedErrorMsg) {
		t.Errorf("Expected error to contain '%s', got '%s'", expectedErrorMsg, err.Error())
	}
}

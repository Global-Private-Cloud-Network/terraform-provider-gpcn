package virtualmachines

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
func createTestVMModel(name, image string, allocatePublicIP bool) ResourceModel {
	size := ResourceModelSize{
		Category: types.StringValue("general"),
		Tier:     types.StringValue("g-micro-1"),
	}

	sizeObj, _ := types.ObjectValueFrom(context.Background(), size.AttrTypes(), size)

	return ResourceModel{
		Name:             types.StringValue(name),
		DatacenterId:     types.StringValue("datacenter-123"),
		Image:            types.StringValue(image),
		Size:             sizeObj,
		AllocatePublicIp: types.BoolValue(allocatePublicIP),
		WaitForStartup:   types.BoolValue(false),
		NetworkIds:       types.ListNull(types.StringType),
		VolumeIds:        types.ListNull(types.StringType),
	}
}

// TestMapVirtualMachineResponseToModelUnit tests the mapping function
func TestMapVirtualMachineResponseToModelUnit(t *testing.T) {
	ctx := context.Background()

	createdAt := time.Now().Format(time.RFC3339)
	updatedAt := time.Now().Add(1 * time.Hour).Format(time.RFC3339)

	response := &ReadVirtualMachinesResponse{
		Success: true,
		Message: "VM retrieved successfully",
	}
	response.Data.Status = "Running"
	response.Data.VirtualMachine.ID = "vm-123"
	response.Data.VirtualMachine.Name = "test-vm"
	response.Data.VirtualMachine.CreatedAt = createdAt
	response.Data.VirtualMachine.UpdatedAt = updatedAt
	response.Data.VirtualMachine.ConfigurationId = 1
	response.Data.VirtualMachine.Configuration = "General - Micro - 1"
	response.Data.VirtualMachine.ConfigurationCode = "g-micro-1"
	response.Data.VirtualMachine.ConfigurationCategoryCode = "general"
	response.Data.VirtualMachine.CPU = 1
	response.Data.VirtualMachine.RAM = 2
	response.Data.VirtualMachine.Disk = 20
	response.Data.VirtualMachine.Image = "Alma Linux 8.x"
	response.Data.VirtualMachine.Username = "admin"
	response.Data.VirtualMachine.Datacenter.ID = "dc-123"
	response.Data.VirtualMachine.Datacenter.Name = "US-East-1"
	response.Data.VirtualMachine.Datacenter.Region = "US-East"
	response.Data.VirtualMachine.Datacenter.CountryAbbr = "US"
	response.Data.VirtualMachine.Datacenter.Country = "United States"

	// Create a mock HTTP server for network interface calls
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" && strings.Contains(r.URL.Path, "/network-interfaces") {
			// Return empty network interfaces
			response := map[string]any{
				"success": true,
				"message": "Network interfaces retrieved",
				"data":    []any{},
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

	model := createTestVMModel("test-vm", "Alma Linux 8.x", false)

	result := MapVirtualMachineResponseToModel(ctx, httpClient, response, model)

	// Verify all fields are mapped correctly
	if result.ID.ValueString() != "vm-123" {
		t.Errorf("Expected ID 'vm-123', got '%s'", result.ID.ValueString())
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

	// Verify configuration map is set
	if result.Configuration.IsNull() {
		t.Error("Expected Configuration to be set")
	}
}

// TestCreateVirtualMachineMockHTTP tests CreateVirtualMachine with a mock HTTP server
func TestCreateVirtualMachineMockHTTP(t *testing.T) {
	jobID := "job-123"
	vmID := "vm-456"
	imageID := int64(10)
	sizeID := int64(1)

	// Track which endpoints were called
	var createCalled, jobStatusCalled, vmStatusCalled bool

	// Create a mock HTTP server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/virtual-machines/") {
			createCalled = true

			// Verify request body
			body, _ := io.ReadAll(r.Body)
			var requestBody map[string]any
			_ = json.Unmarshal(body, &requestBody)

			if requestBody["name"] != "test-vm" {
				t.Errorf("Expected name 'test-vm', got '%v'", requestBody["name"])
			}
			if int64(requestBody["imageId"].(float64)) != imageID {
				t.Errorf("Expected imageId %d, got '%v'", imageID, requestBody["imageId"])
			}
			if int64(requestBody["configurationId"].(float64)) != sizeID {
				t.Errorf("Expected configurationId %d, got '%v'", sizeID, requestBody["configurationId"])
			}

			// Return job response with multiple jobs (VMs can create multiple instances)
			response := client.JobStatusMultiResponse{
				Success: true,
				Message: "VM creation job started",
				Data: client.JobStatusDataResponse{
					Jobs: []client.JobResponse{
						{
							JobID:       jobID,
							ResourceId:  vmID,
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
							ResourceId:  vmID,
							IsCompleted: true,
							HasFailed:   false,
						},
					},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(response)
		} else if r.Method == "GET" && strings.Contains(r.URL.Path, "/virtual-machines/"+vmID) {
			vmStatusCalled = true
			// Return VM details with Running status
			response := ReadVirtualMachinesResponse{
				Success: true,
				Message: "VM retrieved",
			}
			response.Data.Status = "Running"
			response.Data.VirtualMachine.ID = vmID
			response.Data.VirtualMachine.Name = "test-vm"
			response.Data.VirtualMachine.CreatedAt = time.Now().Format(time.RFC3339)
			response.Data.VirtualMachine.UpdatedAt = time.Now().Format(time.RFC3339)
			response.Data.VirtualMachine.ConfigurationId = sizeID
			response.Data.VirtualMachine.Configuration = "General - Micro - 1"
			response.Data.VirtualMachine.ConfigurationCode = "g-micro-1"
			response.Data.VirtualMachine.ConfigurationCategoryCode = "general"
			response.Data.VirtualMachine.CPU = 1
			response.Data.VirtualMachine.RAM = 2
			response.Data.VirtualMachine.Disk = 20
			response.Data.VirtualMachine.Image = "Alma Linux 8.x"
			response.Data.VirtualMachine.Username = "admin"
			response.Data.VirtualMachine.Datacenter.ID = "datacenter-123"
			response.Data.VirtualMachine.Datacenter.Name = "US-East-1"
			response.Data.VirtualMachine.Datacenter.Region = "US-East"
			response.Data.VirtualMachine.Datacenter.CountryAbbr = "US"
			response.Data.VirtualMachine.Datacenter.Country = "United States"
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(response)
		} else if r.Method == "GET" && strings.Contains(r.URL.Path, "/network-interfaces") {
			// Return empty network interfaces
			response := map[string]any{
				"success": true,
				"message": "Network interfaces retrieved",
				"data":    []any{},
			}
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
	model := createTestVMModel("test-vm", "Alma Linux 8.x", false)

	// Call CreateVirtualMachine
	response, err := CreateVirtualMachine(httpClient, ctx, imageID, sizeID, model)

	if err != nil {
		t.Fatalf("CreateVirtualMachine failed: %v", err)
	}

	if response == nil {
		t.Fatal("Expected response, got nil")
	}

	if response.Data.VirtualMachine.ID != vmID {
		t.Errorf("Expected VM ID '%s', got '%s'", vmID, response.Data.VirtualMachine.ID)
	}

	// Verify all endpoints were called
	if !createCalled {
		t.Error("Expected create endpoint to be called")
	}
	if !jobStatusCalled {
		t.Error("Expected job status endpoint to be called")
	}
	if !vmStatusCalled {
		t.Error("Expected VM status endpoint to be called")
	}
}

// TestGetVirtualMachineMockHTTP tests GetVirtualMachine with a mock HTTP server
func TestGetVirtualMachineMockHTTP(t *testing.T) {
	vmID := "vm-789"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" && strings.Contains(r.URL.Path, "/virtual-machines/"+vmID) {
			response := ReadVirtualMachinesResponse{
				Success: true,
				Message: "VM retrieved",
			}
			response.Data.Status = "Running"
			response.Data.VirtualMachine.ID = vmID
			response.Data.VirtualMachine.Name = "test-vm"
			response.Data.VirtualMachine.CreatedAt = time.Now().Format(time.RFC3339)
			response.Data.VirtualMachine.UpdatedAt = time.Now().Format(time.RFC3339)
			response.Data.VirtualMachine.ConfigurationId = 1
			response.Data.VirtualMachine.Configuration = "General - Micro - 1"
			response.Data.VirtualMachine.ConfigurationCode = "g-micro-1"
			response.Data.VirtualMachine.ConfigurationCategoryCode = "general"
			response.Data.VirtualMachine.CPU = 1
			response.Data.VirtualMachine.RAM = 2
			response.Data.VirtualMachine.Disk = 20
			response.Data.VirtualMachine.Image = "Alma Linux 8.x"
			response.Data.VirtualMachine.Username = "admin"
			response.Data.VirtualMachine.Datacenter.ID = "datacenter-123"
			response.Data.VirtualMachine.Datacenter.Name = "US-East-1"
			response.Data.VirtualMachine.Datacenter.Region = "US-East"
			response.Data.VirtualMachine.Datacenter.CountryAbbr = "US"
			response.Data.VirtualMachine.Datacenter.Country = "United States"
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

	response, err := GetVirtualMachine(httpClient, ctx, vmID)

	if err != nil {
		t.Fatalf("GetVirtualMachine failed: %v", err)
	}

	if response == nil {
		t.Fatal("Expected response, got nil")
	}

	if response.Data.VirtualMachine.ID != vmID {
		t.Errorf("Expected VM ID '%s', got '%s'", vmID, response.Data.VirtualMachine.ID)
	}

	if response.Data.Status != "Running" {
		t.Errorf("Expected status 'Running', got '%s'", response.Data.Status)
	}
}

// TestUpdateVirtualMachineMockHTTP tests UpdateVirtualMachine with a mock HTTP server
func TestUpdateVirtualMachineMockHTTP(t *testing.T) {
	vmID := "vm-update-123"
	newName := "updated-vm"

	var updateCalled, vmStatusCalled bool

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "PUT" && strings.Contains(r.URL.Path, "/virtual-machines/"+vmID) {
			updateCalled = true

			// Verify request body contains expected fields
			body, _ := io.ReadAll(r.Body)
			var requestBody map[string]any
			_ = json.Unmarshal(body, &requestBody)

			if requestBody["name"] != newName {
				t.Errorf("Expected name '%s', got '%v'", newName, requestBody["name"])
			}

			// UpdateVirtualMachine returns success directly, no job
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]bool{"success": true})
		} else if r.Method == "GET" && strings.Contains(r.URL.Path, "/virtual-machines/"+vmID) {
			vmStatusCalled = true
			response := ReadVirtualMachinesResponse{
				Success: true,
				Message: "VM retrieved",
			}
			response.Data.Status = "Running"
			response.Data.VirtualMachine.ID = vmID
			response.Data.VirtualMachine.Name = newName
			response.Data.VirtualMachine.CreatedAt = time.Now().Add(-24 * time.Hour).Format(time.RFC3339)
			response.Data.VirtualMachine.UpdatedAt = time.Now().Format(time.RFC3339)
			response.Data.VirtualMachine.ConfigurationId = 1
			response.Data.VirtualMachine.Configuration = "General - Micro - 1"
			response.Data.VirtualMachine.ConfigurationCode = "g-micro-1"
			response.Data.VirtualMachine.ConfigurationCategoryCode = "general"
			response.Data.VirtualMachine.CPU = 1
			response.Data.VirtualMachine.RAM = 2
			response.Data.VirtualMachine.Disk = 20
			response.Data.VirtualMachine.Image = "Alma Linux 8.x"
			response.Data.VirtualMachine.Username = "admin"
			response.Data.VirtualMachine.Datacenter.ID = "datacenter-123"
			response.Data.VirtualMachine.Datacenter.Name = "US-East-1"
			response.Data.VirtualMachine.Datacenter.Region = "US-East"
			response.Data.VirtualMachine.Datacenter.CountryAbbr = "US"
			response.Data.VirtualMachine.Datacenter.Country = "United States"
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(response)
		} else if r.Method == "GET" && strings.Contains(r.URL.Path, "/network-interfaces") {
			// Return empty network interfaces
			response := map[string]any{
				"success": true,
				"message": "Network interfaces retrieved",
				"data":    []any{},
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

	err := UpdateVirtualMachine(httpClient, ctx, vmID, newName)

	if err != nil {
		t.Fatalf("UpdateVirtualMachine failed: %v", err)
	}

	// Now get the updated VM to verify
	response, err := GetVirtualMachine(httpClient, ctx, vmID)

	if err != nil {
		t.Fatalf("UpdateVirtualMachine failed: %v", err)
	}

	if response == nil {
		t.Fatal("Expected response, got nil")
	}

	if response.Data.VirtualMachine.Name != newName {
		t.Errorf("Expected VM name '%s', got '%s'", newName, response.Data.VirtualMachine.Name)
	}

	if !updateCalled {
		t.Error("Expected update endpoint to be called")
	}
	if !vmStatusCalled {
		t.Error("Expected VM status endpoint to be called")
	}
}

// TestPollForVirtualMachineStatusMockHTTP tests the polling mechanism
func TestPollForVirtualMachineStatusMockHTTP(t *testing.T) {
	vmID := "vm-poll-123"
	pollCount := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" && strings.Contains(r.URL.Path, "/virtual-machines/"+vmID) {
			pollCount++
			// First call returns Building, second returns Running
			status := "Building"
			if pollCount >= 2 {
				status = "Running"
			}

			response := ReadVirtualMachinesResponse{
				Success: true,
				Message: "VM retrieved",
			}
			response.Data.Status = status
			response.Data.VirtualMachine.ID = vmID
			response.Data.VirtualMachine.Name = "test-vm"
			response.Data.VirtualMachine.CreatedAt = time.Now().Format(time.RFC3339)
			response.Data.VirtualMachine.UpdatedAt = time.Now().Format(time.RFC3339)
			response.Data.VirtualMachine.ConfigurationId = 1
			response.Data.VirtualMachine.Configuration = "General - Micro - 1"
			response.Data.VirtualMachine.ConfigurationCode = "g-micro-1"
			response.Data.VirtualMachine.ConfigurationCategoryCode = "general"
			response.Data.VirtualMachine.CPU = 1
			response.Data.VirtualMachine.RAM = 2
			response.Data.VirtualMachine.Disk = 20
			response.Data.VirtualMachine.Image = "Alma Linux 8.x"
			response.Data.VirtualMachine.Username = "admin"
			response.Data.VirtualMachine.Datacenter.ID = "datacenter-123"
			response.Data.VirtualMachine.Datacenter.Name = "US-East-1"
			response.Data.VirtualMachine.Datacenter.Region = "US-East"
			response.Data.VirtualMachine.Datacenter.CountryAbbr = "US"
			response.Data.VirtualMachine.Datacenter.Country = "United States"
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

	// Poll for Running status
	response, err := PollForVirtualMachineStatus(httpClient, ctx, vmID, []string{"Running"}, 10)

	if err != nil {
		t.Fatalf("PollForVirtualMachineStatus failed: %v", err)
	}

	if response == nil {
		t.Fatal("Expected response, got nil")
	}

	if response.Data.Status != "Running" {
		t.Errorf("Expected final status 'Running', got '%s'", response.Data.Status)
	}

	if pollCount < 2 {
		t.Errorf("Expected at least 2 polls, got %d", pollCount)
	}
}

// TestValidatePublicIpValueMockHTTP tests public IP validation with custom networks
func TestValidatePublicIpValueMockHTTP(t *testing.T) {
	tests := []struct {
		name             string
		networkType      string
		allocatePublicIP bool
		expectError      bool
	}{
		{
			name:             "public IP with standard network - valid",
			networkType:      "standard",
			allocatePublicIP: true,
			expectError:      false,
		},
		{
			name:             "no public IP with custom network - valid",
			networkType:      "custom",
			allocatePublicIP: false,
			expectError:      false,
		},
		{
			name:             "public IP with custom network - invalid",
			networkType:      "custom",
			allocatePublicIP: true,
			expectError:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			networkID := "network-test-123"

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == "GET" && strings.Contains(r.URL.Path, "/networks/"+networkID) {
					response := map[string]any{
						"success": true,
						"message": "Network retrieved",
						"data": map[string]any{
							"id":          networkID,
							"name":        "test-network",
							"networkType": tt.networkType,
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
			model := createTestVMModel("test-vm", "Alma Linux 8.x", tt.allocatePublicIP)

			// Set network IDs
			networkIds := []string{networkID}
			model.NetworkIds, _ = types.ListValueFrom(ctx, types.StringType, networkIds)

			err := ValidatePublicIpValue(httpClient, ctx, model)

			if tt.expectError && err == nil {
				t.Error("Expected error but got none")
			}

			if !tt.expectError && err != nil {
				t.Errorf("Expected no error but got: %v", err)
			}

			if tt.expectError && err != nil {
				// Check for either error message format
				expectedErrMsg1 := "allocate_public_ip cannot be set to true when attaching a network of type custom"
				expectedErrMsg2 := "allocatePublicIp can only be set to true if the primary network's network_type is standard"
				if !strings.Contains(err.Error(), expectedErrMsg1) && !strings.Contains(err.Error(), expectedErrMsg2) {
					t.Errorf("Expected error to contain custom network validation message, got '%s'", err.Error())
				}
			}
		})
	}
}

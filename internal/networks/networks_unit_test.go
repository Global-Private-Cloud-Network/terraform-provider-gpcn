package networks

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"terraform-provider-gpcn/internal/testutil"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

// Helper function to create a mock ResourceModel for testing
func createTestResourceModel(networkType, cidr, dhcpStart, dhcpEnd, dnsServers string) ResourceModel {
	model := ResourceModel{
		Name:         types.StringValue("test-network"),
		Description:  types.StringValue("Test network"),
		NetworkType:  types.StringValue(networkType),
		DatacenterId: types.StringValue("datacenter-123"),
	}

	if cidr != "" {
		model.CIDRBlock = types.StringValue(cidr)
	} else {
		model.CIDRBlock = types.StringNull()
	}

	if dhcpStart != "" {
		model.DHCPStartAddress = types.StringValue(dhcpStart)
	} else {
		model.DHCPStartAddress = types.StringNull()
	}

	if dhcpEnd != "" {
		model.DHCPEndAddress = types.StringValue(dhcpEnd)
	} else {
		model.DHCPEndAddress = types.StringNull()
	}

	if dnsServers != "" {
		model.DNSServers = types.StringValue(dnsServers)
	} else {
		model.DNSServers = types.StringNull()
	}

	return model
}

// TestMapNetworkResponseToModel tests the mapping function
func TestMapNetworkResponseToModel_Unit(t *testing.T) {
	ctx := context.Background()

	createdAt := time.Now().Format(time.RFC3339)
	updatedAt := time.Now().Add(1 * time.Hour).Format(time.RFC3339)

	response := &readNetworkResponse{
		Success: true,
		Message: "Network retrieved successfully",
	}
	response.Data.ID = "network-123"
	response.Data.Name = "test-network"
	response.Data.Description = "Test network description"
	response.Data.CreatedAt = createdAt
	response.Data.UpdatedAt = updatedAt
	response.Data.SNAT = "true"
	response.Data.CIDRBlock = "10.0.0.0/24"
	response.Data.Gateway = "10.0.0.1"
	response.Data.ConnectedVMs = "2"
	response.Data.NetworkType = "standard"
	response.Data.Country = readNetworkDataLocationResponse{
		ID:   1,
		Name: "United States",
	}
	response.Data.Region = readNetworkDataLocationResponse{
		ID:   10,
		Name: "US-East",
	}
	response.Data.Datacenter.ID = "dc-123"
	response.Data.Datacenter.Name = "US-East-1"
	response.Data.DNSServers = "8.8.8.8, 8.8.4.4"
	response.Data.AllocationPools = []struct {
		Start string `json:"start"`
		End   string `json:"end"`
	}{
		{Start: "10.0.0.10", End: "10.0.0.254"},
	}

	model := createTestResourceModel("standard", "10.0.0.0/24", "10.0.0.10", "10.0.0.254", "8.8.8.8, 8.8.4.4")

	result := MapNetworkResponseToModel(ctx, response, model)

	// Verify all fields are mapped correctly
	if result.ID.ValueString() != "network-123" {
		t.Errorf("Expected ID 'network-123', got '%s'", result.ID.ValueString())
	}

	if result.Description.ValueString() != "Test network description" {
		t.Errorf("Expected description 'Test network description', got '%s'", result.Description.ValueString())
	}

	if result.SNAT.ValueString() != "true" {
		t.Errorf("Expected SNAT 'true', got '%s'", result.SNAT.ValueString())
	}

	if result.CIDRBlock.ValueString() != "10.0.0.0/24" {
		t.Errorf("Expected CIDR block '10.0.0.0/24', got '%s'", result.CIDRBlock.ValueString())
	}

	if result.Gateway.ValueString() != "10.0.0.1" {
		t.Errorf("Expected gateway '10.0.0.1', got '%s'", result.Gateway.ValueString())
	}

	if result.ConnectedVMs.ValueString() != "2" {
		t.Errorf("Expected connected VMs '2', got '%s'", result.ConnectedVMs.ValueString())
	}

	if result.DNSServers.ValueString() != "8.8.8.8, 8.8.4.4" {
		t.Errorf("Expected DNS servers '8.8.8.8, 8.8.4.4', got '%s'", result.DNSServers.ValueString())
	}

	if result.DHCPStartAddress.ValueString() != "10.0.0.10" {
		t.Errorf("Expected DHCP start '10.0.0.10', got '%s'", result.DHCPStartAddress.ValueString())
	}

	if result.DHCPEndAddress.ValueString() != "10.0.0.254" {
		t.Errorf("Expected DHCP end '10.0.0.254', got '%s'", result.DHCPEndAddress.ValueString())
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

// TestMapNetworkResponseToModel_CustomNetwork tests mapping for custom network type
func TestMapNetworkResponseToModel_CustomNetwork_Unit(t *testing.T) {
	ctx := context.Background()

	response := &readNetworkResponse{
		Success: true,
		Message: "Network retrieved successfully",
	}
	response.Data.ID = "network-456"
	response.Data.Name = "custom-network"
	response.Data.Description = "Custom network"
	response.Data.CreatedAt = time.Now().Format(time.RFC3339)
	response.Data.UpdatedAt = time.Now().Format(time.RFC3339)
	response.Data.SNAT = "false"
	response.Data.CIDRBlock = ""
	response.Data.Gateway = ""
	response.Data.ConnectedVMs = "0"
	response.Data.NetworkType = "custom"
	response.Data.Country = readNetworkDataLocationResponse{
		ID:   1,
		Name: "United States",
	}
	response.Data.Region = readNetworkDataLocationResponse{
		ID:   10,
		Name: "US-East",
	}
	response.Data.Datacenter.ID = "dc-123"
	response.Data.Datacenter.Name = "US-East-1"
	response.Data.DNSServers = ""
	response.Data.AllocationPools = []struct {
		Start string `json:"start"`
		End   string `json:"end"`
	}{}

	model := createTestResourceModel("custom", "", "", "", "")

	result := MapNetworkResponseToModel(ctx, response, model)

	// Verify ID is set
	if result.ID.ValueString() != "network-456" {
		t.Errorf("Expected ID 'network-456', got '%s'", result.ID.ValueString())
	}

	// Verify SNAT is false for custom network
	if result.SNAT.ValueString() != "false" {
		t.Errorf("Expected SNAT 'false' for custom network, got '%s'", result.SNAT.ValueString())
	}
}

// TestCreateNetwork_MockHTTP tests CreateNetwork with a mock HTTP server
func TestCreateNetwork_MockHTTP(t *testing.T) {
	jobID := "job-123"
	networkID := "network-456"

	// Track which endpoints were called
	var createCalled, jobStatusCalled, getCalled bool

	server, httpClient := testutil.SetupMockServer(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/networks/") {
				createCalled = true
				testutil.HandleCreateJobResponse(w, jobID, "Network creation job started")
			} else if r.Method == "POST" && strings.Contains(r.URL.Path, "/jobs") {
				jobStatusCalled = true
				testutil.HandleJobResponse(w, jobID, networkID, true)
			} else if r.Method == "GET" && strings.Contains(r.URL.Path, "/networks/"+networkID) {
				getCalled = true
				// Return network details
				response := readNetworkResponse{
					Success: true,
					Message: "Network retrieved",
				}
				response.Data.ID = networkID
				response.Data.Name = "test-network"
				response.Data.Description = "Test network"
				response.Data.CreatedAt = time.Now().Format(time.RFC3339)
				response.Data.UpdatedAt = time.Now().Format(time.RFC3339)
				response.Data.SNAT = "true"
				response.Data.CIDRBlock = "10.0.0.0/24"
				response.Data.Gateway = "10.0.0.1"
				response.Data.ConnectedVMs = "0"
				response.Data.NetworkType = "standard"
				response.Data.Country = readNetworkDataLocationResponse{
					ID:   1,
					Name: "United States",
				}
				response.Data.Region = readNetworkDataLocationResponse{
					ID:   10,
					Name: "US-East",
				}
				response.Data.Datacenter.ID = "dc-123"
				response.Data.Datacenter.Name = "US-East-1"
				response.Data.DNSServers = "8.8.8.8, 8.8.4.4"
				response.Data.AllocationPools = []struct {
					Start string `json:"start"`
					End   string `json:"end"`
				}{
					{Start: "10.0.0.10", End: "10.0.0.254"},
				}
				testutil.WriteJSONResponse(w, response)
			} else {
				testutil.LogUnexpectedRequest(t, w, r)
			}
		},
	})
	defer server.Close()

	ctx := context.Background()
	model := createTestResourceModel("standard", "10.0.0.0/24", "10.0.0.10", "10.0.0.254", "8.8.8.8, 8.8.4.4")

	response, err := CreateNetwork(httpClient, ctx, model)

	if err != nil {
		t.Fatalf("CreateNetwork failed: %v", err)
	}

	if response == nil {
		t.Fatal("Expected response, got nil")
	}

	if response.Data.ID != networkID {
		t.Errorf("Expected network ID '%s', got '%s'", networkID, response.Data.ID)
	}

	// Verify all endpoints were called
	if !createCalled {
		t.Error("Expected create endpoint to be called")
	}
	if !jobStatusCalled {
		t.Error("Expected job status endpoint to be called")
	}
	if !getCalled {
		t.Error("Expected get network endpoint to be called")
	}
}

// TestGetNetwork_MockHTTP tests GetNetwork with a mock HTTP server
func TestGetNetwork_MockHTTP(t *testing.T) {
	networkID := "network-789"

	server, httpClient := testutil.SetupMockServer(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "GET" && strings.Contains(r.URL.Path, "/networks/"+networkID) {
				response := readNetworkResponse{
					Success: true,
					Message: "Network retrieved",
				}
				response.Data.ID = networkID
				response.Data.Name = "test-network"
				response.Data.Description = "Test network"
				response.Data.CreatedAt = time.Now().Format(time.RFC3339)
				response.Data.UpdatedAt = time.Now().Format(time.RFC3339)
				response.Data.SNAT = "true"
				response.Data.CIDRBlock = "10.0.0.0/24"
				response.Data.Gateway = "10.0.0.1"
				response.Data.ConnectedVMs = "0"
				response.Data.NetworkType = "standard"
				response.Data.Country = readNetworkDataLocationResponse{
					ID:   1,
					Name: "United States",
				}
				response.Data.Region = readNetworkDataLocationResponse{
					ID:   10,
					Name: "US-East",
				}
				response.Data.Datacenter.ID = "dc-123"
				response.Data.Datacenter.Name = "US-East-1"
				response.Data.DNSServers = "8.8.8.8, 8.8.4.4"
				response.Data.AllocationPools = []struct {
					Start string `json:"start"`
					End   string `json:"end"`
				}{
					{Start: "10.0.0.10", End: "10.0.0.254"},
				}
				testutil.WriteJSONResponse(w, response)
			} else {
				testutil.LogUnexpectedRequest(t, w, r)
			}
		},
	})
	defer server.Close()

	ctx := context.Background()

	response, err := GetNetwork(httpClient, ctx, networkID)

	if err != nil {
		t.Fatalf("GetNetwork failed: %v", err)
	}

	if response == nil {
		t.Fatal("Expected response, got nil")
	}

	if response.Data.ID != networkID {
		t.Errorf("Expected network ID '%s', got '%s'", networkID, response.Data.ID)
	}
}

// TestUpdateNetwork_MockHTTP tests UpdateNetwork with a mock HTTP server
func TestUpdateNetwork_MockHTTP(t *testing.T) {
	networkID := "network-update-123"

	var updateCalled, getCalled bool

	server, httpClient := testutil.SetupMockServer(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "PUT" && strings.Contains(r.URL.Path, "/networks/"+networkID) {
				updateCalled = true

				// Verify request body contains expected fields
				requestBody := testutil.ReadRequestBody(r)

				if requestBody["name"] != "updated-network" {
					t.Errorf("Expected name 'updated-network', got '%v'", requestBody["name"])
				}

				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				testutil.WriteJSONResponse(w, map[string]bool{"success": true})
			} else if r.Method == "GET" && strings.Contains(r.URL.Path, "/networks/"+networkID) {
				getCalled = true
				response := readNetworkResponse{
					Success: true,
					Message: "Network retrieved",
				}
				response.Data.ID = networkID
				response.Data.Name = "updated-network"
				response.Data.Description = "Updated description"
				response.Data.CreatedAt = time.Now().Format(time.RFC3339)
				response.Data.UpdatedAt = time.Now().Format(time.RFC3339)
				response.Data.SNAT = "true"
				response.Data.CIDRBlock = "10.0.0.0/24"
				response.Data.Gateway = "10.0.0.1"
				response.Data.ConnectedVMs = "0"
				response.Data.NetworkType = "standard"
				response.Data.Country = readNetworkDataLocationResponse{
					ID:   1,
					Name: "United States",
				}
				response.Data.Region = readNetworkDataLocationResponse{
					ID:   10,
					Name: "US-East",
				}
				response.Data.Datacenter.ID = "dc-123"
				response.Data.Datacenter.Name = "US-East-1"
				response.Data.DNSServers = "8.8.8.8, 8.8.4.4"
				response.Data.AllocationPools = []struct {
					Start string `json:"start"`
					End   string `json:"end"`
				}{
					{Start: "10.0.0.10", End: "10.0.0.254"},
				}
				testutil.WriteJSONResponse(w, response)
			} else {
				testutil.LogUnexpectedRequest(t, w, r)
			}
		},
	})
	defer server.Close()

	ctx := context.Background()
	model := createTestResourceModel("standard", "10.0.0.0/24", "10.0.0.10", "10.0.0.254", "8.8.8.8, 8.8.4.4")
	model.ID = types.StringValue(networkID)
	model.Name = types.StringValue("updated-network")
	model.Description = types.StringValue("Updated description")

	response, err := UpdateNetwork(httpClient, ctx, networkID, model)

	if err != nil {
		t.Fatalf("UpdateNetwork failed: %v", err)
	}

	if response == nil {
		t.Fatal("Expected response, got nil")
	}

	if response.Data.Name != "updated-network" {
		t.Errorf("Expected network name 'updated-network', got '%s'", response.Data.Name)
	}

	if !updateCalled {
		t.Error("Expected update endpoint to be called")
	}
	if !getCalled {
		t.Error("Expected get network endpoint to be called")
	}
}

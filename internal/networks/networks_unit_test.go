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

const testDatacenterID = "datacenter-123"

func createTestResourceModel(networkType, cidr, dhcpStart, dhcpEnd, dnsServers string) ResourceModel {
	model := ResourceModel{
		Name:         types.StringValue("test-network"),
		Description:  types.StringValue("Test network"),
		NetworkType:  types.StringValue(networkType),
		DatacenterId: types.StringValue(testDatacenterID),
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

func newNetworkResponse(id, name, networkType string) *readNetworkResponse {
	resp := &readNetworkResponse{Success: true, Message: "Network retrieved"}
	resp.Data.ID = id
	resp.Data.Name = name
	resp.Data.Description = "Test network description"
	resp.Data.CreatedAt = time.Now().Format(time.RFC3339)
	resp.Data.UpdatedAt = time.Now().Format(time.RFC3339)
	resp.Data.SNAT = "true"
	resp.Data.CIDRBlock = "10.0.0.0/24"
	resp.Data.Gateway = "10.0.0.1"
	resp.Data.ConnectedVMs = "0"
	resp.Data.NetworkType = networkType
	resp.Data.Datacenter.ID = "dc-123"
	resp.Data.Datacenter.Name = "US-East-1"
	resp.Data.Datacenter.Region = "East"
	resp.Data.Datacenter.Country = "United States"
	resp.Data.Datacenter.CountryAbbr = "US"
	resp.Data.DNSServers = "8.8.8.8, 8.8.4.4"
	resp.Data.AllocationPools = []struct {
		Start string `json:"start"`
		End   string `json:"end"`
	}{{Start: "10.0.0.10", End: "10.0.0.254"}}
	return resp
}

func TestMapNetworkResponseToModelUnit(t *testing.T) {
	response := newNetworkResponse("network-123", "test-network", "standard")
	response.Data.ConnectedVMs = "2"
	model := createTestResourceModel("standard", "10.0.0.0/24", "10.0.0.10", "10.0.0.254", "8.8.8.8, 8.8.4.4")

	result := MapNetworkResponseToModel(context.Background(), response, model)

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
	if result.CreatedTime.IsNull() || result.CreatedTime.ValueString() == "unknown" {
		t.Errorf("Expected CreatedTime to be set, got '%s'", result.CreatedTime.ValueString())
	}
	if result.LastUpdated.IsNull() || result.LastUpdated.ValueString() == "unknown" {
		t.Errorf("Expected LastUpdated to be set, got '%s'", result.LastUpdated.ValueString())
	}
	if result.Location.IsNull() {
		t.Error("Expected Location to be set")
	}
}

func TestMapNetworkResponseToModelCustomNetworkUnit(t *testing.T) {
	response := newNetworkResponse("network-456", "custom-network", "custom")
	response.Data.Description = "Custom network"
	response.Data.SNAT = "false"
	response.Data.CIDRBlock = ""
	response.Data.Gateway = ""
	response.Data.DNSServers = ""
	response.Data.AllocationPools = nil
	model := createTestResourceModel("custom", "", "", "", "")

	result := MapNetworkResponseToModel(context.Background(), response, model)

	if result.ID.ValueString() != "network-456" {
		t.Errorf("Expected ID 'network-456', got '%s'", result.ID.ValueString())
	}
	if result.SNAT.ValueString() != "false" {
		t.Errorf("Expected SNAT 'false' for custom network, got '%s'", result.SNAT.ValueString())
	}
}

func TestCreateNetworkMockHTTP(t *testing.T) {
	const (
		jobID     = "job-123"
		networkID = "network-456"
	)

	var createCalled, jobStatusCalled, getCalled bool

	server, gpcnClient := testutil.SetupMockServerWithGpcnClient(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/networks/"):
				createCalled = true
				testutil.HandleCreateJobResponse(w, jobID, "Network creation job started")

			case r.Method == "POST" && strings.Contains(r.URL.Path, "/jobs"):
				jobStatusCalled = true
				testutil.HandleJobResponse(w, jobID, networkID, true)

			case r.Method == "GET" && strings.Contains(r.URL.Path, "/networks/"+networkID):
				getCalled = true
				testutil.WriteJSONResponse(w, newNetworkResponse(networkID, "test-network", "standard"))

			default:
				testutil.LogUnexpectedRequest(t, w, r)
			}
		},
	})
	defer server.Close()

	model := createTestResourceModel("standard", "10.0.0.0/24", "10.0.0.10", "10.0.0.254", "8.8.8.8, 8.8.4.4")

	response, err := CreateNetwork(gpcnClient, context.Background(), model)
	if err != nil {
		t.Fatalf("CreateNetwork failed: %v", err)
	}
	if response == nil {
		t.Fatal("Expected response, got nil")
		return
	}
	if response.Data.ID != networkID {
		t.Errorf("Expected network ID '%s', got '%s'", networkID, response.Data.ID)
	}
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

func TestGetNetworkMockHTTP(t *testing.T) {
	const networkID = "network-789"

	server, gpcnClient := testutil.SetupMockServerWithGpcnClient(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "GET" && strings.Contains(r.URL.Path, "/networks/"+networkID) {
				testutil.WriteJSONResponse(w, newNetworkResponse(networkID, "test-network", "standard"))
			} else {
				testutil.LogUnexpectedRequest(t, w, r)
			}
		},
	})
	defer server.Close()

	response, err := GetNetwork(gpcnClient, context.Background(), networkID)
	if err != nil {
		t.Fatalf("GetNetwork failed: %v", err)
	}
	if response == nil {
		t.Fatal("Expected response, got nil")
		return
	}
	if response.Data.ID != networkID {
		t.Errorf("Expected network ID '%s', got '%s'", networkID, response.Data.ID)
	}
}

func TestUpdateNetworkMockHTTP(t *testing.T) {
	const networkID = "network-update-123"

	var updateCalled, getCalled bool

	server, gpcnClient := testutil.SetupMockServerWithGpcnClient(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.Method == "PUT" && strings.Contains(r.URL.Path, "/networks/"+networkID):
				updateCalled = true
				requestBody := testutil.ReadRequestBody(r)
				if requestBody["name"] != "updated-network" {
					t.Errorf("Expected name 'updated-network', got '%v'", requestBody["name"])
				}
				testutil.WriteJSONResponse(w, map[string]bool{"success": true})

			case r.Method == "GET" && strings.Contains(r.URL.Path, "/networks/"+networkID):
				getCalled = true
				resp := newNetworkResponse(networkID, "updated-network", "standard")
				resp.Data.Description = "Updated description"
				testutil.WriteJSONResponse(w, resp)

			default:
				testutil.LogUnexpectedRequest(t, w, r)
			}
		},
	})
	defer server.Close()

	model := createTestResourceModel("standard", "10.0.0.0/24", "10.0.0.10", "10.0.0.254", "8.8.8.8, 8.8.4.4")
	model.ID = types.StringValue(networkID)
	model.Name = types.StringValue("updated-network")
	model.Description = types.StringValue("Updated description")

	response, err := UpdateNetwork(gpcnClient, context.Background(), networkID, model)
	if err != nil {
		t.Fatalf("UpdateNetwork failed: %v", err)
	}
	if response == nil {
		t.Fatal("Expected response, got nil")
		return
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

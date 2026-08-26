package networks

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"terraform-provider-gpcn/internal/client"
	"terraform-provider-gpcn/internal/testutil"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// TestMain decreases the interval between network delete retries. The default
// value of 5 seconds adds only delay to each test of the retry loop.
func TestMain(m *testing.M) {
	DELETE_NETWORK_RETRY_INTERVAL = 10 * time.Millisecond
	os.Exit(m.Run())
}

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
		parts := strings.Split(dnsServers, ", ")
		elements := make([]attr.Value, len(parts))
		for i, p := range parts {
			elements[i] = types.StringValue(p)
		}
		model.DNSServers = types.ListValueMust(types.StringType, elements)
	} else {
		model.DNSServers = types.ListNull(types.StringType)
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
	if result.DNSServers.IsNull() || len(result.DNSServers.Elements()) != 2 {
		t.Errorf("Expected 2 DNS server elements, got %d", len(result.DNSServers.Elements()))
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

// TestDeleteNetworkStopsRetryingOnContextCancellation verifies that the wait
// between delete retries is interruptible. DeleteNetwork retries the job up to
// DELETE_NETWORK_RETRY_COUNT times with a 5-second wait between attempts, so a
// bare sleep here keeps a canceled destroy running for another 20 seconds.
func TestDeleteNetworkStopsRetryingOnContextCancellation(t *testing.T) {
	const networkID = "network-cancel-123"

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	server, gpcnClient := testutil.SetupMockServerWithGpcnClient(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/jobs"):
				// Report the delete job as failed so DeleteNetwork retries, and
				// interrupt the run while that first attempt is in flight.
				cancel()
				testutil.WriteJSONResponse(w, map[string]any{
					"success": true,
					"data":    map[string]any{"jobs": []any{map[string]any{"jobId": "job-1", "hasFailed": true}}},
				})
			case r.Method == http.MethodDelete:
				testutil.WriteJSONResponse(w, map[string]any{
					"success": true,
					"data":    map[string]any{"jobId": "job-1"},
				})
			default:
				// No virtual machines attached to this network.
				testutil.WriteJSONResponse(w, map[string]any{"success": true, "data": []any{}})
			}
		},
	})
	defer server.Close()

	start := time.Now()
	err := DeleteNetwork(gpcnClient, ctx, networkID)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected an error after cancellation, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("error = %v, want it to wrap context.Canceled", err)
	}
	if elapsed > 1*time.Second {
		t.Errorf("DeleteNetwork took %s after cancellation, expected it to abandon the retry wait", elapsed)
	}
}

// An interruption during the retry interval must not remove the delete error.
// That error is the reason for the retry, and the operator needs it.
func TestDeleteNetworkInterruptedReportsLastError(t *testing.T) {
	const networkID = "network-lasterr-123"

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Increase the interval, so that the cancellation happens during the interval
	// and not during a request.
	original := DELETE_NETWORK_RETRY_INTERVAL
	DELETE_NETWORK_RETRY_INTERVAL = 2 * time.Second
	defer func() { DELETE_NETWORK_RETRY_INTERVAL = original }()

	var jobPolls int
	server, gpcnClient := testutil.SetupMockServerWithGpcnClient(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/jobs"):
				jobPolls++
				// The delete job fails. DeleteNetwork waits and tries again.
				testutil.WriteJSONResponse(w, map[string]any{
					"success": true,
					"data":    map[string]any{"jobs": []any{map[string]any{"jobId": "job-1", "hasFailed": true}}},
				})
				if jobPolls == 1 {
					// Interrupt the run during the interval.
					go func() {
						time.Sleep(100 * time.Millisecond)
						cancel()
					}()
				}
			case r.Method == http.MethodDelete:
				testutil.WriteJSONResponse(w, map[string]any{
					"success": true,
					"data":    map[string]any{"jobId": "job-1"},
				})
			default:
				testutil.WriteJSONResponse(w, map[string]any{"success": true, "data": []any{}})
			}
		},
	})
	defer server.Close()

	err := DeleteNetwork(gpcnClient, ctx, networkID)
	if err == nil {
		t.Fatal("expected an error after cancellation, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("error = %v, want it to wrap context.Canceled", err)
	}
	if !errors.Is(err, client.ErrJobFailed) {
		t.Errorf("error = %v, want it to also report the delete failure it was retrying", err)
	}
}

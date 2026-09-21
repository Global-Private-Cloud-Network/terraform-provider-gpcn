package networks

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"terraform-provider-gpcn/internal/testutil"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
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
	if result.GatewayIP.ValueString() != "10.0.0.1" {
		t.Errorf("Expected gateway_ip '10.0.0.1', got '%s'", result.GatewayIP.ValueString())
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

func TestGetNetworkInterfacesSortsByInterfaceIndex(t *testing.T) {
	const vmID = "vm-sort-123"

	server, gpcnClient := testutil.SetupMockServerWithGpcnClient(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "GET" && strings.Contains(r.URL.Path, "/network-interfaces") {
				// Return the interfaces out of order to prove the sort
				testutil.WriteJSONResponse(w, map[string]any{
					"success": true, "message": "Network interfaces retrieved",
					"data": []map[string]any{
						{"id": "interface-c", "networkInterface": 2, "networkId": "network-c"},
						{"id": "interface-a", "networkInterface": 0, "networkId": "network-a"},
						{"id": "interface-b", "networkInterface": 1, "networkId": "network-b"},
					},
				})
			} else {
				testutil.LogUnexpectedRequest(t, w, r)
			}
		},
	})
	defer server.Close()

	interfaces, err := GetNetworkInterfaces(gpcnClient, context.Background(), vmID)
	if err != nil {
		t.Fatalf("GetNetworkInterfaces failed: %v", err)
	}

	want := []int64{0, 1, 2}
	if len(interfaces) != len(want) {
		t.Fatalf("Expected %d interfaces, got %d", len(want), len(interfaces))
	}
	for i, w := range want {
		if got := interfaces[i].NetworkInterface.ValueInt64(); got != w {
			t.Errorf("Interface at index %d: expected NetworkInterface %d, got %d", i, w, got)
		}
	}
}

// A VPC interface leaves every legacy column null, and an unfinished reservation leaves
// the MAC and the addresses null too. The provider must keep those nulls, because an
// empty string reads as a value the platform never sent.
func TestGetNetworkInterfacesKeepsNullsNullUnit(t *testing.T) {
	const vmID = "vm-vpc-nulls"

	server, gpcnClient := testutil.SetupMockServerWithGpcnClient(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "GET" && strings.Contains(r.URL.Path, "/network-interfaces") {
				testutil.WriteJSONResponse(w, map[string]any{
					"success": true, "message": "Network interfaces retrieved",
					"data": []map[string]any{{
						"id":               "interface-vpc",
						"networkInterface": 1,
						"isPrimary":        1,
						"macAddress":       nil,
						"publicIp":         nil,
						"publicIpId":       nil,
						"privateIp":        "10.20.0.7",
						"world":            "vpc",
						"networkName":      nil,
						"networkId":        nil,
						"cidrBlock":        "10.20.0.0/24",
						"gatewayIp":        nil,
						"networkType":      nil,
						"vpcSubnetId":      "subnet-1",
						"subnetName":       "web",
						"vpcId":            "vpc-1",
						"vpcName":          "prod",
						"l2SegmentId":      nil,
						"l2SegmentName":    nil,
					}},
				})
			} else {
				testutil.LogUnexpectedRequest(t, w, r)
			}
		},
	})
	defer server.Close()

	interfaces, err := GetNetworkInterfaces(gpcnClient, context.Background(), vmID)
	if err != nil {
		t.Fatalf("GetNetworkInterfaces failed: %v", err)
	}
	if len(interfaces) != 1 {
		t.Fatalf("Expected 1 interface, got %d", len(interfaces))
	}
	got := interfaces[0]

	nullFields := map[string]types.String{
		"mac_address":     got.MacAddress,
		"public_ip":       got.PublicIP,
		"public_ip_id":    got.PublicIPID,
		"network_name":    got.NetworkName,
		"network_id":      got.NetworkID,
		"gateway_ip":      got.GatewayIP,
		"network_type":    got.NetworkType,
		"l2_segment_id":   got.L2SegmentID,
		"l2_segment_name": got.L2SegmentName,
	}
	for name, value := range nullFields {
		if !value.IsNull() {
			t.Errorf("Expected %s to stay null, got '%s'", name, value.ValueString())
		}
	}

	setFields := map[string][2]string{
		"private_ip":    {got.PrivateIP.ValueString(), "10.20.0.7"},
		"world":         {got.World.ValueString(), "vpc"},
		"cidr_block":    {got.CIDRBlock.ValueString(), "10.20.0.0/24"},
		"vpc_subnet_id": {got.VpcSubnetID.ValueString(), "subnet-1"},
		"subnet_name":   {got.SubnetName.ValueString(), "web"},
		"vpc_id":        {got.VpcID.ValueString(), "vpc-1"},
		"vpc_name":      {got.VpcName.ValueString(), "prod"},
	}
	for name, pair := range setFields {
		if pair[0] != pair[1] {
			t.Errorf("Expected %s to be '%s', got '%s'", name, pair[1], pair[0])
		}
	}
	if !got.IsPrimary.ValueBool() {
		t.Error("Expected is_primary to be true for the integer 1")
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

func TestFirstUsableHostFromCIDR(t *testing.T) {
	cases := []struct {
		cidr string
		want string
	}{
		{"10.0.0.0/24", "10.0.0.1"},
		{"192.168.5.0/24", "192.168.5.1"},
		{"10.20.0.0/22", "10.20.0.1"},
	}
	for _, c := range cases {
		got, err := firstUsableHostFromCIDR(c.cidr)
		if err != nil {
			t.Errorf("firstUsableHostFromCIDR(%q) returned error: %v", c.cidr, err)
			continue
		}
		if got != c.want {
			t.Errorf("firstUsableHostFromCIDR(%q) = %q, want %q", c.cidr, got, c.want)
		}
	}
	if _, err := firstUsableHostFromCIDR("not-a-cidr"); err == nil {
		t.Error("Expected error for invalid CIDR, got nil")
	}
}

// TestCreateNetworkDefaultRoute confirms the POST body sends gateway_ip as defaultRoute,
// and sets defaultRouteEnabled from whether a value is present. The DefaultRouteFromCIDR
// plan modifier resolves the value before create, so create sends it unchanged.
func TestCreateNetworkDefaultRoute(t *testing.T) {
	const (
		jobID     = "job-dr"
		networkID = "network-dr"
	)
	cases := []struct {
		name        string
		gatewayIP   types.String
		wantRoute   string
		wantEnabled bool
	}{
		{"value present", types.StringValue("192.168.5.1"), "192.168.5.1", true},
		{"value absent", types.StringNull(), "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var gotDefaultRoute any
			var gotEnabled any
			server, gpcnClient := testutil.SetupMockServerWithGpcnClient(testutil.MockServerConfig{
				T: t,
				Handler: func(w http.ResponseWriter, r *http.Request) {
					switch {
					case r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/networks/"):
						body := testutil.ReadRequestBody(r)
						gotDefaultRoute = body["defaultRoute"]
						gotEnabled = body["defaultRouteEnabled"]
						testutil.HandleCreateJobResponse(w, jobID, "Network creation job started")
					case r.Method == "POST" && strings.Contains(r.URL.Path, "/jobs"):
						testutil.HandleJobResponse(w, jobID, networkID, true)
					case r.Method == "GET" && strings.Contains(r.URL.Path, "/networks/"+networkID):
						testutil.WriteJSONResponse(w, newNetworkResponse(networkID, "test-network", "standard"))
					default:
						testutil.LogUnexpectedRequest(t, w, r)
					}
				},
			})
			defer server.Close()

			model := createTestResourceModel("standard", "192.168.5.0/24", "10.0.0.10", "10.0.0.254", "8.8.8.8, 8.8.4.4")
			model.GatewayIP = c.gatewayIP

			if _, err := CreateNetwork(gpcnClient, context.Background(), model); err != nil {
				t.Fatalf("CreateNetwork failed: %v", err)
			}
			if gotDefaultRoute != c.wantRoute {
				t.Errorf("Expected defaultRoute '%s', got '%v'", c.wantRoute, gotDefaultRoute)
			}
			if gotEnabled != c.wantEnabled {
				t.Errorf("Expected defaultRouteEnabled %v, got '%v'", c.wantEnabled, gotEnabled)
			}
		})
	}
}

func TestRemoveNetworkInterfaceByNetworkIdMissingInterface(t *testing.T) {
	const (
		vmID      = "vm-missing-123"
		networkID = "network-not-attached"
	)

	server, gpcnClient := testutil.SetupMockServerWithGpcnClient(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/network-interfaces") {
				testutil.WriteJSONResponse(w, map[string]any{
					"success": true, "message": "Network interfaces retrieved",
					"data": []map[string]any{
						{"id": "interface-a", "networkInterface": 0, "networkId": "network-a"},
					},
				})
			} else {
				testutil.LogUnexpectedRequest(t, w, r)
			}
		},
	})
	defer server.Close()

	err := RemoveNetworkInterfaceByNetworkId(gpcnClient, context.Background(), vmID, networkID)
	if err == nil {
		t.Fatal("Expected an error when the network has no matching interface, got nil")
	}
	if !strings.Contains(err.Error(), networkID) {
		t.Errorf("Expected error to name network ID '%s', got '%s'", networkID, err.Error())
	}
	if strings.Contains(err.Error(), "%s") {
		t.Errorf("Expected error to have no unfilled format verb, got '%s'", err.Error())
	}
}

func TestRefreshNetworkModelFromResponseAfterMapUnit(t *testing.T) {
	response := newNetworkResponse("network-123", "renamed-in-portal", "standard")
	model := createTestResourceModel("custom", "10.0.0.0/24", "10.0.0.10", "10.0.0.254", "8.8.8.8, 8.8.4.4")

	result := RefreshNetworkModelFromResponse(response, MapNetworkResponseToModel(context.Background(), response, model))

	if result.Name.ValueString() != "renamed-in-portal" {
		t.Errorf("Expected name 'renamed-in-portal', got '%s'", result.Name.ValueString())
	}
	if result.DatacenterId.ValueString() != testDatacenterID {
		t.Errorf("Expected datacenter ID '%s', got '%s'", testDatacenterID, result.DatacenterId.ValueString())
	}
	if result.NetworkType.ValueString() != "custom" {
		t.Errorf("Expected network type 'custom', got '%s'", result.NetworkType.ValueString())
	}
}

func TestRefreshNetworkModelFromResponseKeepsValuesOnEmptyUnit(t *testing.T) {
	response := newNetworkResponse("network-123", "", "standard")
	model := createTestResourceModel("custom", "10.0.0.0/24", "10.0.0.10", "10.0.0.254", "8.8.8.8, 8.8.4.4")

	result := RefreshNetworkModelFromResponse(response, model)

	if result.Name.ValueString() != "test-network" {
		t.Errorf("Expected name 'test-network', got '%s'", result.Name.ValueString())
	}
	if result.DatacenterId.ValueString() != testDatacenterID {
		t.Errorf("Expected datacenter ID '%s', got '%s'", testDatacenterID, result.DatacenterId.ValueString())
	}
	if result.NetworkType.ValueString() != "custom" {
		t.Errorf("Expected network type 'custom', got '%s'", result.NetworkType.ValueString())
	}
}

func TestMapNetworkResponseToModelKeepsPlanValuesUnit(t *testing.T) {
	response := newNetworkResponse("network-123", "renamed-in-portal", "standard")
	model := createTestResourceModel("custom", "10.0.0.0/24", "10.0.0.10", "10.0.0.254", "8.8.8.8, 8.8.4.4")

	result := MapNetworkResponseToModel(context.Background(), response, model)

	if result.Name.ValueString() != "test-network" {
		t.Errorf("Expected name 'test-network', got '%s'", result.Name.ValueString())
	}
	if result.DatacenterId.ValueString() != testDatacenterID {
		t.Errorf("Expected datacenter ID '%s', got '%s'", testDatacenterID, result.DatacenterId.ValueString())
	}
	if result.NetworkType.ValueString() != "custom" {
		t.Errorf("Expected network type 'custom', got '%s'", result.NetworkType.ValueString())
	}
}

// TestUpdateNetworkOmitsEmptyCIDRBlockMockHTTP guards the update body against an empty
// cidrBlock. The API stores an empty value for every custom network. Its update schema
// validates the key against a CIDR pattern, so the empty value turns a rename into a 422.
func TestUpdateNetworkOmitsEmptyCIDRBlockMockHTTP(t *testing.T) {
	const networkID = "network-cidr-guard"

	cases := []struct {
		name      string
		cidrBlock types.String
		want      any
	}{
		{name: "null", cidrBlock: types.StringNull(), want: nil},
		{name: "empty", cidrBlock: types.StringValue(""), want: nil},
		{name: "set", cidrBlock: types.StringValue("10.0.0.0/24"), want: "10.0.0.0/24"},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			var putBody map[string]any

			server, gpcnClient := testutil.SetupMockServerWithGpcnClient(testutil.MockServerConfig{
				T: t,
				Handler: func(w http.ResponseWriter, r *http.Request) {
					switch {
					case r.Method == "PUT" && strings.Contains(r.URL.Path, "/networks/"+networkID):
						putBody = testutil.ReadRequestBody(r)
						testutil.WriteJSONResponse(w, map[string]bool{"success": true})
					case r.Method == "GET" && strings.Contains(r.URL.Path, "/networks/"+networkID):
						testutil.WriteJSONResponse(w, newNetworkResponse(networkID, "custom-network", "custom"))
					default:
						testutil.LogUnexpectedRequest(t, w, r)
					}
				},
			})
			defer server.Close()

			model := createTestResourceModel("custom", "", "", "", "")
			model.ID = types.StringValue(networkID)
			model.Name = types.StringValue("custom-network")
			model.CIDRBlock = testCase.cidrBlock

			if _, err := UpdateNetwork(gpcnClient, context.Background(), networkID, model); err != nil {
				t.Fatalf("UpdateNetwork failed: %v", err)
			}
			if putBody == nil {
				t.Fatal("Expected the update endpoint to be called")
			}
			got, present := putBody["cidrBlock"]
			if testCase.want == nil {
				if present {
					t.Errorf("Expected cidrBlock to be absent from the update body, got '%v'", got)
				}
				return
			}
			if !present {
				t.Fatalf("Expected cidrBlock '%v' in the update body, got no key", testCase.want)
			}
			if got != testCase.want {
				t.Errorf("Expected cidrBlock '%v', got '%v'", testCase.want, got)
			}
		})
	}
}

// TestCustomNetworkGoneWarningUnit pins the adoption warning byte for byte. The test
// harness cannot observe a warning diagnostic. This test is therefore the only guard on
// the text a user reads after the platform adopts a custom network.
func TestCustomNetworkGoneWarningUnit(t *testing.T) {
	const networkID = "network-adopted-123"

	warning := CustomNetworkGoneWarning(networkID)

	if got, want := warning.Severity(), diag.SeverityWarning; got != want {
		t.Errorf("Expected severity %v, got %v", want, got)
	}
	if got, want := warning.Summary(), "Network removed from state"; got != want {
		t.Errorf("Expected summary '%s', got '%s'", want, got)
	}
	want := "Network network-adopted-123 was not found. If it was adopted into an L2 segment by the platform, remove it from state and import the segment as gpcn_l2_segment: terraform state rm gpcn_network.<name> && terraform import gpcn_l2_segment.<name> <segment-id>."
	if got := warning.Detail(); got != want {
		t.Errorf("Expected detail '%s', got '%s'", want, got)
	}
}

package virtualmachines

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

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
)

const (
	testDatacenterID = "datacenter-123"
	testVMImage      = "Alma Linux 8.x"
)

func createTestVMModel(name, image string, allocatePublicIP bool) ResourceModel {
	size := ResourceModelSize{
		Category: types.StringValue("general"),
		Tier:     types.StringValue("g-micro-1"),
	}
	sizeObj, _ := types.ObjectValueFrom(context.Background(), size.AttrTypes(), size)

	auth := ResourceModelAuth{
		SshKeyId: types.StringValue("ssh-key-123"),
		Username: types.StringNull(),
		Password: types.StringNull(),
	}
	authObj, _ := types.ObjectValueFrom(context.Background(), auth.AttrTypes(), auth)

	return ResourceModel{
		Name:             types.StringValue(name),
		DatacenterId:     types.StringValue(testDatacenterID),
		Image:            types.StringValue(image),
		Size:             sizeObj,
		AllocatePublicIp: types.BoolValue(allocatePublicIP),
		WaitForStartup:   types.BoolValue(false),
		NetworkIds:       types.ListNull(types.StringType),
		VolumeIds:        types.ListNull(types.StringType),
		Auth:             authObj,
	}
}

func newVMResponse(id, name string) *ReadVirtualMachinesResponse {
	resp := &ReadVirtualMachinesResponse{Success: true, Message: "VM retrieved"}
	resp.Data.Status = "Running"
	resp.Data.ID = id
	resp.Data.Name = name
	resp.Data.CreatedAt = time.Now().Format(time.RFC3339)
	resp.Data.UpdatedAt = time.Now().Format(time.RFC3339)
	resp.Data.Configuration = ConfigurationResponse{
		ID:           1,
		Name:         "General - Micro - 1",
		Code:         "g-micro-1",
		CategoryCode: "general",
		CPU:          1,
		RAM:          2,
		Disk:         20,
	}
	resp.Data.Image = testVMImage
	resp.Data.Username = "admin"
	resp.Data.Datacenter.ID = testDatacenterID
	resp.Data.Datacenter.Name = "US-East-1"
	resp.Data.Datacenter.Region = "East"
	resp.Data.Datacenter.CountryAbbr = "US"
	resp.Data.Datacenter.Country = "United States"
	return resp
}

func emptyNetworkInterfacesResponse() map[string]any {
	return map[string]any{"success": true, "message": "Network interfaces retrieved", "data": []any{}}
}

func TestMapVirtualMachineResponseToModelUnit(t *testing.T) {
	server, gpcnClient := testutil.SetupMockServerWithGpcnClient(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "GET" && strings.Contains(r.URL.Path, "/network-interfaces") {
				testutil.WriteJSONResponse(w, emptyNetworkInterfacesResponse())
			}
		},
	})
	defer server.Close()

	response := newVMResponse("vm-123", "test-vm")
	model := createTestVMModel("test-vm", testVMImage, false)

	result, diags := MapVirtualMachineResponseToModel(context.Background(), gpcnClient, response, model)
	if diags.HasError() {
		t.Fatalf("Unexpected error: %v", diags)
	}

	if result.ID.ValueString() != "vm-123" {
		t.Errorf("Expected ID 'vm-123', got '%s'", result.ID.ValueString())
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
	if result.Configuration.IsNull() {
		t.Error("Expected Configuration to be set")
	}
}

func TestCreateVirtualMachineMockHTTP(t *testing.T) {
	const (
		jobID   = "job-123"
		vmID    = "vm-456"
		imageID = int64(10)
		sizeID  = int64(1)
	)

	var createCalled, jobStatusCalled, vmStatusCalled bool

	server, gpcnClient := testutil.SetupMockServerWithGpcnClient(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/virtual-machines/"):
				createCalled = true
				body, _ := io.ReadAll(r.Body)
				var req map[string]any
				_ = json.Unmarshal(body, &req)

				if req["name"] != "test-vm" {
					t.Errorf("Expected name 'test-vm', got '%v'", req["name"])
				}
				if int64(req["imageId"].(float64)) != imageID {
					t.Errorf("Expected imageId %d, got '%v'", imageID, req["imageId"])
				}

				testutil.WriteJSONResponse(w, client.JobStatusMultiResponse{
					Success: true,
					Message: "VM creation job started",
					Data: client.JobStatusDataResponse{
						Jobs: []client.JobResponse{{JobID: jobID, ResourceId: vmID}},
					},
				})

			case r.Method == "POST" && strings.Contains(r.URL.Path, "/jobs"):
				jobStatusCalled = true
				testutil.HandleJobResponse(w, jobID, vmID, true)

			case r.Method == "GET" && strings.Contains(r.URL.Path, "/virtual-machines/"+vmID):
				vmStatusCalled = true
				testutil.WriteJSONResponse(w, newVMResponse(vmID, "test-vm"))

			case r.Method == "GET" && strings.Contains(r.URL.Path, "/network-interfaces"):
				testutil.WriteJSONResponse(w, emptyNetworkInterfacesResponse())

			default:
				testutil.LogUnexpectedRequest(t, w, r)
			}
		},
	})
	defer server.Close()

	response, err := CreateVirtualMachine(gpcnClient, context.Background(), imageID, sizeID, createTestVMModel("test-vm", testVMImage, false))
	if err != nil {
		t.Fatalf("CreateVirtualMachine failed: %v", err)
	}
	if response == nil {
		t.Fatal("Expected response, got nil")
		return
	}
	if response.Data.ID != vmID {
		t.Errorf("Expected VM ID '%s', got '%s'", vmID, response.Data.ID)
	}
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

func TestGetVirtualMachineMockHTTP(t *testing.T) {
	const vmID = "vm-789"

	server, gpcnClient := testutil.SetupMockServerWithGpcnClient(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "GET" && strings.Contains(r.URL.Path, "/virtual-machines/"+vmID) {
				testutil.WriteJSONResponse(w, newVMResponse(vmID, "test-vm"))
			} else {
				testutil.LogUnexpectedRequest(t, w, r)
			}
		},
	})
	defer server.Close()

	response, err := GetVirtualMachine(gpcnClient, context.Background(), vmID)
	if err != nil {
		t.Fatalf("GetVirtualMachine failed: %v", err)
	}
	if response == nil {
		t.Fatal("Expected response, got nil")
		return
	}
	if response.Data.ID != vmID {
		t.Errorf("Expected VM ID '%s', got '%s'", vmID, response.Data.ID)
	}
	if response.Data.Status != "Running" {
		t.Errorf("Expected status 'Running', got '%s'", response.Data.Status)
	}
}

func TestUpdateVirtualMachineMockHTTP(t *testing.T) {
	const (
		vmID    = "vm-update-123"
		newName = "updated-vm"
	)

	var updateCalled, vmStatusCalled bool

	server, gpcnClient := testutil.SetupMockServerWithGpcnClient(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.Method == "PUT" && strings.Contains(r.URL.Path, "/virtual-machines/"+vmID):
				updateCalled = true
				body, _ := io.ReadAll(r.Body)
				var req map[string]any
				_ = json.Unmarshal(body, &req)
				if req["name"] != newName {
					t.Errorf("Expected name '%s', got '%v'", newName, req["name"])
				}
				testutil.WriteJSONResponse(w, map[string]bool{"success": true})

			case r.Method == "GET" && strings.Contains(r.URL.Path, "/virtual-machines/"+vmID):
				vmStatusCalled = true
				resp := newVMResponse(vmID, newName)
				resp.Data.CreatedAt = time.Now().Add(-24 * time.Hour).Format(time.RFC3339)
				testutil.WriteJSONResponse(w, resp)

			case r.Method == "GET" && strings.Contains(r.URL.Path, "/network-interfaces"):
				testutil.WriteJSONResponse(w, emptyNetworkInterfacesResponse())

			default:
				testutil.LogUnexpectedRequest(t, w, r)
			}
		},
	})
	defer server.Close()

	if err := UpdateVirtualMachine(gpcnClient, context.Background(), vmID, newName); err != nil {
		t.Fatalf("UpdateVirtualMachine failed: %v", err)
	}

	response, err := GetVirtualMachine(gpcnClient, context.Background(), vmID)
	if err != nil {
		t.Fatalf("GetVirtualMachine failed: %v", err)
	}
	if response.Data.Name != newName {
		t.Errorf("Expected VM name '%s', got '%s'", newName, response.Data.Name)
	}
	if !updateCalled {
		t.Error("Expected update endpoint to be called")
	}
	if !vmStatusCalled {
		t.Error("Expected VM status endpoint to be called")
	}
}

func TestPollForVirtualMachineStatusMockHTTP(t *testing.T) {
	const vmID = "vm-poll-123"
	pollCount := 0

	server, gpcnClient := testutil.SetupMockServerWithGpcnClient(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "GET" && strings.Contains(r.URL.Path, "/virtual-machines/"+vmID) {
				pollCount++
				resp := newVMResponse(vmID, "test-vm")
				if pollCount < 2 {
					resp.Data.Status = "Building"
				}
				testutil.WriteJSONResponse(w, resp)
			}
		},
	})
	defer server.Close()

	response, err := PollForVirtualMachineStatus(gpcnClient, context.Background(), vmID, []string{"Running"}, 10, 0)
	if err != nil {
		t.Fatalf("PollForVirtualMachineStatus failed: %v", err)
	}
	if response == nil {
		t.Fatal("Expected response, got nil")
		return
	}
	if response.Data.Status != "Running" {
		t.Errorf("Expected final status 'Running', got '%s'", response.Data.Status)
	}
	if pollCount < 2 {
		t.Errorf("Expected at least 2 polls, got %d", pollCount)
	}
}

func TestValidatePublicIpValueMockHTTP(t *testing.T) {
	tests := []struct {
		name             string
		networkType      string
		allocatePublicIP bool
		expectError      bool
	}{
		{"public IP with standard network - valid", "standard", true, false},
		{"no public IP with custom network - valid", "custom", false, false},
		{"public IP with custom network - invalid", "custom", true, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			networkID := "network-test-123"

			server, gpcnClient := testutil.SetupMockServerWithGpcnClient(testutil.MockServerConfig{
				T: t,
				Handler: func(w http.ResponseWriter, r *http.Request) {
					if r.Method == "GET" && strings.Contains(r.URL.Path, "/networks/"+networkID) {
						testutil.WriteJSONResponse(w, map[string]any{
							"success": true,
							"message": "Network retrieved",
							"data":    map[string]any{"id": networkID, "name": "test-network", "networkType": tc.networkType},
						})
					}
				},
			})
			defer server.Close()

			model := createTestVMModel("test-vm", testVMImage, tc.allocatePublicIP)
			model.NetworkIds, _ = types.ListValueFrom(context.Background(), types.StringType, []string{networkID})

			err := ValidatePublicIpValue(gpcnClient, context.Background(), model)

			if tc.expectError && err == nil {
				t.Error("Expected error but got none")
			}
			if !tc.expectError && err != nil {
				t.Errorf("Expected no error but got: %v", err)
			}
			if tc.expectError && err != nil {
				if !strings.Contains(err.Error(), "allocate_public_ip") && !strings.Contains(err.Error(), "allocatePublicIp") {
					t.Errorf("Expected error to contain validation message, got '%s'", err.Error())
				}
			}
		})
	}
}

func TestSetNetworkModelValuesNotPresentWithPublicIP(t *testing.T) {
	const (
		vmID      = "vm-network-123"
		publicIP  = "203.0.113.42"
		networkID = "network-456"
	)

	server, gpcnClient := testutil.SetupMockServerWithGpcnClient(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "GET" && strings.Contains(r.URL.Path, "/network-interfaces") {
				testutil.WriteJSONResponse(w, map[string]any{
					"success": true, "message": "Network interfaces retrieved",
					"data": []map[string]any{{
						"id": "interface-001", "networkInterface": 0, "isPrimary": 1,
						"publicIp": publicIP, "publicIpId": "pubip-001", "privateIp": "10.0.0.5",
						"networkName": "default-network", "networkId": networkID,
						"cidrBlock": "10.0.0.0/24", "gatewayIp": "10.0.0.1", "networkType": "standard",
					}},
				})
			} else {
				testutil.LogUnexpectedRequest(t, w, r)
			}
		},
	})
	defer server.Close()

	model := ResourceModel{
		Name:             types.StringValue("test-vm"),
		DatacenterId:     types.StringValue(testDatacenterID),
		Image:            types.StringValue(testVMImage),
		AllocatePublicIp: types.BoolNull(),
		PublicIp:         types.StringNull(),
		NetworkIds:       types.ListNull(types.StringType),
	}

	result, _ := setNetworkModelValuesNotPresent(context.Background(), gpcnClient, vmID, model)

	if result.PublicIp.ValueString() != publicIP {
		t.Errorf("Expected public IP '%s', got '%s'", publicIP, result.PublicIp.ValueString())
	}
	if !result.AllocatePublicIp.ValueBool() {
		t.Error("Expected AllocatePublicIp to be true when public IP exists")
	}
	if result.NetworkIds.IsNull() {
		t.Error("Expected NetworkIds to be populated")
	}
}

func TestSetNetworkModelValuesNotPresentWithoutPublicIP(t *testing.T) {
	const (
		vmID      = "vm-network-789"
		networkID = "network-789"
	)

	server, gpcnClient := testutil.SetupMockServerWithGpcnClient(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "GET" && strings.Contains(r.URL.Path, "/network-interfaces") {
				testutil.WriteJSONResponse(w, map[string]any{
					"success": true, "message": "Network interfaces retrieved",
					"data": []map[string]any{{
						"id": "interface-002", "networkInterface": 0, "isPrimary": 1,
						"publicIp": "", "publicIpId": "", "privateIp": "10.0.0.10",
						"networkName": "private-network", "networkId": networkID,
						"cidrBlock": "10.0.0.0/24", "gatewayIp": "10.0.0.1", "networkType": "custom",
					}},
				})
			} else {
				testutil.LogUnexpectedRequest(t, w, r)
			}
		},
	})
	defer server.Close()

	model := ResourceModel{
		Name:             types.StringValue("test-vm"),
		DatacenterId:     types.StringValue(testDatacenterID),
		Image:            types.StringValue(testVMImage),
		AllocatePublicIp: types.BoolNull(),
		PublicIp:         types.StringNull(),
		NetworkIds:       types.ListNull(types.StringType),
	}

	result, _ := setNetworkModelValuesNotPresent(context.Background(), gpcnClient, vmID, model)

	if result.PublicIp.ValueString() != "" {
		t.Errorf("Expected empty public IP, got '%s'", result.PublicIp.ValueString())
	}
	if result.AllocatePublicIp.ValueBool() {
		t.Error("Expected AllocatePublicIp to be false when no public IP exists")
	}
}

func TestMapVirtualMachineResponseToModelUpdatesAuthUsername(t *testing.T) {
	const (
		vmID     = "vm-full-mapping-123"
		username = "admin"
		publicIP = "198.51.100.50"
	)

	server, gpcnClient := testutil.SetupMockServerWithGpcnClient(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "GET" && strings.Contains(r.URL.Path, "/network-interfaces") {
				testutil.WriteJSONResponse(w, map[string]any{
					"success": true, "message": "Network interfaces retrieved",
					"data": []map[string]any{{
						"id": "interface-003", "networkInterface": 0, "isPrimary": 1,
						"publicIp": publicIP, "publicIpId": "pubip-003", "privateIp": "10.0.0.20",
						"networkName": "standard-network", "networkId": "network-std-001",
						"cidrBlock": "10.0.0.0/24", "gatewayIp": "10.0.0.1", "networkType": "standard",
					}},
				})
			} else {
				testutil.LogUnexpectedRequest(t, w, r)
			}
		},
	})
	defer server.Close()

	response := newVMResponse(vmID, "test-vm-full")
	response.Data.Username = username

	model := createTestVMModel("test-vm-full", testVMImage, true)

	result, diags := MapVirtualMachineResponseToModel(context.Background(), gpcnClient, response, model)
	if diags.HasError() {
		t.Fatalf("Unexpected error: %v", diags)
	}

	if result.ID.ValueString() != vmID {
		t.Errorf("Expected ID '%s', got '%s'", vmID, result.ID.ValueString())
	}
	if result.PublicIp.ValueString() != publicIP {
		t.Errorf("Expected public IP '%s', got '%s'", publicIP, result.PublicIp.ValueString())
	}

	var auth ResourceModelAuth
	authDiags := result.Auth.As(context.Background(), &auth, basetypes.ObjectAsOptions{})
	if authDiags.HasError() {
		t.Fatalf("Failed to extract auth: %v", authDiags)
	}
	if auth.Username.ValueString() != username {
		t.Errorf("Expected auth.username '%s', got '%s'", username, auth.Username.ValueString())
	}
}

func TestSetModelValuesNotPresentPopulatesAuthOnImport(t *testing.T) {
	const (
		vmID     = "vm-import-456"
		username = "importuser"
		sshKeyID = "ssh-key-abc"
	)

	server, gpcnClient := testutil.SetupMockServerWithGpcnClient(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "GET" && strings.Contains(r.URL.Path, "/network-interfaces") {
				testutil.WriteJSONResponse(w, emptyNetworkInterfacesResponse())
			} else {
				testutil.LogUnexpectedRequest(t, w, r)
			}
		},
	})
	defer server.Close()

	response := newVMResponse(vmID, "import-vm")
	response.Data.Username = username
	response.Data.SshKeyId = sshKeyID

	// Simulate an import: Auth is null
	model := createTestVMModel("import-vm", testVMImage, false)
	model.Auth = types.ObjectNull(ResourceModelAuth{}.AttrTypes())

	result, diags := MapVirtualMachineResponseToModel(context.Background(), gpcnClient, response, model)
	if diags.HasError() {
		t.Fatalf("Unexpected error: %v", diags)
	}

	if result.Auth.IsNull() {
		t.Fatal("Expected auth to be populated on import, got null")
	}

	var auth ResourceModelAuth
	authDiags := result.Auth.As(context.Background(), &auth, basetypes.ObjectAsOptions{})
	if authDiags.HasError() {
		t.Fatalf("Failed to extract auth: %v", authDiags)
	}
	if auth.Username.ValueString() != username {
		t.Errorf("Expected auth.username '%s', got '%s'", username, auth.Username.ValueString())
	}
	if auth.SshKeyId.ValueString() != sshKeyID {
		t.Errorf("Expected auth.ssh_key_id '%s', got '%s'", sshKeyID, auth.SshKeyId.ValueString())
	}
}

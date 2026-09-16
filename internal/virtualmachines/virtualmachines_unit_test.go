package virtualmachines

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"terraform-provider-gpcn/internal/client"
	"terraform-provider-gpcn/internal/networks"
	"terraform-provider-gpcn/internal/testutil"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
)

const (
	testDatacenterID = "datacenter-123"
	testVMImage      = "Alma Linux 8.x"
)

func createTestVMModel(name, image string, allocatePublicIP bool) ResourceModel {
	auth := ResourceModelInitialAuth{
		SshKeyId: types.StringValue("ssh-key-123"),
		Username: types.StringNull(),
		Password: types.StringNull(),
	}
	authObj, _ := types.ObjectValueFrom(context.Background(), auth.AttrTypes(), auth)

	return ResourceModel{
		Name:             types.StringValue(name),
		DatacenterId:     types.StringValue(testDatacenterID),
		ImageId:          types.StringValue(image),
		SizeId:           types.StringValue("sku-uuid-test"),
		AllocatePublicIp: types.BoolValue(allocatePublicIP),
		NetworkIds:       types.ListNull(types.StringType),
		InitialAuth:      authObj,
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
		Code:         "G-Micro-1",
		CategoryCode: "general",
		SkuId:        "sku-uuid-test",
		SkuCode:      "general-G-Micro-1",
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

func useFastVMStatusPollInterval(t *testing.T) {
	t.Helper()
	useVMStatusPollInterval(t, 5*time.Millisecond)
	useVMStatusSettleWait(t, 5*time.Millisecond)
}

func useVMStatusSettleWait(t *testing.T, wait time.Duration) {
	t.Helper()
	original := VM_STATUS_SETTLE_WAIT
	VM_STATUS_SETTLE_WAIT = wait
	t.Cleanup(func() { VM_STATUS_SETTLE_WAIT = original })
}

func useVMStatusPollInterval(t *testing.T, interval time.Duration) {
	t.Helper()
	original := VM_STATUS_POLL_INTERVAL
	VM_STATUS_POLL_INTERVAL = interval
	t.Cleanup(func() { VM_STATUS_POLL_INTERVAL = original })
}

// Only sequential tests change these package variables, so the change is safe.
// Go resumes a parallel test after every sequential test ends.
func useNoInitialPollDelay(t *testing.T) {
	t.Helper()
	original := DEFAULT_INITIAL_POLL_DELAY_SECONDS
	DEFAULT_INITIAL_POLL_DELAY_SECONDS = 0
	t.Cleanup(func() { DEFAULT_INITIAL_POLL_DELAY_SECONDS = original })
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
	useFastVMStatusPollInterval(t)
	useNoInitialPollDelay(t)
	const (
		jobID   = "job-123"
		vmID    = "vm-456"
		imageID = "550e8400-e29b-41d4-a716-446655440000"
		sizeID  = "sku-abc-123"
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
				if req["imageId"].(string) != imageID {
					t.Errorf("Expected imageId %s, got '%v'", imageID, req["imageId"])
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

	if err := UpdateVirtualMachine(gpcnClient, context.Background(), vmID, map[string]any{"name": newName}); err != nil {
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
	useFastVMStatusPollInterval(t)
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
		ImageId:          types.StringValue(testVMImage),
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

	var ifaces []networks.ReadVirtualMachineNetworkDataResponseTF
	if diags := result.NetworkInterfaces.ElementsAs(context.Background(), &ifaces, false); diags.HasError() {
		t.Fatalf("Failed to read network interfaces: %v", diags)
	}
	if len(ifaces) != 1 {
		t.Fatalf("Expected 1 network interface, got %d", len(ifaces))
	}
	if ifaces[0].PrivateIP.ValueString() != "10.0.0.5" {
		t.Errorf("Expected private IP '10.0.0.5', got '%s'", ifaces[0].PrivateIP.ValueString())
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
		ImageId:          types.StringValue(testVMImage),
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

	var auth ResourceModelInitialAuth
	authDiags := result.InitialAuth.As(context.Background(), &auth, basetypes.ObjectAsOptions{})
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
	model.InitialAuth = types.ObjectNull(ResourceModelInitialAuth{}.AttrTypes())

	result, diags := MapVirtualMachineResponseToModel(context.Background(), gpcnClient, response, model)
	if diags.HasError() {
		t.Fatalf("Unexpected error: %v", diags)
	}

	if result.InitialAuth.IsNull() {
		t.Fatal("Expected auth to be populated on import, got null")
	}

	var auth ResourceModelInitialAuth
	authDiags := result.InitialAuth.As(context.Background(), &auth, basetypes.ObjectAsOptions{})
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

func TestSetModelValuesNotPresentResolvesImageIdOnImport(t *testing.T) {
	const (
		vmID    = "vm-image-import-789"
		imageID = "eb7da49d-cc71-480a-968d-fbf2841bedf7"
	)

	server, gpcnClient := testutil.SetupMockServerWithGpcnClient(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.Method == "GET" && strings.Contains(r.URL.Path, "/network-interfaces"):
				testutil.WriteJSONResponse(w, emptyNetworkInterfacesResponse())
			case r.Method == "GET" && strings.Contains(r.URL.Path, "/virtual-machine-images"):
				testutil.WriteJSONResponse(w, map[string]any{
					"success": true,
					"message": "Virtual machine images retrieved successfully",
					"data": []any{
						map[string]any{
							"id": 1, "name": "Linux", "sortOrder": 1,
							"images": []any{
								map[string]any{"id": imageID, "name": testVMImage},
							},
						},
					},
				})
			default:
				testutil.LogUnexpectedRequest(t, w, r)
			}
		},
	})
	defer server.Close()

	response := newVMResponse(vmID, "import-image-vm")
	// response.Data.Image is set to testVMImage by newVMResponse

	// Simulate import: ImageId is null, must be resolved from the image name
	model := createTestVMModel("import-image-vm", testVMImage, false)
	model.ImageId = types.StringNull()

	result, diags := MapVirtualMachineResponseToModel(context.Background(), gpcnClient, response, model)
	if diags.HasError() {
		t.Fatalf("Unexpected diagnostics: %v", diags)
	}

	if result.ImageId.IsNull() || result.ImageId.ValueString() == "" {
		t.Fatal("Expected image_id to be resolved on import, got null/empty")
	}
	if result.ImageId.ValueString() != imageID {
		t.Errorf("Expected image_id '%s', got '%s'", imageID, result.ImageId.ValueString())
	}
}

func TestPollForVirtualMachineStatusIgnoresEmptyStatus(t *testing.T) {
	useFastVMStatusPollInterval(t)
	const vmID = "vm-poll-empty-status"
	pollCount := 0

	server, gpcnClient := testutil.SetupMockServerWithGpcnClient(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "GET" && strings.Contains(r.URL.Path, "/virtual-machines/"+vmID) {
				pollCount++
				resp := newVMResponse(vmID, "test-vm")
				if pollCount < 2 {
					resp.Data.Status = ""
				}
				testutil.WriteJSONResponse(w, resp)
			}
		},
	})
	defer server.Close()

	response, err := PollForVirtualMachineStatus(gpcnClient, context.Background(), vmID, []string{"Running"}, 30, 0)
	if err != nil {
		t.Fatalf("PollForVirtualMachineStatus failed: %v", err)
	}
	if response == nil {
		t.Fatal("Expected response, got nil")
		return
	}
	if pollCount < 2 {
		t.Errorf("Expected the poller to keep polling past an empty status, got %d poll(s)", pollCount)
	}
	if response.Data.Status != "Running" {
		t.Errorf("Expected final status 'Running', got '%s'", response.Data.Status)
	}
}

func TestPollForVirtualMachineStatusTimesOut(t *testing.T) {
	// The long interval matters. A poller that counts iterations gives up before the
	// timeout expires.
	useVMStatusPollInterval(t, 50*time.Millisecond)
	const vmID = "vm-stuck-123"
	const timeoutMaxSec = 1

	server, gpcnClient := testutil.SetupMockServerWithGpcnClient(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "GET" && strings.Contains(r.URL.Path, "/virtual-machines/"+vmID) {
				resp := newVMResponse(vmID, "test-vm")
				resp.Data.Status = "Building"
				testutil.WriteJSONResponse(w, resp)
			}
		},
	})
	defer server.Close()

	start := time.Now()
	response, err := PollForVirtualMachineStatus(gpcnClient, context.Background(), vmID, []string{"Running"}, timeoutMaxSec, 0)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("Expected a timeout error, got nil")
	}
	if response != nil {
		t.Errorf("Expected no response on timeout, got '%v'", response)
	}
	expectedMessage := fmt.Sprintf(ErrVirtualMachineStatusTimeoutTemplate, timeoutMaxSec)
	if err.Error() != expectedMessage {
		t.Errorf("Expected error '%s', got '%s'", expectedMessage, err.Error())
	}
	if elapsed < 900*time.Millisecond {
		t.Errorf("Expected the poller to wait about %d second(s), it gave up after %v", timeoutMaxSec, elapsed)
	}
	if elapsed > 10*time.Second {
		t.Errorf("Expected the poller to stop near %d second(s), it ran for %v", timeoutMaxSec, elapsed)
	}
}

func TestPollForVirtualMachineStatusWaitsTheSettleWait(t *testing.T) {
	// The settle wait must be its own dial, so the poll interval stays near zero here.
	useVMStatusPollInterval(t, time.Millisecond)
	useVMStatusSettleWait(t, 120*time.Millisecond)
	const vmID = "vm-settle-123"

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

	start := time.Now()
	response, err := PollForVirtualMachineStatus(gpcnClient, context.Background(), vmID, []string{"Running"}, 30, 0)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("PollForVirtualMachineStatus failed: %v", err)
	}
	if response == nil {
		t.Fatal("Expected response, got nil")
	}
	if elapsed < 100*time.Millisecond {
		t.Errorf("Expected the poller to settle for about %v after the match, it returned after %v", VM_STATUS_SETTLE_WAIT, elapsed)
	}
	if elapsed > 5*time.Second {
		t.Errorf("Expected the poller to return soon after the settle wait, it ran for %v", elapsed)
	}
}

func TestPollForVirtualMachineStatusPreservesNotFound(t *testing.T) {
	useFastVMStatusPollInterval(t)

	server, gpcnClient := testutil.SetupMockServerWithRealTransport(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message":"virtual machine not found"}`))
		},
	})
	defer server.Close()

	_, err := PollForVirtualMachineStatus(gpcnClient, context.Background(), "vm-gone-123", []string{"Running"}, 10, 0)
	if err == nil {
		t.Fatal("Expected an error, got nil")
	}
	if !client.IsNotFound(err) {
		t.Errorf("Expected the poller to preserve the 404 so IsNotFound reports it, got '%v'", err)
	}
}

func TestUpdatePublicIPIfChangedReportsMissingPrimaryInterface(t *testing.T) {
	const vmID = "vm-no-primary-123"

	server, gpcnClient := testutil.SetupMockServerWithGpcnClient(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "GET" && strings.Contains(r.URL.Path, "/network-interfaces") {
				testutil.WriteJSONResponse(w, map[string]any{
					"success": true, "message": "Network interfaces retrieved",
					"data": []map[string]any{{
						"id": "interface-003", "networkInterface": 0, "isPrimary": 0,
						"publicIp": "", "publicIpId": "", "privateIp": "10.0.0.20",
						"networkName": "no-primary-network", "networkId": "network-003",
						"cidrBlock": "10.0.0.0/24", "gatewayIp": "10.0.0.1", "networkType": "standard",
					}},
				})
			} else {
				testutil.LogUnexpectedRequest(t, w, r)
			}
		},
	})
	defer server.Close()

	state := createTestVMModel("test-vm", testVMImage, false)
	plan := createTestVMModel("test-vm", testVMImage, true)

	diags := UpdatePublicIPIfChanged(gpcnClient, context.Background(), vmID, state, plan)
	if !diags.HasError() {
		t.Fatal("Expected an error diagnostic when no interface is primary")
	}

	summary := diags.Errors()[0].Summary()
	if summary != ErrSummaryNoPrimaryNetworkInterface {
		t.Errorf("Expected the summary '%s', got '%s'", ErrSummaryNoPrimaryNetworkInterface, summary)
	}
	detail := diags.Errors()[0].Detail()
	expectedDetail := fmt.Sprintf(ErrDetailNoPrimaryNetworkInterface, vmID)
	if detail != expectedDetail {
		t.Errorf("Expected the detail '%s', got '%s'", expectedDetail, detail)
	}
}

func TestMapVirtualMachineResponseToModelRefreshesDrift(t *testing.T) {
	const (
		vmID            = "vm-drift-123"
		newName         = "renamed-in-portal"
		newSkuID        = "sku-uuid-resized"
		newDatacenterID = "datacenter-999"
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

	response := newVMResponse(vmID, newName)
	response.Data.Configuration.SkuId = newSkuID
	response.Data.Datacenter.ID = newDatacenterID

	// State still holds the values Terraform last wrote, before the out-of-band change
	model := createTestVMModel("stale-vm", testVMImage, false)

	result, diags := MapVirtualMachineResponseToModel(context.Background(), gpcnClient, response, model)
	if diags.HasError() {
		t.Fatalf("Unexpected diagnostics: %v", diags)
	}
	result = RefreshVirtualMachineModelFromResponse(response, result)

	if result.Name.ValueString() != newName {
		t.Errorf("Expected name '%s', got '%s'", newName, result.Name.ValueString())
	}
	if result.SizeId.ValueString() != newSkuID {
		t.Errorf("Expected size_id '%s', got '%s'", newSkuID, result.SizeId.ValueString())
	}
	// The datacenter is fixed at creation, so a refresh of it can only cause a false diff.
	if result.DatacenterId.ValueString() != testDatacenterID {
		t.Errorf("Expected datacenter_id '%s', got '%s'", testDatacenterID, result.DatacenterId.ValueString())
	}
}

func TestMapVirtualMachineResponseToModelKeepsPlanValues(t *testing.T) {
	const (
		vmID            = "vm-plan-123"
		apiName         = "canonicalised-in-api"
		apiSkuID        = "sku-uuid-api"
		apiDatacenterID = "datacenter-api"
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

	response := newVMResponse(vmID, apiName)
	response.Data.Configuration.SkuId = apiSkuID
	response.Data.Datacenter.ID = apiDatacenterID

	// Create and Update map over the plan. A lagging API must not replace the planned values.
	model := createTestVMModel("planned-vm", testVMImage, false)

	result, diags := MapVirtualMachineResponseToModel(context.Background(), gpcnClient, response, model)
	if diags.HasError() {
		t.Fatalf("Unexpected diagnostics: %v", diags)
	}

	if result.Name.ValueString() != "planned-vm" {
		t.Errorf("Expected name 'planned-vm', got '%s'", result.Name.ValueString())
	}
	if result.SizeId.ValueString() != "sku-uuid-test" {
		t.Errorf("Expected size_id 'sku-uuid-test', got '%s'", result.SizeId.ValueString())
	}
	if result.DatacenterId.ValueString() != testDatacenterID {
		t.Errorf("Expected datacenter_id '%s', got '%s'", testDatacenterID, result.DatacenterId.ValueString())
	}
}

func TestRefreshVirtualMachineModelFromResponseKeepsValuesOnEmpty(t *testing.T) {
	response := newVMResponse("vm-empty-123", "")
	response.Data.Configuration.SkuId = ""

	model := createTestVMModel("configured-vm", testVMImage, false)

	result := RefreshVirtualMachineModelFromResponse(response, model)

	if result.Name.ValueString() != "configured-vm" {
		t.Errorf("Expected name 'configured-vm', got '%s'", result.Name.ValueString())
	}
	if result.SizeId.ValueString() != "sku-uuid-test" {
		t.Errorf("Expected size_id 'sku-uuid-test', got '%s'", result.SizeId.ValueString())
	}
}

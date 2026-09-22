package virtualmachines

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"terraform-provider-gpcn/internal/client"
	"terraform-provider-gpcn/internal/networks"
	"terraform-provider-gpcn/internal/testutil"

	"github.com/hashicorp/terraform-plugin-framework/attr"
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
		SubnetId:         types.StringValue("subnet-uuid-test"),
		L2SegmentIds:     types.ListValueMust(types.StringType, nil),
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

func useVirtualMachineStatusTimeout(t *testing.T, seconds int) {
	t.Helper()
	original := DEFAULT_VIRTUALMACHINE_STATUS_TIMEOUT_SECONDS
	DEFAULT_VIRTUALMACHINE_STATUS_TIMEOUT_SECONDS = seconds
	t.Cleanup(func() { DEFAULT_VIRTUALMACHINE_STATUS_TIMEOUT_SECONDS = original })
}

func useNetworkTimeout(t *testing.T, seconds int) {
	t.Helper()
	original := DEFAULT_NETWORK_TIMEOUT_SECONDS
	DEFAULT_NETWORK_TIMEOUT_SECONDS = seconds
	t.Cleanup(func() { DEFAULT_NETWORK_TIMEOUT_SECONDS = original })
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
				if _, present := req["networkId"]; present {
					t.Error("Expected networkId to be absent when the model names no network")
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

func TestSetNetworkModelValuesNotPresentWithPublicIP(t *testing.T) {
	const (
		vmID     = "vm-network-123"
		publicIP = "203.0.113.42"
		subnetID = "subnet-456"
	)

	server, gpcnClient := testutil.SetupMockServerWithGpcnClient(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "GET" && strings.Contains(r.URL.Path, "/network-interfaces") {
				testutil.WriteJSONResponse(w, map[string]any{
					"success": true, "message": "Network interfaces retrieved",
					"data": []map[string]any{{
						"id": "interface-001", "networkInterface": 1, "isPrimary": 1,
						"publicIp": publicIP, "publicIpId": "pubip-001", "privateIp": "10.0.0.5",
						"world": "vpc", "cidrBlock": "10.0.0.0/24", "vpcSubnetId": subnetID,
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
		SubnetId:         types.StringNull(),
		L2SegmentIds:     types.ListNull(types.StringType),
	}

	result, _ := setNetworkModelValuesNotPresent(context.Background(), gpcnClient, vmID, model)

	if result.PublicIp.ValueString() != publicIP {
		t.Errorf("Expected public IP '%s', got '%s'", publicIP, result.PublicIp.ValueString())
	}
	if result.AllocatePublicIp.ValueBool() {
		t.Error("Expected AllocatePublicIp to stay false when an address exists")
	}
	if result.PublicIpId.ValueString() != "pubip-001" {
		t.Errorf("Expected public_ip_id 'pubip-001', got '%s'", result.PublicIpId.ValueString())
	}
	if result.SubnetId.ValueString() != subnetID {
		t.Errorf("Expected subnet_id '%s', got '%s'", subnetID, result.SubnetId.ValueString())
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
		vmID     = "vm-network-789"
		subnetID = "subnet-789"
	)

	server, gpcnClient := testutil.SetupMockServerWithGpcnClient(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "GET" && strings.Contains(r.URL.Path, "/network-interfaces") {
				testutil.WriteJSONResponse(w, map[string]any{
					"success": true, "message": "Network interfaces retrieved",
					"data": []map[string]any{{
						"id": "interface-002", "networkInterface": 1, "isPrimary": 1,
						"publicIp": nil, "publicIpId": nil, "privateIp": "10.0.0.10",
						"world": "vpc", "cidrBlock": "10.0.0.0/24", "vpcSubnetId": subnetID,
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
		SubnetId:         types.StringNull(),
		L2SegmentIds:     types.ListNull(types.StringType),
	}

	result, _ := setNetworkModelValuesNotPresent(context.Background(), gpcnClient, vmID, model)

	if !result.PublicIp.IsNull() {
		t.Errorf("Expected a null public IP, got '%s'", result.PublicIp.ValueString())
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

func TestPollForVirtualMachineStatusAcceptsStopped(t *testing.T) {
	useFastVMStatusPollInterval(t)
	const vmID = "vm-stopped-123"

	server, gpcnClient := testutil.SetupMockServerWithGpcnClient(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "GET" && strings.Contains(r.URL.Path, "/virtual-machines/"+vmID) {
				resp := newVMResponse(vmID, "test-vm")
				resp.Data.Status = VMStatusStopped.String()
				testutil.WriteJSONResponse(w, resp)
			} else {
				testutil.LogUnexpectedRequest(t, w, r)
			}
		},
	})
	defer server.Close()

	targets := []string{VMStatusShutoff.String(), VMStatusStopped.String()}
	response, err := PollForVirtualMachineStatus(gpcnClient, context.Background(), vmID, targets, 30, 0)
	if err != nil {
		t.Fatalf("PollForVirtualMachineStatus failed: %v", err)
	}
	if response == nil {
		t.Fatal("Expected response, got nil")
		return
	}
	if response.Data.Status != VMStatusStopped.String() {
		t.Errorf("Expected final status '%s', got '%s'", VMStatusStopped, response.Data.Status)
	}
}

func TestPollForVirtualMachineStatusKeepsPollingOnError(t *testing.T) {
	useFastVMStatusPollInterval(t)
	const vmID = "vm-error-123"
	const timeoutMaxSec = 1
	pollCount := 0

	server, gpcnClient := testutil.SetupMockServerWithGpcnClient(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "GET" && strings.Contains(r.URL.Path, "/virtual-machines/"+vmID) {
				pollCount++
				resp := newVMResponse(vmID, "test-vm")
				resp.Data.Status = VMStatusError.String()
				testutil.WriteJSONResponse(w, resp)
			} else {
				testutil.LogUnexpectedRequest(t, w, r)
			}
		},
	})
	defer server.Close()

	start := time.Now()
	targets := []string{VMStatusRunning.String()}
	response, err := PollForVirtualMachineStatus(gpcnClient, context.Background(), vmID, targets, timeoutMaxSec, 0)
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
	if pollCount < 2 {
		t.Errorf("Expected the poller to keep polling past an Error status, got %d poll(s)", pollCount)
	}
	if elapsed < 900*time.Millisecond {
		t.Errorf("Expected the poller to wait about %d second(s), it gave up after %v", timeoutMaxSec, elapsed)
	}
}

func TestPollForVirtualMachineStatusFailsFastOnDeleting(t *testing.T) {
	useFastVMStatusPollInterval(t)
	const vmID = "vm-deleting-123"
	pollCount := 0

	server, gpcnClient := testutil.SetupMockServerWithGpcnClient(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "GET" && strings.Contains(r.URL.Path, "/virtual-machines/"+vmID) {
				pollCount++
				resp := newVMResponse(vmID, "test-vm")
				resp.Data.Status = VMStatusDeleting.String()
				testutil.WriteJSONResponse(w, resp)
			} else {
				testutil.LogUnexpectedRequest(t, w, r)
			}
		},
	})
	defer server.Close()

	targets := []string{VMStatusRunning.String()}
	_, err := PollForVirtualMachineStatus(gpcnClient, context.Background(), vmID, targets, 3, 0)
	if err == nil {
		t.Fatal("Expected a terminal status error, got nil")
	}
	if !strings.Contains(err.Error(), `reached status "Deleting"`) {
		t.Errorf("Expected the error to name the observed status, got '%s'", err.Error())
	}
	if pollCount != 1 {
		t.Errorf("Expected exactly 1 GET before the poller gave up, got %d", pollCount)
	}
}

func TestPollForVirtualMachineStatusFailsFastOnDestroyed(t *testing.T) {
	useFastVMStatusPollInterval(t)
	const vmID = "vm-destroyed-123"
	pollCount := 0

	server, gpcnClient := testutil.SetupMockServerWithGpcnClient(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "GET" && strings.Contains(r.URL.Path, "/virtual-machines/"+vmID) {
				pollCount++
				resp := newVMResponse(vmID, "test-vm")
				resp.Data.Status = VMStatusDestroyed.String()
				testutil.WriteJSONResponse(w, resp)
			} else {
				testutil.LogUnexpectedRequest(t, w, r)
			}
		},
	})
	defer server.Close()

	targets := []string{VMStatusShutoff.String(), VMStatusStopped.String()}
	_, err := PollForVirtualMachineStatus(gpcnClient, context.Background(), vmID, targets, 3, 0)
	if err == nil {
		t.Fatal("Expected a terminal status error, got nil")
	}
	expectedMessage := fmt.Sprintf(ErrDetailVMTerminalStatus, vmID, VMStatusDestroyed.String(), "Shutoff, Stopped")
	if err.Error() != expectedMessage {
		t.Errorf("Expected error '%s', got '%s'", expectedMessage, err.Error())
	}
	if !strings.Contains(err.Error(), `reached status "Destroyed"`) {
		t.Errorf("Expected the error to name the observed status, got '%s'", err.Error())
	}
	if pollCount != 1 {
		t.Errorf("Expected exactly 1 GET before the poller gave up, got %d", pollCount)
	}
}

func TestPollForVirtualMachineStatusKeepsPollingOnUnknown(t *testing.T) {
	useFastVMStatusPollInterval(t)
	const vmID = "vm-unknown-123"
	pollCount := 0

	server, gpcnClient := testutil.SetupMockServerWithGpcnClient(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "GET" && strings.Contains(r.URL.Path, "/virtual-machines/"+vmID) {
				pollCount++
				resp := newVMResponse(vmID, "test-vm")
				if pollCount < 3 {
					resp.Data.Status = VMStatusUnknown.String()
				}
				testutil.WriteJSONResponse(w, resp)
			} else {
				testutil.LogUnexpectedRequest(t, w, r)
			}
		},
	})
	defer server.Close()

	response, err := PollForVirtualMachineStatus(gpcnClient, context.Background(), vmID, []string{VMStatusRunning.String()}, 30, 0)
	if err != nil {
		t.Fatalf("PollForVirtualMachineStatus failed: %v", err)
	}
	if response == nil {
		t.Fatal("Expected response, got nil")
		return
	}
	if pollCount < 3 {
		t.Errorf("Expected the poller to keep polling past a transient Unknown, got %d poll(s)", pollCount)
	}
}

func TestIsTerminalStatusError(t *testing.T) {
	useFastVMStatusPollInterval(t)
	const vmID = "vm-terminal-check-123"

	terminalServer, terminalClient := testutil.SetupMockServerWithGpcnClient(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			resp := newVMResponse(vmID, "test-vm")
			resp.Data.Status = VMStatusDestroyed.String()
			testutil.WriteJSONResponse(w, resp)
		},
	})
	defer terminalServer.Close()

	_, terminalErr := PollForVirtualMachineStatus(terminalClient, context.Background(), vmID, []string{VMStatusRunning.String()}, 3, 0)
	if terminalErr == nil {
		t.Fatal("Expected a terminal status error, got nil")
	}
	if !IsTerminalStatusError(terminalErr) {
		t.Errorf("Expected IsTerminalStatusError to report the poller's terminal error, got false for '%v'", terminalErr)
	}
	if !IsTerminalStatusError(fmt.Errorf("stop virtual machine: %w", terminalErr)) {
		t.Error("Expected IsTerminalStatusError to see through a wrapping error")
	}
	expectedMessage := fmt.Sprintf(ErrDetailVMTerminalStatus, vmID, VMStatusDestroyed.String(), VMStatusRunning.String())
	if terminalErr.Error() != expectedMessage {
		t.Errorf("Expected error '%s', got '%s'", expectedMessage, terminalErr.Error())
	}

	stuckServer, stuckClient := testutil.SetupMockServerWithGpcnClient(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			resp := newVMResponse(vmID, "test-vm")
			resp.Data.Status = VMStatusProvisioning.String()
			testutil.WriteJSONResponse(w, resp)
		},
	})
	defer stuckServer.Close()

	_, timeoutErr := PollForVirtualMachineStatus(stuckClient, context.Background(), vmID, []string{VMStatusRunning.String()}, 1, 0)
	if timeoutErr == nil {
		t.Fatal("Expected a timeout error, got nil")
	}
	if IsTerminalStatusError(timeoutErr) {
		t.Errorf("Expected IsTerminalStatusError to reject the timeout error, got true for '%v'", timeoutErr)
	}

	missingServer, missingClient := testutil.SetupMockServerWithRealTransport(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message":"virtual machine not found"}`))
		},
	})
	defer missingServer.Close()

	_, notFoundErr := PollForVirtualMachineStatus(missingClient, context.Background(), vmID, []string{VMStatusRunning.String()}, 3, 0)
	if notFoundErr == nil {
		t.Fatal("Expected a not found error, got nil")
	}
	if IsTerminalStatusError(fmt.Errorf("stop virtual machine: %w", notFoundErr)) {
		t.Errorf("Expected IsTerminalStatusError to reject a wrapped not found error, got true for '%v'", notFoundErr)
	}
}

func TestStopVirtualMachineAcceptsStopped(t *testing.T) {
	useFastVMStatusPollInterval(t)
	useNetworkTimeout(t, 2)
	const vmID = "vm-stop-accepts-stopped"

	server, gpcnClient := testutil.SetupMockServerWithGpcnClient(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/stop"):
				testutil.WriteJSONResponse(w, map[string]any{"success": true, "message": "Stop requested"})
			case r.Method == "GET" && strings.Contains(r.URL.Path, "/virtual-machines/"+vmID):
				resp := newVMResponse(vmID, "test-vm")
				resp.Data.Status = VMStatusStopped.String()
				testutil.WriteJSONResponse(w, resp)
			default:
				testutil.LogUnexpectedRequest(t, w, r)
			}
		},
	})
	defer server.Close()

	if err := StopVirtualMachine(gpcnClient, context.Background(), vmID); err != nil {
		t.Fatalf("StopVirtualMachine failed: %v", err)
	}
}

func TestCreateVirtualMachineAcceptsStoppedStatus(t *testing.T) {
	useFastVMStatusPollInterval(t)
	useNoInitialPollDelay(t)
	useVirtualMachineStatusTimeout(t, 2)
	const (
		jobID   = "job-stopped-1"
		vmID    = "vm-created-stopped"
		imageID = "550e8400-e29b-41d4-a716-446655440000"
		sizeID  = "sku-abc-123"
	)

	server, gpcnClient := testutil.SetupMockServerWithGpcnClient(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/virtual-machines/"):
				testutil.WriteJSONResponse(w, client.JobStatusMultiResponse{
					Success: true,
					Message: "VM creation job started",
					Data: client.JobStatusDataResponse{
						Jobs: []client.JobResponse{{JobID: jobID, ResourceId: vmID}},
					},
				})
			case r.Method == "POST" && strings.Contains(r.URL.Path, "/jobs"):
				testutil.HandleJobResponse(w, jobID, vmID, true)
			case r.Method == "GET" && strings.Contains(r.URL.Path, "/virtual-machines/"+vmID):
				resp := newVMResponse(vmID, "test-vm")
				resp.Data.Status = VMStatusStopped.String()
				testutil.WriteJSONResponse(w, resp)
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
	if response.Data.Status != VMStatusStopped.String() {
		t.Errorf("Expected the create poller to settle on '%s', got '%s'", VMStatusStopped, response.Data.Status)
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

// A public IP attaches to a VPC interface. A machine whose primary interface carries an
// L2 segment names no VPC, so the provider refuses the change. A verb with no VPC to
// name would otherwise reach GPCN.
func TestUpdatePublicIPIfChangedRefusesAnL2PrimaryInterface(t *testing.T) {
	const vmID = "vm-l2-primary-123"

	server, gpcnClient := testutil.SetupMockServerWithGpcnClient(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "GET" && strings.Contains(r.URL.Path, "/network-interfaces") {
				testutil.WriteJSONResponse(w, map[string]any{
					"success": true, "message": "Network interfaces retrieved",
					"data": []map[string]any{{
						"id": "interface-l2", "networkInterface": 1, "isPrimary": 1,
						"world": "l2", "l2SegmentId": "segment-a", "l2SegmentName": "name of segment-a",
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
		t.Fatal("Expected an error diagnostic when the primary interface is not on a VPC")
	}

	summary := diags.Errors()[0].Summary()
	if summary != ErrSummaryUnableToUpdatePublicIPConfiguration {
		t.Errorf("Expected the summary '%s', got '%s'", ErrSummaryUnableToUpdatePublicIPConfiguration, summary)
	}
	detail := diags.Errors()[0].Detail()
	expectedDetail := fmt.Sprintf(ErrDetailPrimaryInterfaceNotOnAVpc, vmID)
	if detail != expectedDetail {
		t.Errorf("Expected the detail '%s', got '%s'", expectedDetail, detail)
	}
}

// segmentModelWithIds builds a model that carries the given segment list.
func segmentModelWithIds(segmentIds ...string) ResourceModel {
	model := createTestVMModel("test-vm", testVMImage, false)
	elements := make([]attr.Value, 0, len(segmentIds))
	for _, segmentId := range segmentIds {
		elements = append(elements, types.StringValue(segmentId))
	}
	model.L2SegmentIds = types.ListValueMust(types.StringType, elements)
	return model
}

// l2SegmentNics renders a primary VPC interface plus one L2 interface per segment. It
// stands for the live list the caller of UpdateL2SegmentsIfChanged reads.
func l2SegmentNics(segmentIds ...string) []networks.ReadVirtualMachineNetworkDataResponseTF {
	nics := []networks.ReadVirtualMachineNetworkDataResponseTF{{
		ID:               types.StringValue("nic-primary"),
		NetworkInterface: types.Int64Value(1),
		IsPrimary:        types.BoolValue(true),
		World:            types.StringValue(networks.NicWorldVpc),
		VpcID:            types.StringValue("vpc-1"),
		VpcSubnetID:      types.StringValue("subnet-uuid-test"),
	}}
	for index, segmentId := range segmentIds {
		nics = append(nics, networks.ReadVirtualMachineNetworkDataResponseTF{
			ID:               types.StringValue(fmt.Sprintf("nic-%s", segmentId)),
			NetworkInterface: types.Int64Value(int64(index + 2)),
			IsPrimary:        types.BoolValue(false),
			World:            types.StringValue(networks.NicWorldL2),
			L2SegmentID:      types.StringValue(segmentId),
		})
	}
	return nics
}

// segmentUpdateMockServer records every attach body. It answers the interface route of
// the named machine only. A call that addresses another machine reaches no arm.
func segmentUpdateMockServer(t *testing.T, vmID string) (*httptest.Server, *client.GpcnClient, *[]string) {
	t.Helper()

	interfacesPath := "/v1/resource/virtual-machines/" + vmID + "/network-interfaces"
	attached := []string{}
	server, gpcnClient := testutil.SetupMockServerWithGpcnClient(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.Method == http.MethodPost && r.URL.Path == interfacesPath:
				body := testutil.ReadRequestBody(r)
				segmentId, _ := body["l2SegmentId"].(string)
				attached = append(attached, segmentId)
				testutil.HandleCreateJobResponse(w, "job-1", "attach issued")
			case r.Method == http.MethodPost && r.URL.Path == "/v1/resource/jobs/":
				testutil.HandleJobResponse(w, "job-1", "", true)
			default:
				testutil.LogUnexpectedRequest(t, w, r)
			}
		},
	})

	return server, gpcnClient, &attached
}

// A read-back failure can leave state behind a change the platform already made. The
// next apply then asks for a segment the machine already carries. GPCN refuses a
// duplicate interface, so the adds come from the live list and not from state.
func TestUpdateL2SegmentsIfChangedSkipsASegmentTheMachineCarries(t *testing.T) {
	const vmID = "vm-live-segments-123"

	server, gpcnClient, attached := segmentUpdateMockServer(t, vmID)
	defer server.Close()

	state := segmentModelWithIds("segment-a")
	plan := segmentModelWithIds("segment-a", "segment-b")

	diags := UpdateL2SegmentsIfChanged(gpcnClient, context.Background(), vmID, state, plan, l2SegmentNics("segment-a", "segment-b"))
	if diags.HasError() {
		t.Fatalf("Expected no error diagnostic, got %v", diags.Errors())
	}
	if len(*attached) != 0 {
		t.Errorf("Expected no attach call, got %v", *attached)
	}
}

// The live list is the only input the adds come from. A segment the machine lacks must
// still reach the attach route.
func TestUpdateL2SegmentsIfChangedAttachesASegmentTheMachineLacks(t *testing.T) {
	const vmID = "vm-missing-segment-123"

	server, gpcnClient, attached := segmentUpdateMockServer(t, vmID)
	defer server.Close()

	state := segmentModelWithIds("segment-a")
	plan := segmentModelWithIds("segment-a", "segment-b")

	diags := UpdateL2SegmentsIfChanged(gpcnClient, context.Background(), vmID, state, plan, l2SegmentNics("segment-a"))
	if diags.HasError() {
		t.Fatalf("Expected no error diagnostic, got %v", diags.Errors())
	}
	if !slices.Equal(*attached, []string{"segment-b"}) {
		t.Errorf("Expected exactly one attach of 'segment-b', got %v", *attached)
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

	// State still holds the values Terraform last wrote, before the out-of-band change.
	model := createTestVMModel("stale-vm", testVMImage, false)

	result, diags := MapVirtualMachineResponseToModel(context.Background(), gpcnClient, response, model)
	if diags.HasError() {
		t.Fatalf("Unexpected diagnostics: %v", diags)
	}
	result = RefreshVirtualMachineModelFromResponse(response, result)

	if result.Name.ValueString() != newName {
		t.Errorf("Expected name '%s', got '%s'", newName, result.Name.ValueString())
	}
	// A refreshed size_id plans a downgrade, and the API rejects it, so the VM is replaced.
	if result.SizeId.ValueString() != "sku-uuid-test" {
		t.Errorf("Expected size_id 'sku-uuid-test' to survive, got '%s'", result.SizeId.ValueString())
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

	model := createTestVMModel("configured-vm", testVMImage, false)

	result := RefreshVirtualMachineModelFromResponse(response, model)

	if result.Name.ValueString() != "configured-vm" {
		t.Errorf("Expected name 'configured-vm', got '%s'", result.Name.ValueString())
	}
}

// The detail is a user-facing string that the release pins. It tells the operator that
// the machine is in state and tainted. A re-run of apply replaces it unless the operator
// untaints it first.
func TestVirtualMachineCreatedAttachFailedDetailBytes(t *testing.T) {
	const expected = "virtual machine %s was created and is in state, but attaching %s failed: %s. Terraform has marked the machine tainted: run terraform untaint on it and apply again to attach the remaining networks, or let the next apply replace it."

	if ErrDetailVMCreatedAttachFailed != expected {
		t.Errorf("Expected detail '%s', got '%s'", expected, ErrDetailVMCreatedAttachFailed)
	}
}

// The create body is .strict() at GPCN. An extra key is a 400, and a missing one is a
// refusal. The assertion is the exact key set, not a subset.
func TestCreateVirtualMachineSendsSubnetIdBodyMockHTTP(t *testing.T) {
	useFastVMStatusPollInterval(t)
	useNoInitialPollDelay(t)
	const (
		jobID    = "job-subnet-1"
		vmID     = "vm-subnet-1"
		imageID  = "550e8400-e29b-41d4-a716-446655440000"
		sizeID   = "sku-abc-123"
		subnetID = "33333333-3333-3333-3333-333333333333"
	)

	var createBody map[string]any

	server, gpcnClient := testutil.SetupMockServerWithGpcnClient(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/virtual-machines/"):
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Fatalf("reading the create body failed: %v", err)
				}
				if err := json.Unmarshal(body, &createBody); err != nil {
					t.Fatalf("unmarshaling the create body failed: %v", err)
				}
				testutil.WriteJSONResponse(w, client.JobStatusMultiResponse{
					Success: true,
					Message: "VM creation job started",
					Data: client.JobStatusDataResponse{
						Jobs: []client.JobResponse{{JobID: jobID, ResourceId: vmID}},
					},
				})
			case r.Method == "POST" && strings.Contains(r.URL.Path, "/jobs"):
				testutil.HandleJobResponse(w, jobID, vmID, true)
			case r.Method == "GET" && strings.Contains(r.URL.Path, "/virtual-machines/"+vmID):
				testutil.WriteJSONResponse(w, newVMResponse(vmID, "test-vm"))
			default:
				testutil.LogUnexpectedRequest(t, w, r)
			}
		},
	})
	defer server.Close()

	model := createTestVMModel("test-vm", testVMImage, false)
	model.SubnetId = types.StringValue(subnetID)

	if _, err := CreateVirtualMachine(gpcnClient, context.Background(), imageID, sizeID, model); err != nil {
		t.Fatalf("CreateVirtualMachine failed: %v", err)
	}

	if createBody == nil {
		t.Fatal("Expected the create endpoint to be called")
	}
	wantKeys := []string{"authMethod", "datacenterId", "imageId", "name", "skuId", "sshKeyId", "subnetId"}
	gotKeys := make([]string, 0, len(createBody))
	for key := range createBody {
		gotKeys = append(gotKeys, key)
	}
	slices.Sort(gotKeys)
	if !slices.Equal(gotKeys, wantKeys) {
		t.Fatalf("Expected the create body keys %v, got %v", wantKeys, gotKeys)
	}
	if createBody["subnetId"] != subnetID {
		t.Errorf("Expected subnetId '%s', got '%v'", subnetID, createBody["subnetId"])
	}
}

// acquirePublicIp and publicIpId are mutually exclusive at GPCN, so a false flag must not
// ride along beside a held address.
func TestCreateVirtualMachineSendsPublicIpKeysMockHTTP(t *testing.T) {
	useFastVMStatusPollInterval(t)
	useNoInitialPollDelay(t)
	const (
		jobID       = "job-subnet-2"
		vmID        = "vm-subnet-2"
		imageID     = "550e8400-e29b-41d4-a716-446655440000"
		sizeID      = "sku-abc-123"
		subnetID    = "33333333-3333-3333-3333-333333333333"
		publicIpID  = "44444444-4444-4444-4444-444444444444"
		acquireKey  = "acquirePublicIp"
		publicIpKey = "publicIpId"
	)

	newServer := func(t *testing.T, captured *map[string]any) (*httptest.Server, *client.GpcnClient) {
		t.Helper()
		return testutil.SetupMockServerWithGpcnClient(testutil.MockServerConfig{
			T: t,
			Handler: func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/virtual-machines/"):
					body, err := io.ReadAll(r.Body)
					if err != nil {
						t.Fatalf("reading the create body failed: %v", err)
					}
					if err := json.Unmarshal(body, captured); err != nil {
						t.Fatalf("unmarshaling the create body failed: %v", err)
					}
					testutil.WriteJSONResponse(w, client.JobStatusMultiResponse{
						Success: true,
						Message: "VM creation job started",
						Data: client.JobStatusDataResponse{
							Jobs: []client.JobResponse{{JobID: jobID, ResourceId: vmID}},
						},
					})
				case r.Method == "POST" && strings.Contains(r.URL.Path, "/jobs"):
					testutil.HandleJobResponse(w, jobID, vmID, true)
				case r.Method == "GET" && strings.Contains(r.URL.Path, "/virtual-machines/"+vmID):
					testutil.WriteJSONResponse(w, newVMResponse(vmID, "test-vm"))
				default:
					testutil.LogUnexpectedRequest(t, w, r)
				}
			},
		})
	}

	t.Run("acquire true sends the flag", func(t *testing.T) {
		var createBody map[string]any
		server, gpcnClient := newServer(t, &createBody)
		defer server.Close()

		model := createTestVMModel("test-vm", testVMImage, true)
		model.SubnetId = types.StringValue(subnetID)
		if _, err := CreateVirtualMachine(gpcnClient, context.Background(), imageID, sizeID, model); err != nil {
			t.Fatalf("CreateVirtualMachine failed: %v", err)
		}
		if acquire, ok := createBody[acquireKey].(bool); !ok || !acquire {
			t.Errorf("Expected %s true, got '%v'", acquireKey, createBody[acquireKey])
		}
		if _, present := createBody[publicIpKey]; present {
			t.Errorf("Expected %s to be absent", publicIpKey)
		}
	})

	t.Run("a held address sends no acquire flag", func(t *testing.T) {
		var createBody map[string]any
		server, gpcnClient := newServer(t, &createBody)
		defer server.Close()

		model := createTestVMModel("test-vm", testVMImage, false)
		model.SubnetId = types.StringValue(subnetID)
		model.PublicIpId = types.StringValue(publicIpID)
		if _, err := CreateVirtualMachine(gpcnClient, context.Background(), imageID, sizeID, model); err != nil {
			t.Fatalf("CreateVirtualMachine failed: %v", err)
		}
		if _, present := createBody[acquireKey]; present {
			t.Errorf("Expected %s to be absent, got '%v'", acquireKey, createBody[acquireKey])
		}
		if createBody[publicIpKey] != publicIpID {
			t.Errorf("Expected %s '%s', got '%v'", publicIpKey, publicIpID, createBody[publicIpKey])
		}
	})
}

// A VPC interface carries null in every legacy column. The mapper must carry those nulls
// into state, because an empty string reads as a value GPCN never sent.
func TestMapNICNullsStayNullUnit(t *testing.T) {
	const vmID = "vm-nic-nulls"

	server, gpcnClient := testutil.SetupMockServerWithGpcnClient(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "GET" && strings.Contains(r.URL.Path, "/network-interfaces") {
				testutil.WriteJSONResponse(w, map[string]any{
					"success": true, "message": "Network interfaces retrieved",
					"data": []map[string]any{{
						"id": "interface-vpc", "networkInterface": 1, "isPrimary": 1,
						"macAddress": nil, "publicIp": nil, "publicIpId": nil, "privateIp": "10.20.0.7",
						"world":       "vpc",
						"networkName": nil, "networkId": nil, "cidrBlock": "10.20.0.0/24",
						"gatewayIp": nil, "networkType": nil,
						"vpcSubnetId": "subnet-1", "subnetName": "web", "vpcId": "vpc-1", "vpcName": "prod",
						"l2SegmentId": nil, "l2SegmentName": nil,
					}},
				})
			} else {
				testutil.LogUnexpectedRequest(t, w, r)
			}
		},
	})
	defer server.Close()

	model := ResourceModel{
		SubnetId:         types.StringValue("subnet-1"),
		AllocatePublicIp: types.BoolValue(false),
		L2SegmentIds:     types.ListValueMust(types.StringType, nil),
	}

	result, diags := setNetworkModelValuesNotPresent(context.Background(), gpcnClient, vmID, model)
	if diags.HasError() {
		t.Fatalf("setNetworkModelValuesNotPresent reported errors: %v", diags)
	}

	var ifaces []networks.ReadVirtualMachineNetworkDataResponseTF
	if listDiags := result.NetworkInterfaces.ElementsAs(context.Background(), &ifaces, false); listDiags.HasError() {
		t.Fatalf("Failed to read network interfaces: %v", listDiags)
	}
	if len(ifaces) != 1 {
		t.Fatalf("Expected 1 network interface, got %d", len(ifaces))
	}
	nulls := map[string]types.String{
		"mac_address":     ifaces[0].MacAddress,
		"public_ip":       ifaces[0].PublicIP,
		"public_ip_id":    ifaces[0].PublicIPID,
		"network_name":    ifaces[0].NetworkName,
		"network_id":      ifaces[0].NetworkID,
		"gateway_ip":      ifaces[0].GatewayIP,
		"network_type":    ifaces[0].NetworkType,
		"l2_segment_id":   ifaces[0].L2SegmentID,
		"l2_segment_name": ifaces[0].L2SegmentName,
	}
	for name, value := range nulls {
		if !value.IsNull() {
			t.Errorf("Expected %s to stay null, got '%s'", name, value.ValueString())
		}
	}
	if ifaces[0].World.ValueString() != "vpc" {
		t.Errorf("Expected world 'vpc', got '%s'", ifaces[0].World.ValueString())
	}
	if !result.PublicIp.IsNull() {
		t.Errorf("Expected public_ip to stay null, got '%s'", result.PublicIp.ValueString())
	}
}

// An import starts with no configuration. The birth subnet and the attached segments are
// readable only through the interface list. The mapper fills them from it.
func TestSetNetworkModelValuesNotPresentFillsVpcIdentityOnImportUnit(t *testing.T) {
	const vmID = "vm-import-vpc"

	server, gpcnClient := testutil.SetupMockServerWithGpcnClient(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "GET" && strings.Contains(r.URL.Path, "/network-interfaces") {
				testutil.WriteJSONResponse(w, map[string]any{
					"success": true, "message": "Network interfaces retrieved",
					"data": []map[string]any{
						{
							"id": "interface-1", "networkInterface": 1, "isPrimary": 1,
							"world": "vpc", "privateIp": "10.20.0.7", "publicIp": "203.0.113.9",
							"publicIpId": "pubip-import", "vpcSubnetId": "subnet-1", "vpcId": "vpc-1",
						},
						{
							"id": "interface-2", "networkInterface": 2, "isPrimary": 0,
							"world": "l2", "l2SegmentId": "segment-a",
						},
						{
							"id": "interface-3", "networkInterface": 3, "isPrimary": 0,
							"world": "l2", "l2SegmentId": "segment-b",
						},
					},
				})
			} else {
				testutil.LogUnexpectedRequest(t, w, r)
			}
		},
	})
	defer server.Close()

	model := ResourceModel{
		SubnetId:         types.StringNull(),
		L2SegmentIds:     types.ListNull(types.StringType),
		AllocatePublicIp: types.BoolNull(),
		PublicIp:         types.StringNull(),
	}

	result, diags := setNetworkModelValuesNotPresent(context.Background(), gpcnClient, vmID, model)
	if diags.HasError() {
		t.Fatalf("setNetworkModelValuesNotPresent reported errors: %v", diags)
	}

	if result.SubnetId.ValueString() != "subnet-1" {
		t.Errorf("Expected subnet_id 'subnet-1', got '%s'", result.SubnetId.ValueString())
	}
	if result.AllocatePublicIp.ValueBool() {
		t.Error("Expected allocate_public_ip false on an import, whatever address the machine carries")
	}
	if result.PublicIp.ValueString() != "203.0.113.9" {
		t.Errorf("Expected public_ip '203.0.113.9', got '%s'", result.PublicIp.ValueString())
	}
	if result.PublicIpId.ValueString() != "pubip-import" {
		t.Errorf("Expected public_ip_id 'pubip-import', got '%s'", result.PublicIpId.ValueString())
	}
	var segments []string
	if listDiags := result.L2SegmentIds.ElementsAs(context.Background(), &segments, false); listDiags.HasError() {
		t.Fatalf("Failed to read l2_segment_ids: %v", listDiags)
	}
	want := []string{"segment-a", "segment-b"}
	if !slices.Equal(segments, want) {
		t.Errorf("Expected l2_segment_ids %v, got %v", want, segments)
	}
}

// GPCN stores an acquired address and a held one in the same row. An import therefore
// records the address as held. An inferred intent would release the operator's address.
func TestSetNetworkModelValuesNotPresentNeverInfersAnAcquiredAddressUnit(t *testing.T) {
	tests := []struct {
		name         string
		address      any
		addressID    any
		intent       types.Bool
		wantAllocate bool
		wantHeld     string
	}{
		{"an import with an address records it as held", "203.0.113.9", "pubip-held", types.BoolNull(), false, "pubip-held"},
		{"an import without an address records none", nil, nil, types.BoolNull(), false, ""},
		{"a configured intent survives", "203.0.113.9", "pubip-held", types.BoolValue(true), true, ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server, gpcnClient := testutil.SetupMockServerWithGpcnClient(testutil.MockServerConfig{
				T: t,
				Handler: func(w http.ResponseWriter, r *http.Request) {
					if r.Method == "GET" && strings.Contains(r.URL.Path, "/network-interfaces") {
						testutil.WriteJSONResponse(w, map[string]any{
							"success": true, "message": "Network interfaces retrieved",
							"data": []map[string]any{{
								"id": "interface-1", "networkInterface": 1, "isPrimary": 1,
								"world": "vpc", "privateIp": "10.20.0.7",
								"publicIp": tc.address, "publicIpId": tc.addressID,
								"vpcSubnetId": "subnet-1", "vpcId": "vpc-1",
							}},
						})
					} else {
						testutil.LogUnexpectedRequest(t, w, r)
					}
				},
			})
			defer server.Close()

			model := ResourceModel{
				SubnetId:         types.StringNull(),
				L2SegmentIds:     types.ListNull(types.StringType),
				AllocatePublicIp: tc.intent,
				PublicIpId:       types.StringNull(),
				PublicIp:         types.StringNull(),
			}

			result, diags := setNetworkModelValuesNotPresent(context.Background(), gpcnClient, "vm-held-address", model)
			if diags.HasError() {
				t.Fatalf("setNetworkModelValuesNotPresent reported errors: %v", diags)
			}

			if result.AllocatePublicIp.ValueBool() != tc.wantAllocate {
				t.Errorf("Expected allocate_public_ip %t, got %t", tc.wantAllocate, result.AllocatePublicIp.ValueBool())
			}
			if tc.wantHeld == "" {
				if !result.PublicIpId.IsNull() {
					t.Errorf("Expected a null public_ip_id, got '%s'", result.PublicIpId.ValueString())
				}
				return
			}
			if result.PublicIpId.ValueString() != tc.wantHeld {
				t.Errorf("Expected public_ip_id '%s', got '%s'", tc.wantHeld, result.PublicIpId.ValueString())
			}
		})
	}
}

// The refusal of an address on a machine that has no VPC is the only report the user
// reads. The release pins the bytes, and the compiler accepts any rewording.
func TestPrimaryInterfaceNotOnAVpcBytes(t *testing.T) {
	const expectedSummary = "Unable to update public IP configuration"
	if ErrSummaryUnableToUpdatePublicIPConfiguration != expectedSummary {
		t.Errorf("Expected the summary '%s', got '%s'", expectedSummary, ErrSummaryUnableToUpdatePublicIPConfiguration)
	}

	const expectedDetail = "the primary network interface of virtual machine %s is not on a VPC subnet, and a public IP attaches to a VPC interface only"
	if ErrDetailPrimaryInterfaceNotOnAVpc != expectedDetail {
		t.Errorf("Expected the detail '%s', got '%s'", expectedDetail, ErrDetailPrimaryInterfaceNotOnAVpc)
	}
}

// A failed start leaves a stopped machine, and these bytes are the only report of it.
// The release pins them, and the compiler accepts any rewording.
func TestVirtualMachineLeftStoppedBytes(t *testing.T) {
	tests := []struct {
		name     string
		actual   string
		expected string
	}{
		{
			name:     "summary",
			actual:   ErrSummaryVMLeftStopped,
			expected: "Virtual machine left stopped",
		},
		{
			name:     "update detail",
			actual:   ErrDetailVMLeftStoppedUpdate,
			expected: "virtual machine %s was stopped for the change and did not start again: %s. Start it in the portal.",
		},
		{
			name:     "create detail",
			actual:   ErrDetailVMLeftStoppedCreate,
			expected: "virtual machine %s was stopped for the change and did not start again: %s. Start it in the portal, then run terraform untaint on it; otherwise the next apply replaces the machine.",
		},
		{
			name:     "retry detail",
			actual:   ErrDetailVMLeftStoppedRetry,
			expected: "virtual machine %s was stopped for the change and did not start again: %s. Start it in the portal, then run terraform plan and check the proposed changes before applying.",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.actual != tc.expected {
				t.Errorf("Expected %s '%s', got '%s'", tc.name, tc.expected, tc.actual)
			}
		})
	}
}

const (
	testPublicIpVpcID      = "vpc-address-1"
	testPublicIpAcquiredID = "ip-acquired-1"
	testPublicIpJobAcquire = "job-acquire"
	testPublicIpJobAttach  = "job-attach"
	testPublicIpJobDetach  = "job-detach"
	testPublicIpJobRelease = "job-release"
)

// publicIpPrimaryInterfaceBody reports one primary VPC interface. An empty carriedID
// leaves both address columns out, as GPCN does for an interface with no address.
func publicIpPrimaryInterfaceBody(carriedID string) map[string]any {
	row := map[string]any{
		"id": "nic-primary", "networkInterface": 1, "isPrimary": 1,
		"world": networks.NicWorldVpc, "vpcId": testPublicIpVpcID,
		"vpcSubnetId": "subnet-uuid-test",
	}
	if carriedID != "" {
		row["publicIp"] = "203.0.113.10"
		row["publicIpId"] = carriedID
	}
	return map[string]any{
		"success": true, "message": "Network interfaces retrieved",
		"data": []map[string]any{row},
	}
}

// publicIpVerbTarget returns the address id a verb route names.
func publicIpVerbTarget(routePath, addressesPath, verb string) string {
	return strings.TrimSuffix(strings.TrimPrefix(routePath, addressesPath+"/"), verb)
}

// writePublicIpJobStatus answers one poll. A job the test names reports a failure, so
// the verb that issued it refuses like the platform does.
func writePublicIpJobStatus(w http.ResponseWriter, r *http.Request, failedJobs []string) {
	jobID := ""
	if jobIds, ok := testutil.ReadRequestBody(r)["jobIds"].([]any); ok && len(jobIds) > 0 {
		jobID, _ = jobIds[0].(string)
	}
	if !slices.Contains(failedJobs, jobID) {
		testutil.HandleJobResponse(w, jobID, "", true)
		return
	}
	testutil.WriteJSONResponse(w, map[string]any{
		"success": true, "message": "Job status retrieved",
		"data": map[string]any{"jobs": []map[string]any{{
			"jobId": jobID, "isCompleted": false, "isTerminal": true,
			"hasFailed": true, "errorMessage": jobID + " refused",
		}}},
	})
}

// publicIpUpdateMockServer serves the VPC address verbs and records them in order. The
// primary interface carries the given address. Each job id in failedJobs answers a
// failed job, so a test chooses which verb refuses.
func publicIpUpdateMockServer(t *testing.T, carriedID string, failedJobs ...string) (*httptest.Server, *client.GpcnClient, *[]string) {
	t.Helper()

	addressesPath := "/v1/resource/vpcs/" + testPublicIpVpcID + "/public-ips"
	verbs := []string{}

	server, gpcnClient := testutil.SetupMockServerWithGpcnClient(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/network-interfaces"):
				testutil.WriteJSONResponse(w, publicIpPrimaryInterfaceBody(carriedID))
			case r.Method == http.MethodPost && r.URL.Path == addressesPath:
				verbs = append(verbs, "acquire")
				testutil.WriteJSONResponse(w, map[string]any{
					"success": true, "message": "Operation initiated successfully",
					"data": map[string]any{
						"publicIpId": testPublicIpAcquiredID, "jobId": testPublicIpJobAcquire,
					},
				})
			case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/attach"):
				verbs = append(verbs, "attach "+publicIpVerbTarget(r.URL.Path, addressesPath, "/attach"))
				testutil.HandleCreateJobResponse(w, testPublicIpJobAttach, "attach issued")
			case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/detach"):
				verbs = append(verbs, "detach "+publicIpVerbTarget(r.URL.Path, addressesPath, "/detach"))
				testutil.HandleCreateJobResponse(w, testPublicIpJobDetach, "detach issued")
			case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, addressesPath+"/"):
				verbs = append(verbs, "release "+strings.TrimPrefix(r.URL.Path, addressesPath+"/"))
				testutil.HandleCreateJobResponse(w, testPublicIpJobRelease, "release issued")
			case r.Method == http.MethodPost && r.URL.Path == "/v1/resource/jobs/":
				writePublicIpJobStatus(w, r, failedJobs)
			default:
				testutil.LogUnexpectedRequest(t, w, r)
			}
		},
	})

	return server, gpcnClient, &verbs
}

// A read-back that fails after an acquisition leaves state behind the platform. The
// next apply then asks for an address the interface already carries. GPCN answers 409
// for a second address, so the acquire reads the live interface and not state.
func TestUpdatePublicIPIfChangedSkipsTheAcquireWhenThePrimaryCarriesAnAddress(t *testing.T) {
	const vmID = "vm-carries-an-address"

	server, gpcnClient, verbs := publicIpUpdateMockServer(t, testPublicIpAcquiredID)
	defer server.Close()

	state := createTestVMModel("test-vm", testVMImage, false)
	plan := createTestVMModel("test-vm", testVMImage, true)

	diags := UpdatePublicIPIfChanged(gpcnClient, context.Background(), vmID, state, plan)
	if diags.HasError() {
		t.Fatalf("Expected no error diagnostic, got %v", diags.Errors())
	}
	if len(*verbs) != 0 {
		t.Errorf("Expected no address verb, got %v", *verbs)
	}
}

// The API inserts the address row before it dispatches the job, so a failed acquisition
// leaves a real address. The diagnostic is the only record of it.
func TestUpdatePublicIPIfChangedNamesTheAddressWhenTheAcquireJobFails(t *testing.T) {
	const vmID = "vm-acquire-job-fails"

	server, gpcnClient, verbs := publicIpUpdateMockServer(t, "", testPublicIpJobAcquire)
	defer server.Close()

	state := createTestVMModel("test-vm", testVMImage, false)
	plan := createTestVMModel("test-vm", testVMImage, true)

	diags := UpdatePublicIPIfChanged(gpcnClient, context.Background(), vmID, state, plan)
	if !diags.HasError() {
		t.Fatal("Expected an error diagnostic when the acquisition job fails")
	}
	detail := diags.Errors()[0].Detail()
	wantPrefix := fmt.Sprintf("public IP %s was acquired for virtual machine %s but its acquisition job failed: ", testPublicIpAcquiredID, vmID)
	if !strings.HasPrefix(detail, wantPrefix) {
		t.Errorf("Expected the detail to start with '%s', got '%s'", wantPrefix, detail)
	}
	if !strings.HasSuffix(detail, ". Release it in the portal or import it as gpcn_vpc_public_ip.") {
		t.Errorf("Expected the detail to end with the remedy sentence, got '%s'", detail)
	}
	if !slices.Equal(*verbs, []string{"acquire"}) {
		t.Errorf("Expected the acquire alone, got %v", *verbs)
	}
}

// An acquired address that no interface carries is unusable and unrecorded, so it goes
// back before the report. The user then has nothing to clean up.
func TestUpdatePublicIPIfChangedReleasesTheAddressWhenTheAttachFails(t *testing.T) {
	const vmID = "vm-attach-fails"

	server, gpcnClient, verbs := publicIpUpdateMockServer(t, "", testPublicIpJobAttach)
	defer server.Close()

	state := createTestVMModel("test-vm", testVMImage, false)
	plan := createTestVMModel("test-vm", testVMImage, true)

	diags := UpdatePublicIPIfChanged(gpcnClient, context.Background(), vmID, state, plan)
	if !diags.HasError() {
		t.Fatal("Expected an error diagnostic when the attach fails")
	}
	detail := diags.Errors()[0].Detail()
	wantPrefix := fmt.Sprintf("public IP %s was acquired for virtual machine %s but attaching it failed: ", testPublicIpAcquiredID, vmID)
	if !strings.HasPrefix(detail, wantPrefix) {
		t.Errorf("Expected the detail to start with '%s', got '%s'", wantPrefix, detail)
	}
	if !strings.HasSuffix(detail, "; the address was released.") {
		t.Errorf("Expected the detail to report the release, got '%s'", detail)
	}
	want := []string{"acquire", "attach " + testPublicIpAcquiredID, "release " + testPublicIpAcquiredID}
	if !slices.Equal(*verbs, want) {
		t.Errorf("Expected %v, got %v", want, *verbs)
	}
}

// A release that fails after a failed attach leaves the address in holdings. The
// diagnostic names it, because no later destroy reaches it.
func TestUpdatePublicIPIfChangedNamesTheAddressWhenTheReleaseAlsoFails(t *testing.T) {
	const vmID = "vm-release-also-fails"

	server, gpcnClient, verbs := publicIpUpdateMockServer(t, "", testPublicIpJobAttach, testPublicIpJobRelease)
	defer server.Close()

	state := createTestVMModel("test-vm", testVMImage, false)
	plan := createTestVMModel("test-vm", testVMImage, true)

	diags := UpdatePublicIPIfChanged(gpcnClient, context.Background(), vmID, state, plan)
	if !diags.HasError() {
		t.Fatal("Expected an error diagnostic when the release also fails")
	}
	detail := diags.Errors()[0].Detail()
	wantPrefix := fmt.Sprintf("public IP %s was acquired for virtual machine %s but attaching it failed: ", testPublicIpAcquiredID, vmID)
	if !strings.HasPrefix(detail, wantPrefix) {
		t.Errorf("Expected the detail to start with '%s', got '%s'", wantPrefix, detail)
	}
	if !strings.Contains(detail, "; releasing it failed too: ") {
		t.Errorf("Expected the detail to report the failed release, got '%s'", detail)
	}
	if !strings.HasSuffix(detail, ". Release it in the portal or import it as gpcn_vpc_public_ip.") {
		t.Errorf("Expected the detail to end with the remedy sentence, got '%s'", detail)
	}
	want := []string{"acquire", "attach " + testPublicIpAcquiredID, "release " + testPublicIpAcquiredID}
	if !slices.Equal(*verbs, want) {
		t.Errorf("Expected %v, got %v", want, *verbs)
	}
}

// A detach that succeeds takes the address off the interface, so a later gate finds
// nothing to release. The failed release is therefore the last record of the address.
func TestUpdatePublicIPIfChangedNamesTheAddressWhenTheReleaseAfterDetachFails(t *testing.T) {
	const vmID = "vm-release-after-detach-fails"

	server, gpcnClient, verbs := publicIpUpdateMockServer(t, testPublicIpAcquiredID, testPublicIpJobRelease)
	defer server.Close()

	state := createTestVMModel("test-vm", testVMImage, true)
	plan := createTestVMModel("test-vm", testVMImage, false)

	diags := UpdatePublicIPIfChanged(gpcnClient, context.Background(), vmID, state, plan)
	if !diags.HasError() {
		t.Fatal("Expected an error diagnostic when the release fails")
	}
	detail := diags.Errors()[0].Detail()
	wantPrefix := fmt.Sprintf("public IP %s was acquired for virtual machine %s but releasing it failed: ", testPublicIpAcquiredID, vmID)
	if !strings.HasPrefix(detail, wantPrefix) {
		t.Errorf("Expected the detail to start with '%s', got '%s'", wantPrefix, detail)
	}
	if !strings.HasSuffix(detail, ". Release it in the portal or import it as gpcn_vpc_public_ip.") {
		t.Errorf("Expected the detail to end with the remedy sentence, got '%s'", detail)
	}
	want := []string{"detach " + testPublicIpAcquiredID, "release " + testPublicIpAcquiredID}
	if !slices.Equal(*verbs, want) {
		t.Errorf("Expected %v, got %v", want, *verbs)
	}
}

// An orphaned address costs money and hides from Terraform, so the release pins the
// sentences that tell the operator where it is.
func TestPublicIpOrphanDetailBytes(t *testing.T) {
	tests := []struct {
		name     string
		actual   string
		expected string
	}{
		{
			name:     "orphaned",
			actual:   ErrDetailPublicIpOrphaned,
			expected: "public IP %s was acquired for virtual machine %s but %s: %s. Release it in the portal or import it as gpcn_vpc_public_ip.",
		},
		{
			name:     "attach failed and the release succeeded",
			actual:   ErrDetailPublicIpAttachFailedReleased,
			expected: "public IP %s was acquired for virtual machine %s but attaching it failed: %s; the address was released.",
		},
		{
			name:     "attach failed and the release failed too",
			actual:   ErrDetailPublicIpAttachFailedReleaseFailed,
			expected: "public IP %s was acquired for virtual machine %s but attaching it failed: %s; releasing it failed too: %s. Release it in the portal or import it as gpcn_vpc_public_ip.",
		},
		{name: "acquisition phrase", actual: ErrPhrasePublicIpAcquisitionFailed, expected: "its acquisition job failed"},
		{name: "attach phrase", actual: ErrPhrasePublicIpAttachFailed, expected: "attaching it failed"},
		{name: "release phrase", actual: ErrPhrasePublicIpReleaseFailed, expected: "releasing it failed"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.actual != tc.expected {
				t.Errorf("Expected '%s', got '%s'", tc.expected, tc.actual)
			}
		})
	}
}

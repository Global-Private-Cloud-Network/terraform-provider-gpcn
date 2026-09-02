package virtualmachines

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"terraform-provider-gpcn/internal/client"
	"terraform-provider-gpcn/internal/testutil"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
)

// TestMain decreases the delays of PollForVirtualMachineStatus for this package.
// The default delays add 45 seconds to a test suite that uses only mock
// responses. A test that must have a specific delay gives it as an argument.
func TestMain(m *testing.M) {
	VIRTUALMACHINE_STATUS_POLL_INTERVAL = 10 * time.Millisecond
	DEFAULT_INITIAL_POLL_DELAY = 0
	os.Exit(m.Run())
}

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

	response, err := PollForVirtualMachineStatus(gpcnClient, context.Background(), vmID, []string{"Running"}, StatusPollOptions{Timeout: 10 * time.Second})
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

// TestPollForVirtualMachineStatusStopsOnContextCancellation verifies that the
// 5-second wait between status checks is interruptible. This loop runs for the
// lifetime of a create, resize, or attach, so a bare sleep here keeps Terraform
// working after the user has already interrupted the run.
func TestPollForVirtualMachineStatusStopsOnContextCancellation(t *testing.T) {
	const vmID = "vm-poll-cancel"
	polled := make(chan struct{}, 1)

	server, gpcnClient := testutil.SetupMockServerWithGpcnClient(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "GET" && strings.Contains(r.URL.Path, "/virtual-machines/"+vmID) {
				resp := newVMResponse(vmID, "test-vm")
				resp.Data.Status = "Building"
				testutil.WriteJSONResponse(w, resp)
				select {
				case polled <- struct{}{}:
				default:
				}
			}
		},
	})
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		_, err := PollForVirtualMachineStatus(gpcnClient, ctx, vmID, []string{"Running"}, StatusPollOptions{Timeout: 10 * time.Minute})
		done <- err
	}()

	select {
	case <-polled:
	case <-time.After(5 * time.Second):
		t.Fatal("poller never reached the server")
	}

	// Let the status check return so the loop is inside its 5-second wait.
	time.Sleep(100 * time.Millisecond)
	canceledAt := time.Now()
	cancel()

	select {
	case err := <-done:
		if waited := time.Since(canceledAt); waited > 500*time.Millisecond {
			t.Errorf("poller took %s to return after cancellation, expected it to abandon the interval wait", waited)
		}
		if !errors.Is(err, context.Canceled) {
			t.Errorf("error = %v, want it to wrap context.Canceled", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("poller did not return after the context was canceled")
	}
}

// TestPollForVirtualMachineStatusInitialDelayStopsOnCancellation covers the other
// wait in the poller: the initial delay, which defaults to 30 seconds.
func TestPollForVirtualMachineStatusInitialDelayStopsOnCancellation(t *testing.T) {
	const vmID = "vm-delay-cancel"
	// TestMain sets DEFAULT_INITIAL_POLL_DELAY to zero.
	const initialDelay = 30 * time.Second

	server, gpcnClient := testutil.SetupMockServerWithGpcnClient(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			testutil.WriteJSONResponse(w, newVMResponse(vmID, "test-vm"))
		},
	})
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	start := time.Now()
	_, err := PollForVirtualMachineStatus(gpcnClient, ctx, vmID, []string{"Running"}, StatusPollOptions{Timeout: 10 * time.Minute, InitialDelay: initialDelay})
	elapsed := time.Since(start)

	if !errors.Is(err, context.Canceled) {
		t.Errorf("error = %v, want it to wrap context.Canceled", err)
	}
	if elapsed > 500*time.Millisecond {
		t.Errorf("poller took %s to return, expected it to skip the %s initial delay on a canceled context",
			elapsed, initialDelay)
	}
}

// The API can report an empty status. The loop must not accept an empty status
// as a target status, because the virtual machine did not start.
func TestPollForVirtualMachineStatusRejectsEmptyStatus(t *testing.T) {
	const vmID = "vm-empty-status"
	var pollCount int

	server, gpcnClient := testutil.SetupMockServerWithGpcnClient(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			resp := newVMResponse(vmID, "test-vm")
			pollCount++
			if pollCount < 3 {
				// The API did not set the status.
				resp.Data.Status = ""
			}
			testutil.WriteJSONResponse(w, resp)
		},
	})
	defer server.Close()

	got, err := PollForVirtualMachineStatus(gpcnClient, context.Background(), vmID, []string{"Running", "Shutoff"}, StatusPollOptions{Timeout: 10 * time.Minute})
	if err != nil {
		t.Fatalf("PollForVirtualMachineStatus failed: %v", err)
	}
	if got.Data.Status != "Running" {
		t.Errorf("poller returned a VM with status %q, want it to have kept waiting for a target status", got.Data.Status)
	}
	if pollCount < 3 {
		t.Errorf("server saw %d polls, want at least 3: the empty statuses should not have satisfied the wait", pollCount)
	}
}

// A cancellation can happen during the status request or during an interval.
// The error must keep the cause in both conditions.
func TestPollForVirtualMachineStatusWrapsRequestFailure(t *testing.T) {
	const vmID = "vm-cancel-in-request"

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	server, gpcnClient := testutil.SetupMockServerWithGpcnClient(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			// Interrupt the run during the request, not during the interval.
			cancel()
			time.Sleep(200 * time.Millisecond)
			resp := newVMResponse(vmID, "test-vm")
			resp.Data.Status = "Building"
			testutil.WriteJSONResponse(w, resp)
		},
	})
	defer server.Close()

	_, err := PollForVirtualMachineStatus(gpcnClient, ctx, vmID, []string{"Running"}, StatusPollOptions{Timeout: 10 * time.Minute})
	if err == nil {
		t.Fatal("expected an error after cancellation, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("error = %v, want it to wrap context.Canceled", err)
	}
}

// timeoutMaxSec must apply to the clock. A sum of the intervals ignores the time
// of each status request. A request can continue for the request timeout
// multiplied by the retry count.
func TestPollForVirtualMachineStatusTimeoutCountsRequestTime(t *testing.T) {
	const (
		vmID         = "vm-slow-status"
		requestDelay = 300 * time.Millisecond
	)

	server, gpcnClient := testutil.SetupMockServerWithGpcnClient(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(requestDelay)
			resp := newVMResponse(vmID, "test-vm")
			resp.Data.Status = "Building"
			testutil.WriteJSONResponse(w, resp)
		},
	})
	defer server.Close()

	// Each request takes 300ms and the interval is 10ms. Therefore the clock, not
	// a count of the intervals, must end the loop.
	start := time.Now()
	_, err := PollForVirtualMachineStatus(gpcnClient, context.Background(), vmID, []string{"Running"}, StatusPollOptions{Timeout: 1 * time.Second})
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected a timeout error, got nil")
	}
	if !strings.Contains(err.Error(), "still not in the target status") {
		t.Errorf("error = %v, want the status timeout message", err)
	}
	if elapsed > 3*time.Second {
		t.Errorf("poller ran for %s against a 1s timeout: the request time is not being counted", elapsed)
	}
}

// A create that fails after the API gives an ID must name that ID in its
// message. Terraform records no state for the machine, so the message is the
// only way the operator learns that it exists.
func TestCreateVirtualMachineReportsPartialCreate(t *testing.T) {
	const (
		jobID   = "job-partial"
		vmID    = "vm-partial-789"
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
					Data: client.JobStatusDataResponse{
						Jobs: []client.JobResponse{{JobID: jobID, ResourceId: vmID}},
					},
				})
			case r.Method == "POST" && strings.Contains(r.URL.Path, "/jobs"):
				// The create job is complete. The virtual machine exists.
				testutil.HandleJobResponse(w, jobID, vmID, true)
			case r.Method == "GET" && strings.Contains(r.URL.Path, "/virtual-machines/"+vmID):
				// The provider cannot confirm that the machine started.
				w.WriteHeader(http.StatusInternalServerError)
			default:
				testutil.LogUnexpectedRequest(t, w, r)
			}
		},
	})
	defer server.Close()

	_, err := CreateVirtualMachine(gpcnClient, context.Background(), imageID, sizeID, createTestVMModel("test-vm", testVMImage, false))
	if err == nil {
		t.Fatal("expected an error when the VM never reaches a target status, got nil")
	}

	if !strings.Contains(err.Error(), vmID) {
		t.Errorf("error = %v, want the message to name the virtual machine that the API created", err)
	}

	var partial *client.PartialCreateError
	if !errors.As(err, &partial) {
		t.Fatalf("error = %v, want a PartialCreateError", err)
	}
	if partial.ResourceID != vmID {
		t.Errorf("ResourceID = %q, want %q", partial.ResourceID, vmID)
	}
}

// The API can report a target status before the virtual machine is ready. The
// poll must read the status again after it waits, and continue if the machine
// left the target status during that wait.
func TestPollForVirtualMachineStatusConfirmsTargetStatus(t *testing.T) {
	const vmID = "vm-flapping-status"
	var pollCount int

	server, gpcnClient := testutil.SetupMockServerWithGpcnClient(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			pollCount++
			resp := newVMResponse(vmID, "test-vm")
			switch pollCount {
			case 1:
				resp.Data.Status = "Running" // the API reports the target early
			case 2:
				resp.Data.Status = "Building" // the confirmation read disagrees
			default:
				resp.Data.Status = "Running"
			}
			testutil.WriteJSONResponse(w, resp)
		},
	})
	defer server.Close()

	got, err := PollForVirtualMachineStatus(gpcnClient, context.Background(), vmID,
		[]string{"Running"}, StatusPollOptions{Timeout: 10 * time.Minute})
	if err != nil {
		t.Fatalf("PollForVirtualMachineStatus failed: %v", err)
	}
	if got.Data.Status != "Running" {
		t.Errorf("returned status %q, want %q", got.Data.Status, "Running")
	}
	if pollCount < 4 {
		t.Errorf("server saw %d reads, want at least 4: the poll must confirm the status and continue after it disagrees", pollCount)
	}
}

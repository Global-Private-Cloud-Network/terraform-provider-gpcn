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

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
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

	return ResourceModel{
		Name:             types.StringValue(name),
		DatacenterId:     types.StringValue(testDatacenterID),
		Image:            types.StringValue(image),
		Size:             sizeObj,
		AllocatePublicIp: types.BoolValue(allocatePublicIP),
		WaitForStartup:   types.BoolValue(false),
		NetworkIds:       types.ListNull(types.StringType),
		VolumeIds:        types.ListNull(types.StringType),
	}
}

func newVMResponse(id, name string) *ReadVirtualMachinesResponse {
	resp := &ReadVirtualMachinesResponse{Success: true, Message: "VM retrieved"}
	resp.Data.Status = "Running"
	resp.Data.VirtualMachine.ID = id
	resp.Data.VirtualMachine.Name = name
	resp.Data.VirtualMachine.CreatedAt = time.Now().Format(time.RFC3339)
	resp.Data.VirtualMachine.UpdatedAt = time.Now().Format(time.RFC3339)
	resp.Data.VirtualMachine.ConfigurationId = 1
	resp.Data.VirtualMachine.Configuration = "General - Micro - 1"
	resp.Data.VirtualMachine.ConfigurationCode = "g-micro-1"
	resp.Data.VirtualMachine.ConfigurationCategoryCode = "general"
	resp.Data.VirtualMachine.CPU = 1
	resp.Data.VirtualMachine.RAM = 2
	resp.Data.VirtualMachine.Disk = 20
	resp.Data.VirtualMachine.Image = testVMImage
	resp.Data.VirtualMachine.Username = "admin"
	resp.Data.VirtualMachine.Datacenter.ID = testDatacenterID
	resp.Data.VirtualMachine.Datacenter.Name = "US-East-1"
	resp.Data.VirtualMachine.Datacenter.Region = "US-East"
	resp.Data.VirtualMachine.Datacenter.CountryAbbr = "US"
	resp.Data.VirtualMachine.Datacenter.Country = "United States"
	return resp
}

func emptyNetworkInterfacesResponse() map[string]any {
	return map[string]any{"success": true, "message": "Network interfaces retrieved", "data": []any{}}
}

func TestMapVirtualMachineResponseToModelUnit(t *testing.T) {
	server, httpClient := testutil.SetupMockServer(testutil.MockServerConfig{
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

	result := MapVirtualMachineResponseToModel(context.Background(), httpClient, response, model)

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

	server, httpClient := testutil.SetupMockServer(testutil.MockServerConfig{
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

	response, err := CreateVirtualMachine(httpClient, context.Background(), imageID, sizeID, createTestVMModel("test-vm", testVMImage, false))
	if err != nil {
		t.Fatalf("CreateVirtualMachine failed: %v", err)
	}
	if response == nil {
		t.Fatal("Expected response, got nil")
	}
	if response.Data.VirtualMachine.ID != vmID {
		t.Errorf("Expected VM ID '%s', got '%s'", vmID, response.Data.VirtualMachine.ID)
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

	server, httpClient := testutil.SetupMockServer(testutil.MockServerConfig{
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

	response, err := GetVirtualMachine(httpClient, context.Background(), vmID)
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

func TestUpdateVirtualMachineMockHTTP(t *testing.T) {
	const (
		vmID    = "vm-update-123"
		newName = "updated-vm"
	)

	var updateCalled, vmStatusCalled bool

	server, httpClient := testutil.SetupMockServer(testutil.MockServerConfig{
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
				resp.Data.VirtualMachine.CreatedAt = time.Now().Add(-24 * time.Hour).Format(time.RFC3339)
				testutil.WriteJSONResponse(w, resp)

			case r.Method == "GET" && strings.Contains(r.URL.Path, "/network-interfaces"):
				testutil.WriteJSONResponse(w, emptyNetworkInterfacesResponse())

			default:
				testutil.LogUnexpectedRequest(t, w, r)
			}
		},
	})
	defer server.Close()

	if err := UpdateVirtualMachine(httpClient, context.Background(), vmID, newName); err != nil {
		t.Fatalf("UpdateVirtualMachine failed: %v", err)
	}

	response, err := GetVirtualMachine(httpClient, context.Background(), vmID)
	if err != nil {
		t.Fatalf("GetVirtualMachine failed: %v", err)
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

func TestPollForVirtualMachineStatusMockHTTP(t *testing.T) {
	const vmID = "vm-poll-123"
	pollCount := 0

	server, httpClient := testutil.SetupMockServer(testutil.MockServerConfig{
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

	response, err := PollForVirtualMachineStatus(httpClient, context.Background(), vmID, []string{"Running"}, 10)
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

			server, httpClient := testutil.SetupMockServer(testutil.MockServerConfig{
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

			err := ValidatePublicIpValue(httpClient, context.Background(), model)

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

func TestGetSSHKeyMockHTTP(t *testing.T) {
	const (
		vmID       = "vm-ssh-123"
		privateKey = "-----BEGIN RSA PRIVATE KEY-----\nMIIEpAIBAAKCAQ...\n-----END RSA PRIVATE KEY-----"
	)

	server, httpClient := testutil.SetupMockServer(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "GET" && strings.Contains(r.URL.Path, "/virtual-machines/"+vmID+"/ssh-key") {
				testutil.WriteJSONResponse(w, map[string]any{
					"success": true,
					"message": "SSH key retrieved",
					"data":    map[string]string{"privateKey": privateKey},
				})
			} else {
				testutil.LogUnexpectedRequest(t, w, r)
			}
		},
	})
	defer server.Close()

	response, err := GetSSHKey(httpClient, context.Background(), vmID)
	if err != nil {
		t.Fatalf("GetSSHKey failed: %v", err)
	}
	if response == nil {
		t.Fatal("Expected response, got nil")
	}
	if response.Data.PrivateKey != privateKey {
		t.Errorf("Expected private key '%s', got '%s'", privateKey, response.Data.PrivateKey)
	}
}

func TestGetPasswordMockHTTP(t *testing.T) {
	const (
		vmID     = "vm-pwd-123"
		password = "SecureP@ssw0rd123!"
	)

	server, httpClient := testutil.SetupMockServer(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "GET" && strings.Contains(r.URL.Path, "/virtual-machines/"+vmID+"/password") {
				testutil.WriteJSONResponse(w, map[string]any{
					"success": true,
					"message": "Password retrieved",
					"data":    map[string]string{"sshPassword": password},
				})
			} else {
				testutil.LogUnexpectedRequest(t, w, r)
			}
		},
	})
	defer server.Close()

	response, err := GetPassword(httpClient, context.Background(), vmID)
	if err != nil {
		t.Fatalf("GetPassword failed: %v", err)
	}
	if response == nil {
		t.Fatal("Expected response, got nil")
	}
	if response.Data.SSHPassword != password {
		t.Errorf("Expected password '%s', got '%s'", password, response.Data.SSHPassword)
	}
}

func TestSetSecretValuesDisplaySecretsTrue(t *testing.T) {
	const (
		vmID       = "vm-secrets-123"
		username   = "admin"
		password   = "SecureP@ssw0rd123!"
		privateKey = "-----BEGIN RSA PRIVATE KEY-----\nMIIEpAIBAAKCAQ...\n-----END RSA PRIVATE KEY-----"
	)

	server, httpClient := testutil.SetupMockServer(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.Method == "GET" && strings.Contains(r.URL.Path, "/ssh-key"):
				testutil.WriteJSONResponse(w, map[string]any{
					"success": true, "message": "SSH key retrieved",
					"data": map[string]string{"privateKey": privateKey},
				})
			case r.Method == "GET" && strings.Contains(r.URL.Path, "/password"):
				testutil.WriteJSONResponse(w, map[string]any{
					"success": true, "message": "Password retrieved",
					"data": map[string]string{"sshPassword": password},
				})
			default:
				testutil.LogUnexpectedRequest(t, w, r)
			}
		},
	})
	defer server.Close()

	response := newVMResponse(vmID, "test-vm")
	response.Data.VirtualMachine.Username = username

	model := createTestVMModel("test-vm", testVMImage, false)
	model.DisplaySecrets = types.BoolValue(true)

	result := setSecretValues(context.Background(), httpClient, response, model)

	if result.Secrets.IsNull() || result.Secrets.IsUnknown() {
		t.Fatal("Expected secrets to be set")
	}

	secretsMap := result.Secrets.Elements()
	assertSecretValue(t, secretsMap, "username", username)
	assertSecretValue(t, secretsMap, "password", password)
	assertSecretValue(t, secretsMap, "ssh_key", privateKey)
}

func TestSetSecretValuesDisplaySecretsFalse(t *testing.T) {
	const vmID = "vm-secrets-456"
	sshKeyCalled, passwordCalled := false, false

	server, httpClient := testutil.SetupMockServer(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			if strings.Contains(r.URL.Path, "/ssh-key") {
				sshKeyCalled = true
			} else if strings.Contains(r.URL.Path, "/password") {
				passwordCalled = true
			}
			w.WriteHeader(http.StatusNotFound)
		},
	})
	defer server.Close()

	response := newVMResponse(vmID, "test-vm")
	model := createTestVMModel("test-vm", testVMImage, false)
	model.DisplaySecrets = types.BoolValue(false)

	result := setSecretValues(context.Background(), httpClient, response, model)

	if sshKeyCalled {
		t.Error("SSH key endpoint should not be called when display_secrets is false")
	}
	if passwordCalled {
		t.Error("Password endpoint should not be called when display_secrets is false")
	}
	if result.Secrets.IsNull() || result.Secrets.IsUnknown() {
		t.Fatal("Expected secrets to be set (even if empty)")
	}

	secretsMap := result.Secrets.Elements()
	assertSecretValue(t, secretsMap, "username", "")
	assertSecretValue(t, secretsMap, "password", "")
	assertSecretValue(t, secretsMap, "ssh_key", "")
}

func assertSecretValue(t *testing.T, secretsMap map[string]attr.Value, key, expected string) {
	t.Helper()
	val, ok := secretsMap[key]
	if !ok {
		t.Fatalf("Expected %s in secrets map", key)
	}
	if val.(types.String).ValueString() != expected {
		t.Errorf("Expected %s '%s', got '%s'", key, expected, val.(types.String).ValueString())
	}
}

func TestSetNetworkModelValuesNotPresentWithPublicIP(t *testing.T) {
	const (
		vmID      = "vm-network-123"
		publicIP  = "203.0.113.42"
		networkID = "network-456"
	)

	server, httpClient := testutil.SetupMockServer(testutil.MockServerConfig{
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

	result := setNetworkModelValuesNotPresent(context.Background(), httpClient, vmID, model)

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

	server, httpClient := testutil.SetupMockServer(testutil.MockServerConfig{
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

	result := setNetworkModelValuesNotPresent(context.Background(), httpClient, vmID, model)

	if result.PublicIp.ValueString() != "" {
		t.Errorf("Expected empty public IP, got '%s'", result.PublicIp.ValueString())
	}
	if result.AllocatePublicIp.ValueBool() {
		t.Error("Expected AllocatePublicIp to be false when no public IP exists")
	}
}

func TestMapVirtualMachineResponseToModelWithSecrets(t *testing.T) {
	const (
		vmID       = "vm-full-mapping-123"
		username   = "admin"
		password   = "TestPassword123!"
		privateKey = "-----BEGIN RSA PRIVATE KEY-----\ntest-key\n-----END RSA PRIVATE KEY-----"
		publicIP   = "198.51.100.50"
	)

	server, httpClient := testutil.SetupMockServer(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.Method == "GET" && strings.Contains(r.URL.Path, "/network-interfaces"):
				testutil.WriteJSONResponse(w, map[string]any{
					"success": true, "message": "Network interfaces retrieved",
					"data": []map[string]any{{
						"id": "interface-003", "networkInterface": 0, "isPrimary": 1,
						"publicIp": publicIP, "publicIpId": "pubip-003", "privateIp": "10.0.0.20",
						"networkName": "standard-network", "networkId": "network-std-001",
						"cidrBlock": "10.0.0.0/24", "gatewayIp": "10.0.0.1", "networkType": "standard",
					}},
				})
			case r.Method == "GET" && strings.Contains(r.URL.Path, "/ssh-key"):
				testutil.WriteJSONResponse(w, map[string]any{
					"success": true, "message": "SSH key retrieved",
					"data": map[string]string{"privateKey": privateKey},
				})
			case r.Method == "GET" && strings.Contains(r.URL.Path, "/password"):
				testutil.WriteJSONResponse(w, map[string]any{
					"success": true, "message": "Password retrieved",
					"data": map[string]string{"sshPassword": password},
				})
			default:
				testutil.LogUnexpectedRequest(t, w, r)
			}
		},
	})
	defer server.Close()

	response := newVMResponse(vmID, "test-vm-full")
	response.Data.VirtualMachine.Username = username

	model := createTestVMModel("test-vm-full", testVMImage, true)
	model.DisplaySecrets = types.BoolValue(true)

	result := MapVirtualMachineResponseToModel(context.Background(), httpClient, response, model)

	if result.ID.ValueString() != vmID {
		t.Errorf("Expected ID '%s', got '%s'", vmID, result.ID.ValueString())
	}
	if result.PublicIp.ValueString() != publicIP {
		t.Errorf("Expected public IP '%s', got '%s'", publicIP, result.PublicIp.ValueString())
	}
	if result.Secrets.IsNull() || result.Secrets.IsUnknown() {
		t.Fatal("Expected secrets to be set")
	}

	secretsMap := result.Secrets.Elements()
	assertSecretValue(t, secretsMap, "username", username)
	assertSecretValue(t, secretsMap, "password", password)
	assertSecretValue(t, secretsMap, "ssh_key", privateKey)
}

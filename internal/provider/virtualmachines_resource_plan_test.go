package provider

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"sync"
	"testing"
	"time"

	"terraform-provider-gpcn/internal/testutil"
	"terraform-provider-gpcn/internal/virtualmachines"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
)

const (
	vmPlanTestID           = "vm-1"
	vmPlanTestDatacenterID = "dc-1"
	vmPlanTestSizeID       = "sku-1"
	vmPlanTestSizeCode     = "G-Small-1"
	vmPlanTestImageID      = "img-1"
	vmPlanTestImageName    = "ubuntu-22.04"
	vmPlanTestSubnetID     = "subnet-1"
	vmPlanTestVpcID        = "vpc-1"
	vmPlanTestSshKeyID     = "key-1"
	vmPlanTestUsername     = "ubuntu"
	vmPlanTestTimestamp    = "2026-01-02T15:04:05Z"
)

// shortenVirtualMachinePolling removes the waits the create, stop, and delete paths
// take. The package variables carry production defaults that make this test minutes long.
// A caller must stay sequential, because Go resumes a parallel test only after every
// sequential test ends.
func shortenVirtualMachinePolling(t *testing.T) {
	t.Helper()

	previousInterval := virtualmachines.VM_STATUS_POLL_INTERVAL
	previousSettle := virtualmachines.VM_STATUS_SETTLE_WAIT
	previousDelay := virtualmachines.DEFAULT_INITIAL_POLL_DELAY_SECONDS
	virtualmachines.VM_STATUS_POLL_INTERVAL = 5 * time.Millisecond
	virtualmachines.VM_STATUS_SETTLE_WAIT = 5 * time.Millisecond
	virtualmachines.DEFAULT_INITIAL_POLL_DELAY_SECONDS = 0
	t.Cleanup(func() {
		virtualmachines.VM_STATUS_POLL_INTERVAL = previousInterval
		virtualmachines.VM_STATUS_SETTLE_WAIT = previousSettle
		virtualmachines.DEFAULT_INITIAL_POLL_DELAY_SECONDS = previousDelay
	})
}

func vmPlanTestSizesBody() map[string]any {
	return map[string]any{
		"success": true,
		"message": "ok",
		"data": map[string]any{
			"datacenterId": vmPlanTestDatacenterID,
			"categories":   []map[string]any{},
		},
	}
}

func vmPlanTestReadBody(name, status, skuId string) map[string]any {
	return map[string]any{
		"success": true,
		"message": "ok",
		"data": map[string]any{
			"status":    status,
			"id":        vmPlanTestID,
			"name":      name,
			"createdAt": vmPlanTestTimestamp,
			"updatedAt": vmPlanTestTimestamp,
			"configuration": map[string]any{
				"name":    vmPlanTestSizeCode,
				"skuId":   skuId,
				"skuCode": "g-small-1",
				"cpu":     2,
				"ram":     4,
				"disk":    80,
			},
			"image":          vmPlanTestImageName,
			"username":       vmPlanTestUsername,
			"sshKeyId":       vmPlanTestSshKeyID,
			"networkHotplug": 1,
			"datacenter": map[string]any{
				"id":          vmPlanTestDatacenterID,
				"name":        "Kansas",
				"region":      "central",
				"countryAbbr": "US",
				"country":     "United States",
			},
		},
	}
}

// The birth subnet comes from the create body. The interface the API reports is
// therefore the one the provider asked for. A fixed fixture could agree by coincidence.
// Every legacy column is null, which is what a VPC interface carries.
func vmPlanTestNetworkInterfacesBody(birthSubnetID string) map[string]any {
	return map[string]any{
		"success": true,
		"message": "ok",
		"data": []map[string]any{{
			"id":               "nic-1",
			"networkInterface": 1,
			"isPrimary":        1,
			"macAddress":       "fa:16:3e:00:00:01",
			"publicIp":         nil,
			"publicIpId":       nil,
			"privateIp":        "10.0.0.5",
			"world":            "vpc",
			"networkName":      nil,
			"networkId":        nil,
			"cidrBlock":        "10.0.0.0/24",
			"gatewayIp":        nil,
			"networkType":      nil,
			"vpcSubnetId":      birthSubnetID,
			"subnetName":       "web",
			"vpcId":            vmPlanTestVpcID,
			"vpcName":          "prod",
			"l2SegmentId":      nil,
			"l2SegmentName":    nil,
		}},
	}
}

// startVirtualMachinePlanMockServer serves the endpoints a drift test needs. The handler
// keeps the name from the last create or update. The read after an apply then agrees with
// the configuration and leaves the refresh plan empty. The returned functions change the
// stored name and SKU out of band, which is how a test creates drift.
// The sizes arm reports no upgrade target, so a planned size change asks for a replacement.
func startVirtualMachinePlanMockServer(t *testing.T) (*httptest.Server, func(string), func(string)) {
	t.Helper()

	var mu sync.Mutex
	name := ""
	birthSubnetID := ""
	skuId := vmPlanTestSizeID
	status := virtualmachines.VMStatusRunning.String()

	vmPath := "/v1/resource/virtual-machines/" + vmPlanTestID

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/auth/check":
			testutil.HandleAuthCheck(w)
		case r.Method == http.MethodPost && r.URL.Path == "/v1/resource/virtual-machines/":
			body := testutil.ReadRequestBody(r)
			mu.Lock()
			name, _ = body["name"].(string)
			birthSubnetID, _ = body["subnetId"].(string)
			mu.Unlock()
			testutil.HandleJobResponse(w, "job-1", vmPlanTestID, true)
		case r.Method == http.MethodPost && r.URL.Path == "/v1/resource/jobs/":
			testutil.HandleJobResponse(w, "job-1", vmPlanTestID, true)
		case r.Method == http.MethodPost && r.URL.Path == vmPath+"/stop":
			mu.Lock()
			status = virtualmachines.VMStatusShutoff.String()
			mu.Unlock()
			testutil.WriteJSONResponse(w, map[string]any{"success": true})
		case r.Method == http.MethodGet && r.URL.Path == vmPath+"/network-interfaces":
			mu.Lock()
			currentSubnet := birthSubnetID
			mu.Unlock()
			testutil.WriteJSONResponse(w, vmPlanTestNetworkInterfacesBody(currentSubnet))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/resource/data-centers/"+vmPlanTestDatacenterID+"/virtual-machine-sizes":
			testutil.WriteJSONResponse(w, vmPlanTestSizesBody())
		case r.Method == http.MethodGet && r.URL.Path == vmPath:
			mu.Lock()
			currentName, currentStatus, currentSku := name, status, skuId
			mu.Unlock()
			testutil.WriteJSONResponse(w, vmPlanTestReadBody(currentName, currentStatus, currentSku))
		case r.Method == http.MethodPut && r.URL.Path == vmPath:
			body := testutil.ReadRequestBody(r)
			mu.Lock()
			if updated, ok := body["name"].(string); ok {
				name = updated
			}
			mu.Unlock()
			testutil.WriteJSONResponse(w, map[string]any{"success": true})
		case r.Method == http.MethodDelete && r.URL.Path == vmPath:
			testutil.HandleCreateJobResponse(w, "job-2", "delete issued")
		default:
			testutil.LogUnexpectedRequest(t, w, r)
		}
	}))
	t.Cleanup(server.Close)

	setName := func(newName string) {
		mu.Lock()
		name = newName
		mu.Unlock()
	}

	setSkuId := func(newSkuId string) {
		mu.Lock()
		skuId = newSkuId
		mu.Unlock()
	}

	return server, setName, setSkuId
}

func vmPlanTestConfig(host, name string) string {
	return fmt.Sprintf(`
provider "gpcn" {
  host    = %q
  api_key = "test-key"
}

resource "gpcn_virtualmachine" "test" {
  name               = %q
  datacenter_id      = %q
  size_id            = %q
  image_id           = %q
  allocate_public_ip = false
  subnet_id          = %q
  initial_auth = {
    ssh_key_id = %q
    username   = %q
  }
}
`, host, name, vmPlanTestDatacenterID, vmPlanTestSizeID, vmPlanTestImageID, vmPlanTestSubnetID, vmPlanTestSshKeyID, vmPlanTestUsername)
}

func TestVirtualMachineResourcePlanDetectsOutOfBandRename(t *testing.T) {
	shortenVirtualMachinePolling(t)
	server, setName, _ := startVirtualMachinePlanMockServer(t)

	config := vmPlanTestConfig(server.URL, "vm-plan-a")

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnVirtualMachineTest, plancheck.ResourceActionCreate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnVirtualMachineTest, "name", "vm-plan-a"),
					resource.TestCheckResourceAttr(gpcnVirtualMachineTest, "id", vmPlanTestID),
				),
			},
			{
				PreConfig: func() { setName("renamed-out-of-band") },
				Config:    config,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnVirtualMachineTest, plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnVirtualMachineTest, "name", "vm-plan-a"),
				),
			},
		},
	})
}

// A refreshed size_id would plan a downgrade that the API refuses, and Terraform would
// then replace the VM. Read keeps the configured value to prevent that.
func TestVirtualMachineResourcePlanIgnoresOutOfBandResize(t *testing.T) {
	shortenVirtualMachinePolling(t)
	server, _, setSkuId := startVirtualMachinePlanMockServer(t)

	config := vmPlanTestConfig(server.URL, "vm-plan-b")

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnVirtualMachineTest, plancheck.ResourceActionCreate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnVirtualMachineTest, "size_id", vmPlanTestSizeID),
				),
			},
			{
				PreConfig: func() { setSkuId("sku-2") },
				Config:    config,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnVirtualMachineTest, plancheck.ResourceActionNoop),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnVirtualMachineTest, "size_id", vmPlanTestSizeID),
				),
			},
		},
	})
}

// startVirtualMachineDestroyMockServer refuses to delete. A test that reaches the delete
// endpoint has not treated the machine as gone.
func startVirtualMachineDestroyMockServer(t *testing.T) (*httptest.Server, func()) {
	t.Helper()

	var mu sync.Mutex
	name := ""
	birthSubnetID := ""
	status := virtualmachines.VMStatusRunning.String()

	vmPath := "/v1/resource/virtual-machines/" + vmPlanTestID

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/auth/check":
			testutil.HandleAuthCheck(w)
		case r.Method == http.MethodPost && r.URL.Path == "/v1/resource/virtual-machines/":
			body := testutil.ReadRequestBody(r)
			mu.Lock()
			name, _ = body["name"].(string)
			birthSubnetID, _ = body["subnetId"].(string)
			mu.Unlock()
			testutil.HandleJobResponse(w, "job-1", vmPlanTestID, true)
		case r.Method == http.MethodPost && r.URL.Path == "/v1/resource/jobs/":
			testutil.HandleJobResponse(w, "job-1", vmPlanTestID, true)
		case r.Method == http.MethodGet && r.URL.Path == vmPath+"/network-interfaces":
			mu.Lock()
			currentSubnet := birthSubnetID
			mu.Unlock()
			testutil.WriteJSONResponse(w, vmPlanTestNetworkInterfacesBody(currentSubnet))
		case r.Method == http.MethodPost && r.URL.Path == vmPath+"/stop":
			testutil.WriteJSONResponse(w, map[string]any{"success": true})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/resource/data-centers/"+vmPlanTestDatacenterID+"/virtual-machine-sizes":
			testutil.WriteJSONResponse(w, vmPlanTestSizesBody())
		case r.Method == http.MethodGet && r.URL.Path == vmPath:
			mu.Lock()
			currentName, currentStatus := name, status
			mu.Unlock()
			testutil.WriteJSONResponse(w, vmPlanTestReadBody(currentName, currentStatus, vmPlanTestSizeID))
		case r.Method == http.MethodDelete && r.URL.Path == vmPath:
			t.Error("Expected no delete call for a machine the platform already removed")
			testutil.HandleCreateJobResponse(w, "job-2", "delete issued")
		default:
			testutil.LogUnexpectedRequest(t, w, r)
		}
	}))
	t.Cleanup(server.Close)

	setDeleting := func() {
		mu.Lock()
		status = virtualmachines.VMStatusDeleting.String()
		mu.Unlock()
	}

	return server, setDeleting
}

// A machine the platform is already removing never reaches a stopped status. The
// pre-delete stop fails fast on a status the machine never leaves. The outcome matches a
// 404: the machine is gone, so it leaves state without an error.
func TestVirtualMachineResourcePlanDestroyTreatsDeletingAsGone(t *testing.T) {
	shortenVirtualMachinePolling(t)
	server, setDeleting := startVirtualMachineDestroyMockServer(t)

	config := vmPlanTestConfig(server.URL, "vm-plan-destroy")

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnVirtualMachineTest, "id", vmPlanTestID),
				),
			},
			{
				PreConfig: setDeleting,
				Config:    config,
				Destroy:   true,
			},
		},
	})
}

// GPCN refuses a held address beside a request for a new one. The provider says so at
// validate time, so the operator never pays for the round trip that would say it.
func TestVirtualMachineResourcePlanRefusesPublicIpIdWithAllocatePublicIp(t *testing.T) {
	config := fmt.Sprintf(`
provider "gpcn" {
  host    = "http://127.0.0.1:1"
  api_key = "test-key"
}

resource "gpcn_virtualmachine" "test" {
  name               = "vm-plan-conflict"
  datacenter_id      = %q
  size_id            = %q
  image_id           = %q
  subnet_id          = %q
  allocate_public_ip = true
  public_ip_id       = "address-1"
  initial_auth = {
    ssh_key_id = %q
    username   = %q
  }
}
`, vmPlanTestDatacenterID, vmPlanTestSizeID, vmPlanTestImageID, vmPlanTestSubnetID, vmPlanTestSshKeyID, vmPlanTestUsername)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      config,
				ExpectError: regexp.MustCompile(`public_ip_id and allocate_public_ip are mutually exclusive`),
			},
		},
	})
}

package provider

import (
	"fmt"
	"net/http"
	"net/http/httptest"
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
	vmPlanTestNetworkID    = "net-1"
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

func vmPlanTestNetworkInterfacesBody() map[string]any {
	return map[string]any{
		"success": true,
		"message": "ok",
		"data": []map[string]any{{
			"id":               "nic-1",
			"networkInterface": 0,
			"isPrimary":        1,
			"publicIp":         "",
			"publicIpId":       "",
			"privateIp":        "10.0.0.5",
			"networkName":      "net-standard",
			"networkId":        vmPlanTestNetworkID,
			"cidrBlock":        "10.0.0.0/24",
			"gatewayIp":        "10.0.0.1",
			"networkType":      "standard",
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
	skuId := vmPlanTestSizeID
	status := virtualmachines.VMStatusRunning.String()

	vmPath := "/v1/resource/virtual-machines/" + vmPlanTestID

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/resource/virtual-machines/":
			body := testutil.ReadRequestBody(r)
			mu.Lock()
			name, _ = body["name"].(string)
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
			testutil.WriteJSONResponse(w, vmPlanTestNetworkInterfacesBody())
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
  network_ids        = [%q]
  initial_auth = {
    ssh_key_id = %q
    username   = %q
  }
}
`, host, name, vmPlanTestDatacenterID, vmPlanTestSizeID, vmPlanTestImageID, vmPlanTestNetworkID, vmPlanTestSshKeyID, vmPlanTestUsername)
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
